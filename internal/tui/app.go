// Package tui is bridge's terminal interface.
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/raven-clown/bridge/config"
	"github.com/raven-clown/bridge/forge"
	"github.com/raven-clown/bridge/inbox"
	"github.com/raven-clown/bridge/internal/termsafe"
	"github.com/raven-clown/bridge/internal/tui/inboxview"
	"github.com/raven-clown/bridge/internal/tui/msgs"
	"github.com/raven-clown/bridge/internal/tui/style"
	"github.com/raven-clown/bridge/internal/tui/threadview"
	"github.com/raven-clown/bridge/store"
)

type Deps struct {
	Refresher *inbox.Refresher
	Forge     forge.Forge
	Store     *store.Store
	Config    config.Config
	Me        string
	Now       func() time.Time
}

type (
	refreshDoneMsg inbox.Result
	tickMsg        struct{}
)

type rateLimiter interface {
	RateLimit() (int, time.Time)
}

type view int

const (
	viewInbox view = iota
	viewThread
)

type Model struct {
	ctx  context.Context
	deps Deps

	width, height int
	view          view
	inbox         inboxview.Model
	thread        threadview.Model

	refreshing  bool
	lastRefresh time.Time
	status      string
}

func newApp(ctx context.Context, d Deps) Model {
	if d.Now == nil {
		d.Now = time.Now
	}
	m := Model{ctx: ctx, deps: d, width: 80, height: 24, inbox: inboxview.New(80, 23, d.Now)}
	if res, ok := d.Refresher.Cached(); ok {
		m.inbox = m.inbox.SetResult(res, true)
	}
	return m
}

func Run(ctx context.Context, d Deps) error {
	_, err := tea.NewProgram(newApp(ctx, d), tea.WithAltScreen(), tea.WithContext(ctx)).Run()
	return err
}

func (m Model) refresh() tea.Cmd {
	r, ctx := m.deps.Refresher, m.ctx
	return func() tea.Msg { return refreshDoneMsg(r.Refresh(ctx)) }
}

func (m Model) tick() tea.Cmd {
	return tea.Tick(time.Duration(m.deps.Config.RefreshSeconds)*time.Second, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.refresh(), m.tick())
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.inbox = m.inbox.SetSize(m.width, m.height-1)
		if m.view == viewThread {
			m.thread = m.thread.SetSize(m.width, m.height-1)
		}
		return m, nil
	case msgs.OpenThread:
		m.view = viewThread
		m.status = ""
		m.thread = threadview.New(msg.ID, threadview.Deps{Forge: m.deps.Forge, Store: m.deps.Store, Now: m.deps.Now}, m.width, m.height-1)
		return m, m.thread.Init()
	case msgs.Back:
		m.view = viewInbox
		return m, nil
	case refreshDoneMsg:
		m.refreshing = false
		m.lastRefresh = m.deps.Now()
		m.inbox = m.inbox.SetResult(inbox.Result(msg), false)
		return m, nil
	case tickMsg:
		if m.refreshing {
			return m, m.tick()
		}
		m.refreshing = true
		return m, tea.Batch(m.refresh(), m.tick())
	case msgs.Refresh:
		if m.refreshing {
			return m, nil
		}
		m.refreshing = true
		m.status = "refreshing"
		return m, m.refresh()
	case msgs.Status:
		m.status = msg.Text
		return m, nil
	case msgs.OpenURL:
		m.status = termsafe.Line(msg.URL)
		return m, nil
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" || (m.view == viewInbox && msg.String() == "q" && !m.inbox.Filtering()) {
			return m, tea.Quit
		}
	}
	var cmd tea.Cmd
	switch m.view {
	case viewThread:
		m.thread, cmd = m.thread.Update(msg)
	default:
		m.inbox, cmd = m.inbox.Update(msg)
	}
	return m, cmd
}

func (m Model) statusBar() string {
	parts := []string{termsafe.Line(m.deps.Me)}
	if rl, ok := m.deps.Forge.(rateLimiter); ok {
		if n, _ := rl.RateLimit(); n >= 0 {
			parts = append(parts, fmt.Sprintf("rate %d", n))
		}
	}
	switch {
	case m.refreshing:
		parts = append(parts, "refreshing")
	case !m.lastRefresh.IsZero():
		parts = append(parts, "refreshed "+ago(m.deps.Now(), m.lastRefresh)+" ago")
	}
	if m.status != "" && m.status != "refreshing" {
		parts = append(parts, m.status)
	}
	return style.StatusBar.Render(ansi.Truncate(strings.Join(parts, " · "), m.width, "…"))
}

func ago(now, t time.Time) string {
	d := now.Sub(t)
	if d < time.Hour {
		return fmt.Sprintf("%dm", max(int(d.Minutes()), 0))
	}
	return fmt.Sprintf("%dh", int(d.Hours()))
}

func (m Model) View() string {
	body := m.inbox.View()
	if m.view == viewThread {
		body = m.thread.View()
	}
	lines := strings.Count(body, "\n") + 1
	if pad := m.height - 1 - lines; pad > 0 {
		body += strings.Repeat("\n", pad)
	}
	return body + "\n" + m.statusBar()
}
