// Package threadview shows one issue or pull request with its comments,
// review threads and events, and composes replies.
package threadview

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/raven-clown/bridge/forge"
	"github.com/raven-clown/bridge/internal/termsafe"
	"github.com/raven-clown/bridge/internal/tui/msgs"
	"github.com/raven-clown/bridge/internal/tui/style"
	"github.com/raven-clown/bridge/store"
	"github.com/raven-clown/bridge/thread"
)

type Deps struct {
	Forge forge.Forge
	Store *store.Store
	Now   func() time.Time
}

// Both carry the item id: an answer for a thread the user already left must
// not land in the one now open.
type (
	loadedMsg struct {
		id  forge.ItemID
		th  forge.Thread
		err error
	}
	sentMsg struct {
		id  forge.ItemID
		err error
	}
)

// block is one selectable part of the thread. replyTo is what a reply to
// it targets: "" for a new comment, the comment id, or the review thread id.
type block struct {
	lines   []string
	replyTo forge.CommentID
	label   string
	passive bool
}

// step moves the cursor to the next selectable block in direction dir.
func (m Model) step(dir int) int {
	for i := m.cursor + dir; i >= 0 && i < len(m.blocks); i += dir {
		if !m.blocks[i].passive {
			return i
		}
	}
	return m.cursor
}

func (m Model) edge(last bool) int {
	if !last {
		return 0
	}
	for i := len(m.blocks) - 1; i >= 0; i-- {
		if !m.blocks[i].passive {
			return i
		}
	}
	return 0
}

type Model struct {
	id            forge.ItemID
	deps          Deps
	width, height int

	loaded bool
	err    error
	th     forge.Thread
	blocks []block
	cursor int

	composing bool
	replyTo   forge.CommentID
	title     string
	editor    textarea.Model
	sendErr   error
	sending   bool
}

func New(id forge.ItemID, d Deps, width, height int) Model {
	if d.Now == nil {
		d.Now = time.Now
	}
	ed := textarea.New()
	ed.Placeholder = "Write a reply in Markdown"
	ed.ShowLineNumbers = false
	ed.SetHeight(5)
	ed.Cursor.SetMode(cursor.CursorStatic)
	m := Model{id: id, deps: d, editor: ed}
	return m.SetSize(width, height)
}

func (m Model) SetSize(width, height int) Model {
	m.width, m.height = width, height
	m.editor.SetWidth(max(width-2, 10))
	if m.loaded {
		m.blocks = m.build()
	}
	return m
}

func (m Model) Composing() bool { return m.composing }

func (m Model) Init() tea.Cmd {
	d, id := m.deps, m.id
	return func() tea.Msg {
		th, err := thread.Load(context.Background(), d.Forge, d.Store, id)
		return loadedMsg{id: id, th: th, err: err}
	}
}

func emit(msg tea.Msg) tea.Cmd { return func() tea.Msg { return msg } }

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case loadedMsg:
		if msg.id != m.id {
			return m, nil
		}
		m.loaded, m.err, m.th = true, msg.err, msg.th
		if msg.err == nil {
			m.blocks = m.build()
			m.cursor = min(m.cursor, max(len(m.blocks)-1, 0))
		}
		return m, nil
	case sentMsg:
		if msg.id != m.id {
			return m, nil
		}
		m.sending = false
		if msg.err != nil {
			m.sendErr = msg.err
			return m, nil
		}
		m.composing, m.sendErr = false, nil
		m.editor.Reset()
		return m, tea.Batch(emit(msgs.Status{Text: "sent"}), m.Init())
	case tea.KeyMsg:
		if m.composing {
			return m.updateCompose(msg)
		}
		return m.updateKeys(msg)
	}
	return m, nil
}

