// Package tui is bridge's terminal interface.
package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/raven-clown/bridge/config"
	"github.com/raven-clown/bridge/forge"
	"github.com/raven-clown/bridge/inbox"
	"github.com/raven-clown/bridge/internal/termsafe"
	"github.com/raven-clown/bridge/internal/tui/inboxview"
	"github.com/raven-clown/bridge/internal/tui/issueview"
	"github.com/raven-clown/bridge/internal/tui/msgs"
	"github.com/raven-clown/bridge/internal/tui/style"
	"github.com/raven-clown/bridge/internal/tui/threadview"
	"github.com/raven-clown/bridge/internal/tui/triageview"
	"github.com/raven-clown/bridge/store"
)

type Deps struct {
	Refresher *inbox.Refresher
	Forge     forge.Forge
	Store     *store.Store
	Config    config.Config
	Me        string
	Now       func() time.Time
	// Warning starts in the status bar, such as a root that does not exist.
	Warning string
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
	viewIssue
)

type Model struct {
	ctx  context.Context
	deps Deps

	width, height int
	view          view
	inbox         inboxview.Model
	thread        threadview.Model
	issue         issueview.Model

	triaging bool
	triage   triageview.Model

	refreshing  bool
	lastRefresh time.Time
	status      string
}

func newApp(ctx context.Context, d Deps) Model {
	if d.Now == nil {
		d.Now = time.Now
	}
	m := Model{ctx: ctx, deps: d, width: 80, height: 24, inbox: inboxview.New(80, 23, d.Now), status: termsafe.Line(d.Warning)}
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
		// Replies and seen marks change the inbox, so refresh on the way back.
		left := m.view
		m.view = viewInbox
		if left == viewThread && !m.refreshing {
			m.refreshing = true
			return m, m.refresh()
		}
		return m, nil
	case msgs.OpenTriage:
		m.triaging = true
		m.triage = triageview.New(msg.IDs, triageview.Deps{Forge: m.deps.Forge, Store: m.deps.Store, Config: m.deps.Config}, m.panelWidth(), m.height-1)
		return m, m.triage.Init()
	case msgs.TriageDone:
		m.triaging = false
		cmds := []tea.Cmd{emit(msgs.Refresh{})}
		if m.view == viewThread {
			cmds = append(cmds, m.thread.Init())
		}
		return m, tea.Batch(cmds...)
	case msgs.NewIssue:
		m.view = viewIssue
		m.issue = issueview.New(m.deps.repos(), issueview.Deps{Forge: m.deps.Forge, Config: m.deps.Config}, m.width, m.height-1)
		return m, nil
	case msgs.IssueCreated:
		m.view = viewInbox
		m.status = "created " + msg.URL
		return m, emit(msgs.Refresh{})
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
		r, ctx := m.deps.Refresher, m.ctx
		return m, func() tea.Msg { return refreshDoneMsg(r.RefreshRescan(ctx)) }
	case msgs.Status:
		m.status = msg.Text
		return m, nil
	case msgs.OpenURL:
		m.status = termsafe.Line(msg.URL)
		return m, nil
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if m.triaging {
			var cmd tea.Cmd
			m.triage, cmd = m.triage.Update(msg)
			return m, cmd
		}
		if m.view == viewInbox && msg.String() == "q" && !m.inbox.Filtering() {
			return m, tea.Quit
		}
		return m.updateView(msg)
	}
	// Everything else may be a reply to the panel or to the view underneath.
	var cmds []tea.Cmd
	if m.triaging {
		var cmd tea.Cmd
		m.triage, cmd = m.triage.Update(msg)
		cmds = append(cmds, cmd)
	}
	next, cmd := m.updateView(msg)
	return next, tea.Batch(append(cmds, cmd)...)
}

func (m Model) updateView(msg tea.Msg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.view {
	case viewThread:
		m.thread, cmd = m.thread.Update(msg)
	case viewIssue:
		m.issue, cmd = m.issue.Update(msg)
	default:
		m.inbox, cmd = m.inbox.Update(msg)
	}
	return m, cmd
}

func emit(msg tea.Msg) tea.Cmd { return func() tea.Msg { return msg } }

func (m Model) panelWidth() int { return max(min(44, m.width/2), 30) }

func (d Deps) repos() []forge.RepoID {
	var out []forge.RepoID
	add := func(r forge.RepoID) {
		if !slices.Contains(out, r) {
			out = append(out, r)
		}
	}
	for _, lr := range d.Refresher.Repos {
		if lr.Repo != nil {
			add(*lr.Repo)
		}
	}
	for _, r := range d.Refresher.Extra {
		add(r)
	}
	return out
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

// overlay draws panel over the right edge of base.
func overlay(base, panel string, width int) string {
	b := strings.Split(base, "\n")
	p := strings.Split(panel, "\n")
	pw := 0
	for _, l := range p {
		pw = max(pw, ansi.StringWidth(l))
	}
	left := max(width-pw, 0)
	for len(b) < len(p) {
		b = append(b, "")
	}
	for i, l := range p {
		row := ansi.Truncate(b[i], left, "")
		b[i] = row + strings.Repeat(" ", max(left-ansi.StringWidth(row), 0)) + l
	}
	return strings.Join(b, "\n")
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
	switch m.view {
	case viewThread:
		body = m.thread.View()
	case viewIssue:
		body = m.issue.View()
	}
	if m.triaging {
		body = overlay(body, m.triage.View(), m.width)
	}
	lines := strings.Count(body, "\n") + 1
	if pad := m.height - 1 - lines; pad > 0 {
		body += strings.Repeat("\n", pad)
	}
	return body + "\n" + m.statusBar()
}
