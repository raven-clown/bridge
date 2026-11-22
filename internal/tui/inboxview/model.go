// Package inboxview is the inbox screen: sections by kind, selection,
// filtering and the keys that open threads and triage.
package inboxview

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/raven-clown/bridge/forge"
	"github.com/raven-clown/bridge/inbox"
	"github.com/raven-clown/bridge/internal/termsafe"
	"github.com/raven-clown/bridge/internal/tui/msgs"
	"github.com/raven-clown/bridge/internal/tui/style"
)

var sectionTitles = map[forge.InboxKind]string{
	forge.OwnPR:         "Your pull requests",
	forge.IncomingPR:    "Pull requests to you",
	forge.IncomingIssue: "Issues",
	forge.ReviewRequest: "Review requests",
	forge.Mention:       "Mentions",
	forge.PushedBranch:  "Pushed branches",
}

type Model struct {
	width, height int
	now           func() time.Time

	res    inbox.Result
	cached bool

	items    []inbox.Item
	sections []int
	cursor   int

	selected map[string]bool
	muted    map[string]bool

	filter    string
	filtering bool
	input     textinput.Model
}

func New(width, height int, now func() time.Time) Model {
	in := textinput.New()
	in.Prompt = "/"
	return Model{width: width, height: height, now: now, selected: map[string]bool{}, muted: map[string]bool{}, input: in}
}

func key(it inbox.Item) string { return it.Kind.String() + " " + it.ID.String() + " " + it.Branch }

func (m Model) SetSize(width, height int) Model {
	m.width, m.height = width, height
	return m
}

// SetResult replaces the items, keeping the cursor on the same item when it
// is still there.
func (m Model) SetResult(res inbox.Result, cached bool) Model {
	var current string
	if it, ok := m.Current(); ok {
		current = key(it)
	}
	m.res, m.cached = res, cached
	m.rebuild()
	for i, it := range m.items {
		if key(it) == current {
			m.cursor = i
		}
	}
	return m
}

func (m *Model) rebuild() {
	f := strings.ToLower(m.filter)
	var kept []inbox.Item
	for _, it := range m.res.Items {
		if m.muted[key(it)] {
			continue
		}
		if f != "" && !strings.Contains(strings.ToLower(it.Repo.String()+" "+it.Title), f) {
			continue
		}
		kept = append(kept, it)
	}
	var order []forge.InboxKind
	groups := map[forge.InboxKind][]inbox.Item{}
	for _, it := range kept {
		if _, ok := groups[it.Kind]; !ok {
			order = append(order, it.Kind)
		}
		groups[it.Kind] = append(groups[it.Kind], it)
	}
	m.items, m.sections = nil, nil
	for _, k := range order {
		m.sections = append(m.sections, len(m.items))
		m.items = append(m.items, groups[k]...)
	}
	for k := range m.selected {
		found := false
		for _, it := range m.items {
			if key(it) == k {
				found = true
			}
		}
		if !found {
			delete(m.selected, k)
		}
	}
	m.cursor = min(m.cursor, max(len(m.items)-1, 0))
}

func (m Model) Current() (inbox.Item, bool) {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return inbox.Item{}, false
	}
	return m.items[m.cursor], true
}

// Filtering reports whether the filter box has the keyboard.
func (m Model) Filtering() bool { return m.filtering }

