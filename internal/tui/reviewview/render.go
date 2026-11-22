package reviewview

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/raven-clown/bridge/forge"
	"github.com/raven-clown/bridge/internal/termsafe"
	"github.com/raven-clown/bridge/internal/tui/style"
	"github.com/raven-clown/bridge/review"
)

const listWidth = 30

func (m Model) topLines() []string {
	lines := []string{style.Title.Render(fmt.Sprintf("%s  %s into %s", m.pr, termsafe.Line(m.diff.HeadRef), termsafe.Line(m.diff.BaseRef)))}
	if m.diff.Truncated {
		lines = append(lines, style.Banner.Render("GitHub lists at most 3000 files; the rest are not shown"))
	}
	if m.stale {
		lines = append(lines, style.Banner.Render("the pull request changed since this draft was started; comments may point at moved lines"))
	}
	return lines
}

func (m Model) footLines() []string {
	var lines []string
	switch m.mode {
	case modeComment, modeSummary:
		title := "Summary"
		if m.mode == modeComment {
			t := m.compose.target
			where := fmt.Sprintf("%s:%d", t.Path, t.Line)
			if t.StartLine > 0 {
				where = fmt.Sprintf("%s:%d-%d", t.Path, t.StartLine, t.Line)
			}
			title = "Comment on " + termsafe.Line(where)
		}
		lines = append(lines, style.Section.Render(title))
		lines = append(lines, strings.Split(m.editor.View(), "\n")...)
		return append(lines, style.Dim.Render("ctrl+s save · esc cancel"))
	case modePick:
		lines = append(lines, style.Section.Render("Submit the review as"))
		for i, e := range events {
			mark := "  "
			if i == m.pick {
				mark = "> "
			}
			lines = append(lines, mark+e.name)
		}
		return append(lines, style.Dim.Render("enter choose · esc cancel"))
	case modeConfirm, modeStale, modeSending:
		return []string{style.Banner.Render(m.prompt())}
	}
	if m.status != "" {
		lines = append(lines, style.Banner.Render(termsafe.Line(m.status)))
	}
	return append(lines, style.Dim.Render("c comment · v range · s summary · S submit · ]/[ hunk · }/{ file · z fold · x viewed · f files · q back"))
}

// commentLines are the draft comments shown under row ri.
func (m Model) commentLines(ri int) []string {
	var out []string
	for _, i := range m.commentsAt(ri) {
		c := m.draft.Comments[i]
		where := ""
		if c.StartLine > 0 {
			where = fmt.Sprintf(" on %d-%d", c.StartLine, c.Line)
		}
		out = append(out, "           "+style.Selected.Render("┃ you (draft)"+where))
		for _, l := range strings.Split(strings.TrimSpace(c.Body), "\n") {
			out = append(out, "           "+style.Selected.Render("┃ ")+termsafe.Line(l))
		}
	}
	return out
}

// window renders rows from start until the body is full, with draft
// comments under their lines, and reports whether the cursor was drawn.
func (m Model) window(start, h int) ([]string, bool) {
	var body []string
	shown := false
	for i := start; i < len(m.rows) && len(body) < h; i++ {
		mark := "  "
		switch i {
		case m.cursor:
			mark, shown = "> ", true
		case m.rangeStart:
			mark = "v "
		}
		body = append(body, mark+m.renderRow(m.rows[i]))
		body = append(body, m.commentLines(i)...)
	}
	if len(body) > h {
		body = body[:h]
	}
	return body, shown
}

func (m Model) bodyHeight() int {
	return m.height - len(m.topLines()) - len(m.footLines())
}

func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	s = ansi.Truncate(s, w, "…")
	return s + strings.Repeat(" ", max(w-ansi.StringWidth(s), 0))
}

func counts(p forge.FilePatch) string { return fmt.Sprintf("+%d -%d", p.Additions, p.Deletions) }

func (m Model) renderRow(r row) string {
	f := m.files[r.file]
	path := termsafe.Line(f.Patch.Path)
	switch r.line {
	case HeaderRow:
		s := style.Section.Render(path) + "  " + style.Dim.Render(counts(f.Patch))
		if f.Patch.PreviousPath != "" {
			s += style.Dim.Render("  from " + termsafe.Line(f.Patch.PreviousPath))
		}
		if m.draft.IsViewed(f.Patch.Path) {
			s += "  " + style.Dim.Render("[viewed]")
		}
		return s
	case PlaceholderRow:
		if f.Missing {
			return style.Dim.Render("    (" + f.Reason + ")")
		}
		return style.Dim.Render("    (" + f.Reason + ", z to expand)")
	}
	l := f.Lines[r.line]
	num := func(n int) string {
		if n == 0 {
			return "    "
		}
		return fmt.Sprintf("%4d", n)
	}
	text := termsafe.Line(l.Text)
	switch l.Kind {
	case review.HunkHeader:
		return style.Dim.Render("          @@ " + text)
	case review.Added:
		return style.Dim.Render(num(0)+" "+num(l.New)) + " " + style.Selected.Render("+"+text)
	case review.Removed:
		return style.Dim.Render(num(l.Old)+" "+num(0)) + " " + style.Error.Render("-"+text)
	}
	return style.Dim.Render(num(l.Old)+" "+num(l.New)) + "  " + text
}

func (m Model) fileList(h int) []string {
	lines := []string{style.Title.Render("Files")}
	current := -1
	if m.cursor < len(m.rows) {
		current = m.rows[m.cursor].file
	}
	for i, f := range m.files {
		mark := "  "
		if i == current {
			mark = "> "
		}
		viewed := "  "
		if m.draft.IsViewed(f.Patch.Path) {
			viewed = "✓ "
		}
		lines = append(lines, mark+viewed+termsafe.Line(f.Patch.Path)+" "+style.Dim.Render(counts(f.Patch)))
	}
	if len(lines) > h {
		lines = lines[:h]
	}
	return lines
}

func (m Model) View() string {
	if !m.loaded {
		return style.Dim.Render("loading " + m.pr.String())
	}
	if m.err != nil {
		msg := m.err.Error()
		if errors.Is(m.err, forge.ErrNotFound) {
			msg = m.pr.String() + " was not found"
		}
		return style.Error.Render(termsafe.Line(msg)) + "\n" + style.Dim.Render("q back")
	}

	top, foot := m.topLines(), m.footLines()
	h := max(m.bodyHeight(), 1)
	diffWidth := m.width
	var list []string
	if m.showList {
		diffWidth = m.width - listWidth - 1
		list = m.fileList(h)
	}

	// Comment lines take room, so move the start down until the cursor fits.
	start := m.top
	body, shown := m.window(start, h)
	for !shown && start < m.cursor {
		start++
		body, shown = m.window(start, h)
	}
	for len(body) < h && m.showList && len(body) < len(list) {
		body = append(body, "")
	}

	lines := slices.Clone(top)
	for i, b := range body {
		line := fit(b, diffWidth)
		if m.showList {
			left := ""
			if i < len(list) {
				left = list[i]
			}
			line = fit(left, listWidth) + " " + line
		}
		lines = append(lines, line)
	}
	lines = append(lines, foot...)
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, m.width, "…")
	}
	return strings.Join(lines, "\n")
}
