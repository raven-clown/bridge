package reviewview

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/raven-clown/bridge/forge"
	"github.com/raven-clown/bridge/internal/tui/msgs"
	"github.com/raven-clown/bridge/review"
)

type mode int

const (
	modeNormal mode = iota
	modeComment
	modeSummary
	modePick
	modeConfirm
	modeStale
	modeSending
)

var events = []struct {
	name  string
	event forge.ReviewEvent
}{
	{"comment", forge.EventComment},
	{"approve", forge.EventApprove},
	{"request changes", forge.EventRequestChanges},
}

type submittedMsg struct {
	pr  forge.PRID
	err error
}

// target is where the comment being written goes; edit is the index of the
// draft comment being changed, or -1 for a new one.
type compose struct {
	target review.DraftComment
	edit   int
}

func newEditor(placeholder string) textarea.Model {
	ed := textarea.New()
	ed.Placeholder = placeholder
	ed.ShowLineNumbers = false
	ed.SetHeight(4)
	ed.Cursor.SetMode(cursor.CursorStatic)
	return ed
}

func (m Model) Editing() bool { return m.mode == modeComment || m.mode == modeSummary }

// position is the comment target for a row, or why there is none.
func (m Model) position(ri int) (path string, line int, side forge.Side, why string) {
	r := m.rows[ri]
	f := m.files[r.file]
	switch {
	case f.Missing:
		return "", 0, "", "cannot comment: " + f.Reason
	case r.line < 0:
		return "", 0, "", "cannot comment on a file header; move to a line"
	}
	l, s, ok := f.Position(r.line)
	if !ok {
		return "", 0, "", "cannot comment on a hunk header; move to a line"
	}
	return f.Patch.Path, l, s, ""
}

func (m Model) startComment() (Model, tea.Cmd) {
	path, line, side, why := m.position(m.cursor)
	if why != "" {
		m.status, m.rangeStart = why, -1
		return m, nil
	}
	target := review.DraftComment{Path: path, Line: line, Side: side}
	if m.rangeStart >= 0 && m.rangeStart != m.cursor {
		spath, sline, sside, swhy := m.position(m.rangeStart)
		switch {
		case swhy != "":
			m.status = swhy
		case m.rows[m.rangeStart].file != m.rows[m.cursor].file || spath != path:
			m.status = "a range must stay in one file"
		case sside != side:
			m.status = "a range must stay on one side of the diff"
		}
		if m.status != "" {
			m.rangeStart = -1
			return m, nil
		}
		target.StartLine, target.StartSide = min(sline, line), side
		target.Line = max(sline, line)
	}
	m.rangeStart = -1
	m.compose = compose{target: target, edit: -1}
	m.mode = modeComment
	m.editor = newEditor("Write a review comment in Markdown")
	m.editor.SetWidth(max(m.width-6, 20))
	return m, m.editor.Focus()
}

// commentsAt are the draft comments ending on the given file line.
func (m Model) commentsAt(ri int) []int {
	path, line, side, why := m.position(ri)
	if why != "" {
		return nil
	}
	var out []int
	for i, c := range m.draft.Comments {
		if c.Path == path && c.Line == line && c.Side == side {
			out = append(out, i)
		}
	}
	return out
}

func (m Model) saveDraft() Model {
	if err := review.SaveDraft(m.deps.Store, m.draft); err != nil {
		m.status = "could not save the draft: " + err.Error()
	}
	return m
}

func (m Model) commentKey(k tea.KeyMsg) (Model, tea.Cmd) {
	switch k.String() {
	case "v":
		if m.rangeStart >= 0 {
			m.rangeStart = -1
		} else {
			m.rangeStart = m.cursor
			m.status = "range started: move, then c to comment"
		}
	case "c":
		return m.startComment()
	case "e":
		at := m.commentsAt(m.cursor)
		if len(at) == 0 {
			m.status = "no draft comment on this line"
			return m, nil
		}
		m.compose = compose{target: m.draft.Comments[at[0]], edit: at[0]}
		m.mode = modeComment
		m.editor = newEditor("")
		m.editor.SetWidth(max(m.width-6, 20))
		m.editor.SetValue(m.draft.Comments[at[0]].Body)
		return m, m.editor.Focus()
	case "D":
		at := m.commentsAt(m.cursor)
		if len(at) == 0 {
			m.status = "no draft comment on this line"
			return m, nil
		}
		m.draft.Remove(at[0])
		return m.saveDraft(), nil
	case "s":
		return m.startSummary(nil)
	case "S":
		m.mode, m.pick = modePick, 0
	}
	return m, nil
}