func emit(msg tea.Msg) tea.Cmd { return func() tea.Msg { return msg } }

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	if m.filtering {
		switch k.Type {
		case tea.KeyEnter:
			m.filtering, m.filter = false, m.input.Value()
			m.input.Blur()
			m.rebuild()
			return m, nil
		case tea.KeyEsc:
			m.filtering, m.filter = false, ""
			m.input.Blur()
			m.input.SetValue("")
			m.rebuild()
			return m, nil
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}

	switch k.String() {
	case "j", "down":
		m.cursor = min(m.cursor+1, max(len(m.items)-1, 0))
	case "k", "up":
		m.cursor = max(m.cursor-1, 0)
	case "g", "home":
		m.cursor = 0
	case "G", "end":
		m.cursor = max(len(m.items)-1, 0)
	case "tab":
		for _, s := range m.sections {
			if s > m.cursor {
				m.cursor = s
				return m, nil
			}
		}
		m.cursor = 0
	case " ":
		if it, ok := m.Current(); ok && it.Kind != forge.PushedBranch {
			if m.selected[key(it)] {
				delete(m.selected, key(it))
			} else {
				m.selected[key(it)] = true
			}
		}
	case "esc":
		if m.filter != "" {
			m.filter = ""
			m.input.SetValue("")
			m.rebuild()
		}
		m.selected = map[string]bool{}
	case "/":
		m.filtering = true
		m.input.SetValue(m.filter)
		return m, m.input.Focus()
	case "enter":
		it, ok := m.Current()
		if !ok {
			return m, nil
		}
		if it.Kind == forge.PushedBranch {
			return m, emit(msgs.Status{Text: fmt.Sprintf("%s has no pull request yet", termsafe.Line(it.Branch))})
		}
		return m, emit(msgs.OpenThread{ID: it.ID})
	case "d":
		it, ok := m.Current()
		if !ok {
			return m, nil
		}
		if !isPR(it) {
			return m, emit(msgs.Status{Text: "d works on pull requests"})
		}
		return m, emit(msgs.OpenReview{ID: it.ID})
	case "t":
		ids := m.targets()
		if len(ids) == 0 {
			return m, nil
		}
		return m, emit(msgs.OpenTriage{IDs: ids})
	case "n":
		return m, emit(msgs.NewIssue{})
	case "R":
		return m, emit(msgs.Refresh{})
	case "o":
		if it, ok := m.Current(); ok && it.URL != "" {
			return m, emit(msgs.OpenURL{URL: it.URL})
		}
	case "x":
		if it, ok := m.Current(); ok {
			m.muted[key(it)] = true
			m.rebuild()
			return m, emit(msgs.Status{Text: "muted until bridge restarts"})
		}
	}
	return m, nil
}

// isPR uses the kind, and the checks GitHub reports only for pull requests.
func isPR(it inbox.Item) bool {
	switch it.Kind {
	case forge.OwnPR, forge.IncomingPR, forge.ReviewRequest:
		return true
	}
	return it.Checks != nil
}

// targets is the selection in display order, or the item under the cursor.
func (m Model) targets() []forge.ItemID {
	var ids []forge.ItemID
	for _, it := range m.items {
		if m.selected[key(it)] {
			ids = append(ids, it.ID)
		}
	}
	if len(ids) == 0 {
		if it, ok := m.Current(); ok && it.Kind != forge.PushedBranch {
			ids = append(ids, it.ID)
		}
	}
	return ids
}

func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	s = ansi.Truncate(s, w, "…")
	return s + strings.Repeat(" ", max(w-ansi.StringWidth(s), 0))
}