func (m Model) updateCompose(k tea.KeyMsg) (Model, tea.Cmd) {
	switch k.String() {
	case "ctrl+s":
		if m.sending {
			return m, nil
		}
		m.sending, m.sendErr = true, nil
		d, id, to, body := m.deps, m.id, m.replyTo, m.editor.Value()
		return m, func() tea.Msg {
			_, err := thread.Reply(context.Background(), d.Forge, d.Store, id, to, body)
			return sentMsg{id: id, err: err}
		}
	case "esc":
		m.composing, m.sendErr = false, nil
		if body := m.editor.Value(); strings.TrimSpace(body) != "" {
			if err := m.deps.Store.SaveReplyDraft(m.id, m.replyTo, body); err != nil {
				return m, emit(msgs.Status{Text: "could not save the draft: " + err.Error()})
			}
			return m, emit(msgs.Status{Text: "draft kept"})
		}
		_ = m.deps.Store.DeleteReplyDraft(m.id)
		return m, nil
	}
	before := m.editor.Value()
	var cmd tea.Cmd
	m.editor, cmd = m.editor.Update(k)
	// Write on every change so ctrl+c or a crash never loses typed text.
	if body := m.editor.Value(); body != before {
		if err := m.deps.Store.SaveReplyDraft(m.id, m.replyTo, body); err != nil {
			m.sendErr = fmt.Errorf("could not save the draft: %w", err)
		}
	}
	return m, cmd
}

func (m Model) startCompose(replyTo forge.CommentID, title string) (Model, tea.Cmd) {
	m.composing, m.replyTo, m.title, m.sendErr = true, replyTo, title, nil
	m.editor.Reset()
	if to, body, ok, err := m.deps.Store.LoadReplyDraft(m.id); err == nil && ok {
		m.editor.SetValue(body)
		if to != "" {
			m.replyTo = to
		}
	}
	return m, m.editor.Focus()
}

func (m Model) updateKeys(k tea.KeyMsg) (Model, tea.Cmd) {
	switch k.String() {
	case "j", "down":
		m.cursor = m.step(1)
	case "k", "up":
		m.cursor = m.step(-1)
	case "g", "home":
		m.cursor = m.edge(false)
	case "G", "end":
		m.cursor = m.edge(true)
	case "r":
		if !m.loaded || m.err != nil || len(m.blocks) == 0 {
			return m, nil
		}
		b := m.blocks[m.cursor]
		return m.startCompose(b.replyTo, b.label)
	case "c":
		if !m.loaded || m.err != nil {
			return m, nil
		}
		return m.startCompose("", "New comment")
	case "t":
		return m, emit(msgs.OpenTriage{IDs: []forge.ItemID{m.id}})
	case "e":
		return m, emit(msgs.Status{Text: "editing comments lands in v0.2"})
	case "o":
		if m.th.Item.URL != "" {
			return m, emit(msgs.OpenURL{URL: m.th.Item.URL})
		}
	case "q", "esc":
		return m, emit(msgs.Back{})
	}
	return m, nil
}

