// Package issueview is the form that creates an issue with the
// repository's sidebar defaults filled in.
package issueview

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/raven-clown/bridge/config"
	"github.com/raven-clown/bridge/forge"
	"github.com/raven-clown/bridge/internal/termsafe"
	"github.com/raven-clown/bridge/internal/tui/msgs"
	"github.com/raven-clown/bridge/internal/tui/style"
	"github.com/raven-clown/bridge/triage"
)

type Deps struct {
	Forge  forge.Forge
	Config config.Config
}

type createdMsg struct {
	id  forge.ItemID
	err error
}

const (
	fieldRepo = iota
	fieldTitle
	fieldBody
	fieldLabels
	fieldCount
)

type Model struct {
	repos         []forge.RepoID
	deps          Deps
	width, height int

	repo         int
	focus        int
	title        textinput.Model
	body         textarea.Model
	labels       textinput.Model
	labelsEdited bool

	sending bool
	err     error
}

func New(repos []forge.RepoID, d Deps, width, height int) Model {
	title := textinput.New()
	title.Placeholder = "Title"
	title.Prompt = ""
	title.Cursor.SetMode(cursor.CursorStatic)
	body := textarea.New()
	body.Placeholder = "Describe it in Markdown"
	body.ShowLineNumbers = false
	body.SetHeight(6)
	body.Cursor.SetMode(cursor.CursorStatic)
	labels := textinput.New()
	labels.Placeholder = "comma separated"
	labels.Prompt = ""
	labels.Cursor.SetMode(cursor.CursorStatic)

	m := Model{repos: repos, deps: d, title: title, body: body, labels: labels, focus: fieldTitle}
	m.prefill()
	m = m.SetSize(width, height)
	m.title.Focus()
	return m
}

func (m Model) SetSize(width, height int) Model {
	m.width, m.height = width, height
	w := max(width-14, 20)
	m.title.Width, m.labels.Width = w, w
	m.body.SetWidth(w)
	return m
}

func (m Model) current() (forge.RepoID, bool) {
	if len(m.repos) == 0 {
		return forge.RepoID{}, false
	}
	return m.repos[m.repo], true
}

func (m *Model) prefill() {
	if m.labelsEdited {
		return
	}
	r, ok := m.current()
	if !ok {
		return
	}
	m.labels.SetValue(strings.Join(m.deps.Config.PolicyFor(r).Sidebar.Labels, ", "))
}

func (m *Model) setFocus(f int) {
	m.focus = (f + fieldCount) % fieldCount
	m.title.Blur()
	m.body.Blur()
	m.labels.Blur()
	switch m.focus {
	case fieldTitle:
		m.title.Focus()
	case fieldBody:
		m.body.Focus()
	case fieldLabels:
		m.labels.Focus()
	}
}

func emit(msg tea.Msg) tea.Cmd { return func() tea.Msg { return msg } }

func splitLabels(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func issueURL(id forge.ItemID) string {
	return fmt.Sprintf("https://%s/%s/%s/issues/%d", id.Repo.Host, id.Repo.Owner, id.Repo.Name, id.Number)
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case createdMsg:
		m.sending = false
		if msg.id.Number == 0 {
			m.err = msg.err
			return m, nil
		}
		created := emit(msgs.IssueCreated{ID: msg.id, URL: issueURL(msg.id)})
		if msg.err != nil {
			return m, tea.Sequence(created, emit(msgs.Status{Text: termsafe.Line(msg.err.Error())}))
		}
		return m, created
	case tea.KeyMsg:
		return m.key(msg)
	}
	return m, nil
}

func (m Model) key(k tea.KeyMsg) (Model, tea.Cmd) {
	if m.sending {
		return m, nil
	}
	switch k.String() {
	case "esc":
		return m, emit(msgs.Back{})
	case "tab":
		m.setFocus(m.focus + 1)
		return m, nil
	case "shift+tab":
		m.setFocus(m.focus - 1)
		return m, nil
	case "ctrl+s":
		return m.submit()
	}
	var cmd tea.Cmd
	switch m.focus {
	case fieldRepo:
		switch k.String() {
		case "left", "h":
			m.repo = (m.repo - 1 + len(m.repos)) % max(len(m.repos), 1)
			m.prefill()
		case "right", "l":
			m.repo = (m.repo + 1) % max(len(m.repos), 1)
			m.prefill()
		}
	case fieldTitle:
		m.title, cmd = m.title.Update(k)
	case fieldBody:
		m.body, cmd = m.body.Update(k)
	case fieldLabels:
		m.labelsEdited = true
		m.labels, cmd = m.labels.Update(k)
	}
	return m, cmd
}

func (m Model) submit() (Model, tea.Cmd) {
	repo, ok := m.current()
	if !ok {
		m.err = fmt.Errorf("no repository to create the issue in")
		return m, nil
	}
	if strings.TrimSpace(m.title.Value()) == "" {
		m.err = triage.ErrNoTitle
		return m, nil
	}
	m.sending, m.err = true, nil
	d := triage.IssueDraft{Title: m.title.Value(), Body: m.body.Value(), Labels: splitLabels(m.labels.Value())}
	f, cfg := m.deps.Forge, m.deps.Config
	return m, func() tea.Msg {
		id, err := triage.CreateIssue(context.Background(), f, cfg, repo, d)
		return createdMsg{id: id, err: err}
	}
}

func (m Model) View() string {
	label := func(field int, name string) string {
		if m.focus == field {
			return style.Selected.Render(fmt.Sprintf("> %-10s", name))
		}
		return fmt.Sprintf("  %-10s", name)
	}
	repo := "(none)"
	if r, ok := m.current(); ok {
		repo = r.String()
	}
	if len(m.repos) > 1 {
		repo = "‹ " + repo + " ›"
	}
	lines := []string{style.Title.Render("New issue"), "", label(fieldRepo, "Repository") + " " + termsafe.Line(repo), label(fieldTitle, "Title") + " " + m.title.View(), label(fieldBody, "Body")}
	for _, l := range strings.Split(m.body.View(), "\n") {
		lines = append(lines, "             "+l)
	}
	lines = append(lines, label(fieldLabels, "Labels")+" "+m.labels.View())

	if r, ok := m.current(); ok {
		side := m.deps.Config.PolicyFor(r).Sidebar
		var hints []string
		if side.Milestone != "" {
			hints = append(hints, "milestone "+side.Milestone)
		}
		if side.Assignee != "" {
			hints = append(hints, "assignee "+side.Assignee)
		}
		if side.Project != "" {
			hints = append(hints, "project "+side.Project)
		}
		if len(hints) > 0 {
			lines = append(lines, style.Dim.Render("             "+termsafe.Line(strings.Join(hints, " · "))))
		}
	}
	lines = append(lines, "")
	switch {
	case m.err != nil:
		lines = append(lines, style.Error.Render(termsafe.Line(m.err.Error())))
	case m.sending:
		lines = append(lines, style.Dim.Render("creating"))
	}
	lines = append(lines, style.Dim.Render("tab next field · ←/→ repository · ctrl+s create · esc cancel"))
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, m.width, "…")
	}
	return strings.Join(lines, "\n")
}