func (m Model) startSummary(then *forge.ReviewEvent) (Model, tea.Cmd) {
	m.mode, m.pending = modeSummary, then
	m.editor = newEditor("Summary of the review")
	m.editor.SetWidth(max(m.width-6, 20))
	m.editor.SetValue(m.draft.Summary)
	return m, m.editor.Focus()
}

func (m Model) editorKey(k tea.KeyMsg) (Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.mode, m.pending = modeNormal, nil
		return m, nil
	case "ctrl+s":
		body := m.editor.Value()
		if m.mode == modeSummary {
			m.draft.Summary = body
			m.mode = modeNormal
			m = m.saveDraft()
			if m.pending != nil {
				ev := *m.pending
				m.pending = nil
				return m.choose(ev)
			}
			return m, nil
		}
		c := m.compose.target
		c.Body = body
		if m.compose.edit >= 0 && m.compose.edit < len(m.draft.Comments) {
			m.draft.Comments[m.compose.edit] = c
		} else {
			m.draft.Add(c)
		}
		m.mode = modeNormal
		return m.saveDraft(), nil
	}
	var cmd tea.Cmd
	m.editor, cmd = m.editor.Update(k)
	return m, cmd
}

func (m Model) pickKey(k tea.KeyMsg) (Model, tea.Cmd) {
	switch k.String() {
	case "j", "down":
		m.pick = min(m.pick+1, len(events)-1)
	case "k", "up":
		m.pick = max(m.pick-1, 0)
	case "esc":
		m.mode = modeNormal
	case "enter":
		return m.choose(events[m.pick].event)
	}
	return m, nil
}

// choose asks for a summary when GitHub needs one, then asks to confirm.
func (m Model) choose(ev forge.ReviewEvent) (Model, tea.Cmd) {
	m.event = ev
	if ev != forge.EventApprove && blank(m.draft.Summary) {
		return m.startSummary(&ev)
	}
	if m.deps.Config.Confirm.Review {
		m.mode = modeConfirm
		return m, nil
	}
	return m.send(false)
}

func blank(s string) bool {
	for _, r := range s {
		if r != ' ' && r != '\n' && r != '\t' {
			return false
		}
	}
	return true
}

func (m Model) send(force bool) (Model, tea.Cmd) {
	m.mode = modeSending
	d, f, s, ev := m.draft, m.deps.Forge, m.deps.Store, m.event
	pr := m.pr
	return m, func() tea.Msg {
		return submittedMsg{pr: pr, err: review.Submit(context.Background(), f, s, d, ev, force)}
	}
}

func (m Model) submitted(msg submittedMsg) (Model, tea.Cmd) {
	switch {
	case msg.err == nil:
		m.mode = modeNormal
		return m, tea.Batch(emit(msgs.Status{Text: "review submitted"}), emit(msgs.Back{}))
	case errors.Is(msg.err, forge.ErrStaleHead):
		m.mode = modeStale
	default:
		m.mode = modeNormal
		m.status = "not submitted: " + msg.err.Error()
	}
	return m, nil
}

func (m Model) promptKey(k tea.KeyMsg) (Model, tea.Cmd) {
	switch k.String() {
	case "y":
		return m.send(m.mode == modeStale)
	case "r":
		if m.mode == modeStale {
			m.mode, m.loaded = modeNormal, false
			return m, m.Init()
		}
	case "n", "esc":
		m.mode = modeNormal
	}
	return m, nil
}

func (m Model) prompt() string {
	n := len(m.draft.Comments)
	noun := "comments"
	if n == 1 {
		noun = "comment"
	}
	name := ""
	for _, e := range events {
		if e.event == m.event {
			name = e.name
		}
	}
	switch m.mode {
	case modeConfirm:
		return fmt.Sprintf("submit %s with %d %s? y/n", name, n, noun)
	case modeStale:
		return "the pull request changed: y submits against the new head, r reloads the diff, n cancels"
	case modeSending:
		return "submitting"
	}
	return ""
}