func ago(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", max(int(d.Minutes()), 0))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

type markdown struct{ r *glamour.TermRenderer }

// markdownStyle asks the terminal for its background once per process;
// glamour's auto style would ask again for every renderer.
var markdownStyle = sync.OnceValue(func() string {
	switch {
	case lipgloss.ColorProfile() == termenv.Ascii:
		return "notty"
	case lipgloss.HasDarkBackground():
		return "dark"
	}
	return "light"
})

func newMarkdown(width int) markdown {
	r, err := glamour.NewTermRenderer(glamour.WithStandardStyle(markdownStyle()), glamour.WithWordWrap(max(width-4, 20)))
	if err != nil {
		return markdown{}
	}
	return markdown{r: r}
}

func (md markdown) render(body string) []string {
	body = strings.TrimSpace(termsafe.Text(body))
	if body == "" {
		return []string{style.Dim.Render("  (no description)")}
	}
	if md.r != nil {
		if out, err := md.r.Render(body); err == nil {
			return strings.Split(strings.Trim(termsafe.Styled(out), "\n"), "\n")
		}
	}
	return strings.Split(body, "\n")
}

func (m Model) commentBlock(md markdown, c forge.Comment, replyTo forge.CommentID, label string) block {
	head := style.Title.Render(termsafe.Line(c.Author.Login)) + style.Dim.Render(" · "+ago(m.deps.Now(), c.At)+" ago")
	return block{lines: append([]string{head}, md.render(c.Body)...), replyTo: replyTo, label: label}
}

func (m Model) build() []block {
	md := newMarkdown(m.width)
	it, line := m.th.Item, termsafe.Line
	head := []string{style.Title.Render(fmt.Sprintf("%s  %s", it.ID, line(it.Title)))}
	meta := []string{"by " + line(it.Author.Login)}
	if len(it.Labels) > 0 {
		var ls []string
		for _, l := range it.Labels {
			ls = append(ls, line(l.Name))
		}
		meta = append(meta, "labels: "+strings.Join(ls, ", "))
	}
	if it.Milestone != nil {
		meta = append(meta, "milestone: "+line(it.Milestone.Title))
	}
	if len(it.Assignees) > 0 {
		var as []string
		for _, a := range it.Assignees {
			as = append(as, line(a.Login))
		}
		meta = append(meta, "assignees: "+strings.Join(as, ", "))
	}
	for _, p := range it.Projects {
		if p.Column != "" {
			meta = append(meta, "project: "+line(p.Column))
		}
	}
	head = append(head, style.Dim.Render(strings.Join(meta, " · ")))
	blocks := []block{{lines: append(head, md.render(m.th.Body)...), label: "New comment"}}

	type timed struct {
		at time.Time
		b  block
	}
	var stream []timed
	for _, c := range m.th.Comments {
		stream = append(stream, timed{c.At, m.commentBlock(md, c, c.ID, "Reply to "+line(c.Author.Login))})
	}
	for _, e := range m.th.Events {
		text := fmt.Sprintf("· %s %s %s · %s ago", line(e.Actor.Login), e.Kind, line(e.Detail), ago(m.deps.Now(), e.At))
		stream = append(stream, timed{e.At, block{lines: []string{style.Dim.Render(text)}, passive: true}})
	}
	for i := 1; i < len(stream); i++ {
		for j := i; j > 0 && stream[j].at.Before(stream[j-1].at); j-- {
			stream[j], stream[j-1] = stream[j-1], stream[j]
		}
	}
	for _, s := range stream {
		blocks = append(blocks, s.b)
	}
	for _, rt := range m.th.ReviewThreads {
		where := fmt.Sprintf("%s:%d", line(rt.Path), rt.Line)
		if rt.Resolved {
			where += " (resolved)"
		}
		blocks = append(blocks, block{lines: []string{style.Section.Render(where)}, passive: true})
		for _, c := range rt.Comments {
			blocks = append(blocks, m.commentBlock(md, c, rt.ID, "Reply in "+where))
		}
	}
	return blocks
}

func (m Model) View() string {
	if !m.loaded {
		return style.Dim.Render("loading " + m.id.String())
	}
	if m.err != nil {
		msg := m.err.Error()
		if errors.Is(m.err, forge.ErrNotFound) {
			msg = m.id.String() + " was not found"
		}
		return style.Error.Render(termsafe.Line(msg)) + "\n" + style.Dim.Render("q back")
	}

	var lines []string
	cursorLine := 0
	for i, b := range m.blocks {
		if i > 0 {
			lines = append(lines, "")
		}
		if i == m.cursor {
			cursorLine = len(lines)
		}
		for j, l := range b.lines {
			mark := "  "
			if i == m.cursor && j == 0 {
				mark = style.Selected.Render("> ")
			}
			lines = append(lines, mark+l)
		}
	}

	var foot []string
	if m.composing {
		foot = append(foot, style.Section.Render(m.title))
		foot = append(foot, strings.Split(m.editor.View(), "\n")...)
		if m.sendErr != nil {
			foot = append(foot, style.Error.Render(termsafe.Line("not sent: "+m.sendErr.Error())))
		}
		hint := "ctrl+s send · esc keep draft"
		if m.sending {
			hint = "sending"
		}
		foot = append(foot, style.Dim.Render(hint))
	} else {
		foot = append(foot, style.Dim.Render("j/k move · r reply · c comment · t triage · o web · q back"))
	}

	room := m.height - len(foot)
	if room > 0 && len(lines) > room {
		start := max(0, min(cursorLine-2, len(lines)-room))
		lines = lines[start : start+room]
	}
	all := append(lines, foot...)
	for i, l := range all {
		all[i] = ansi.Truncate(l, m.width, "…")
	}
	return strings.Join(all, "\n")
}
