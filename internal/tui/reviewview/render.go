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
	if m.status != "" {
		lines = append(lines, style.Banner.Render(termsafe.Line(m.status)))
	}
	return append(lines, style.Dim.Render("j/k move · ]/[ hunk · }/{ file · z fold · x viewed · f files · o web · q back"))
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

	body := make([]string, 0, h)
	for i := m.top; i < len(m.rows) && i < m.top+h; i++ {
		mark := "  "
		if i == m.cursor {
			mark = "> "
		}
		body = append(body, mark+m.renderRow(m.rows[i]))
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