func age(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", max(int(d.Minutes()), 0))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

func urgency(u inbox.Urgency) string {
	switch {
	case u >= inbox.UrgencyUnanswered:
		return style.Error.Render("!!")
	case u >= inbox.UrgencyUntriagedIssue:
		return style.Banner.Render("! ")
	}
	return "  "
}

func checks(c *forge.Checks) string {
	if c == nil {
		return " "
	}
	switch c.State {
	case forge.ChecksPassing:
		return lipglossGood("✓")
	case forge.ChecksFailing:
		return style.Error.Render("✗")
	case forge.ChecksPending:
		return style.Dim.Render("◌")
	case forge.ChecksAwaitingApproval:
		return style.Banner.Render("⏸")
	}
	return " "
}

func lipglossGood(s string) string { return style.Dim.Foreground(style.Good).Render(s) }

func ref(it inbox.Item) string {
	if it.Kind == forge.PushedBranch {
		return "branch"
	}
	return fmt.Sprintf("#%d", it.ID.Number)
}

func (m Model) row(it inbox.Item, cursor bool) string {
	repoW, refW, authorW := 22, 8, 12
	if m.width < 90 {
		repoW, authorW = 14, 0
	}
	cur := " "
	if cursor {
		cur = ">"
	}
	sel := " "
	if m.selected[key(it)] {
		sel = style.Selected.Render("*")
	}
	reply := "    "
	if it.Unanswered > 0 {
		reply = fit(fmt.Sprintf("↩%d", it.Unanswered), 4)
	}
	// marker block 5, then repo, ref, checks 1, reply 4, age 4, and one
	// space between each of the seven columns.
	fixed := 5 + repoW + refW + 1 + 4 + 4 + 6
	if authorW > 0 {
		fixed += authorW + 1
	}
	titleW := max(m.width-fixed, 8)

	parts := []string{
		cur + sel + " " + urgency(it.Urgency),
		fit(termsafe.Line(it.Repo.String()), repoW),
		fit(ref(it), refW),
		fit(termsafe.Line(it.Title), titleW),
	}
	if authorW > 0 {
		parts = append(parts, fit(termsafe.Line(it.Author.Login), authorW))
	}
	parts = append(parts, checks(it.Checks), reply, fit(age(m.now(), it.UpdatedAt), 4))
	line := strings.Join(parts, " ")
	if cursor {
		return style.Cursor.Render(ansi.Strip(line))
	}
	return line
}

func (m Model) View() string {
	var top []string
	const maxProblems = 3
	problems := m.res.Problems
	if len(problems) > maxProblems {
		problems = problems[:maxProblems]
	}
	for _, p := range problems {
		where := p.Source
		if p.Repo != nil {
			where += " " + p.Repo.String()
		}
		line := fmt.Sprintf("! %s: %v", where, p.Err)
		if p.RetryAt != nil {
			line += " (retry after " + p.RetryAt.Local().Format("15:04") + ")"
		}
		top = append(top, style.Error.Render(termsafe.Line(line)))
	}
	if extra := len(m.res.Problems) - len(problems); extra > 0 {
		top = append(top, style.Error.Render(fmt.Sprintf("! and %d more problems (bridge inbox shows them all)", extra)))
	}
	for _, t := range m.res.Truncated {
		top = append(top, style.Banner.Render(t+" has more than 1000 results; showing the first 1000"))
	}
	if m.cached {
		top = append(top, style.Banner.Render("showing cached results from "+age(m.now(), m.res.At)+" ago, refreshing"))
	}

	var list []string
	cursorLine := 0
	if len(m.items) == 0 {
		msg := "Nothing is waiting for you."
		if m.filter != "" {
			msg = "Nothing matches the filter."
		}
		list = append(list, "", style.Dim.Render("  "+msg))
	}
	next := 0
	for i, it := range m.items {
		if next < len(m.sections) && m.sections[next] == i {
			n := len(m.items) - i
			if next+1 < len(m.sections) {
				n = m.sections[next+1] - i
			}
			if i > 0 {
				list = append(list, "")
			}
			list = append(list, style.Section.Render(fmt.Sprintf("%s (%d)", sectionTitles[it.Kind], n)))
			next++
		}
		if i == m.cursor {
			cursorLine = len(list)
		}
		list = append(list, m.row(it, i == m.cursor))
	}

	var foot []string
	if m.filtering {
		foot = append(foot, m.input.View())
	} else if m.filter != "" {
		foot = append(foot, style.Dim.Render("filter: "+termsafe.Line(m.filter)+" (esc to clear)"))
	}
	if n := len(m.selected); n > 0 {
		foot = append(foot, style.Selected.Render(fmt.Sprintf("%d selected", n)))
	}
	foot = append(foot, style.Dim.Render("enter open · d diff · t triage · space select · / filter · n issue · x mute · R refresh · q quit"))

	room := m.height - len(top) - len(foot)
	if room > 0 && len(list) > room {
		start := max(0, min(cursorLine-room/2, len(list)-room))
		list = list[start : start+room]
	}

	lines := append(append(top, list...), foot...)
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, m.width, "…")
	}
	return strings.Join(lines, "\n")
}
