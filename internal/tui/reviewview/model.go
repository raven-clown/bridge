// Package reviewview shows a pull request's diff for review: a file list,
// a continuous diff, collapsed files and viewed marks.
package reviewview

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/raven-clown/bridge/config"
	"github.com/raven-clown/bridge/forge"
	"github.com/raven-clown/bridge/internal/tui/msgs"
	"github.com/raven-clown/bridge/review"
	"github.com/raven-clown/bridge/store"
)

type Deps struct {
	Forge  forge.Forge
	Store  *store.Store
	Config config.Config
	Now    func() time.Time
}

// Row positions that are not diff lines.
const (
	HeaderRow      = -1
	PlaceholderRow = -2
)

type row struct {
	file int
	line int
}

type loadedMsg struct {
	pr    forge.PRID
	diff  forge.Diff
	draft review.Draft
	stale bool
	err   error
}

type Model struct {
	pr            forge.PRID
	deps          Deps
	width, height int

	loaded bool
	err    error
	diff   forge.Diff
	files  []review.File
	draft  review.Draft
	stale  bool

	rows     []row
	cursor   int
	top      int
	showList bool
	status   string
}

func New(pr forge.PRID, d Deps, width, height int) Model {
	if d.Now == nil {
		d.Now = time.Now
	}
	return Model{pr: pr, deps: d, width: width, height: height, showList: width >= 90}
}

func (m Model) SetSize(width, height int) Model {
	m.width, m.height = width, height
	m.keepVisible()
	return m
}

func (m Model) Init() tea.Cmd {
	d, pr := m.deps, m.pr
	return func() tea.Msg {
		diff, err := d.Forge.PRDiff(context.Background(), pr)
		if err != nil {
			return loadedMsg{pr: pr, err: err}
		}
		draft, ok, err := review.LoadDraft(d.Store, pr)
		if err != nil || !ok {
			draft = review.Draft{PR: pr, HeadSHA: diff.HeadSHA}
		}
		return loadedMsg{pr: pr, diff: diff, draft: draft, stale: ok && draft.Stale(diff.HeadSHA)}
	}
}

func (m *Model) rebuild() {
	m.rows = m.rows[:0]
	for fi, f := range m.files {
		m.rows = append(m.rows, row{file: fi, line: HeaderRow})
		if f.Collapsed {
			m.rows = append(m.rows, row{file: fi, line: PlaceholderRow})
			continue
		}
		for li := range f.Lines {
			m.rows = append(m.rows, row{file: fi, line: li})
		}
	}
	m.cursor = min(m.cursor, max(len(m.rows)-1, 0))
	m.keepVisible()
}

// Cursor reports the file and line index under the cursor; the index is
// HeaderRow or PlaceholderRow for those rows.
func (m Model) Cursor() (string, int) {
	if m.cursor >= len(m.rows) {
		return "", HeaderRow
	}
	r := m.rows[m.cursor]
	return m.files[r.file].Patch.Path, r.line
}

func emit(msg tea.Msg) tea.Cmd { return func() tea.Msg { return msg } }

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case loadedMsg:
		if msg.pr != m.pr {
			return m, nil
		}
		m.loaded, m.err = true, msg.err
		if msg.err != nil {
			return m, nil
		}
		m.diff, m.draft, m.stale = msg.diff, msg.draft, msg.stale
		m.files = nil
		for _, p := range msg.diff.Files {
			f, err := review.Parse(p)
			if err != nil {
				f = review.File{Patch: p, Missing: true}
			}
			review.Collapse(&f)
			m.files = append(m.files, f)
		}
		m.rebuild()
		return m, nil
	case tea.KeyMsg:
		return m.key(msg)
	}
	return m, nil
}

func (m Model) key(k tea.KeyMsg) (Model, tea.Cmd) {
	if !m.loaded || m.err != nil {
		if k.String() == "q" || k.String() == "esc" {
			return m, emit(msgs.Back{})
		}
		return m, nil
	}
	m.status = ""
	switch k.String() {
	case "j", "down":
		m.cursor = min(m.cursor+1, len(m.rows)-1)
	case "k", "up":
		m.cursor = max(m.cursor-1, 0)
	case "ctrl+d":
		m.cursor = min(m.cursor+m.bodyHeight()/2, len(m.rows)-1)
	case "ctrl+u":
		m.cursor = max(m.cursor-m.bodyHeight()/2, 0)
	case "g", "home":
		m.cursor = 0
	case "G", "end":
		m.cursor = len(m.rows) - 1
	case "]":
		m.cursor = m.find(1, m.isHunk)
	case "[":
		m.cursor = m.find(-1, m.isHunk)
	case "}":
		m.cursor = m.find(1, m.isHeader)
	case "{":
		m.cursor = m.find(-1, m.isHeader)
	case "z":
		fi := m.rows[m.cursor].file
		if m.files[fi].Missing {
			m.status = "GitHub did not send this file's diff"
			break
		}
		m.files[fi].Collapsed = !m.files[fi].Collapsed
		m.rebuild()
		m.cursor = m.headerOf(fi)
	case "x":
		path := m.files[m.rows[m.cursor].file].Patch.Path
		m.draft.ToggleViewed(path)
		if err := review.SaveDraft(m.deps.Store, m.draft); err != nil {
			m.status = "could not save: " + err.Error()
		}
	case "f":
		m.showList = !m.showList
	case "o":
		return m, emit(msgs.OpenURL{URL: fmt.Sprintf("https://%s/%s/%s/pull/%d/files", m.pr.Repo.Host, m.pr.Repo.Owner, m.pr.Repo.Name, m.pr.Number)})
	case "q", "esc":
		return m, emit(msgs.Back{})
	}
	m.keepVisible()
	return m, nil
}

func (m Model) isHunk(r row) bool {
	return r.line >= 0 && m.files[r.file].Lines[r.line].Kind == review.HunkHeader
}

func (m Model) isHeader(r row) bool { return r.line == HeaderRow }

// find moves from the cursor in dir to the next row that matches, or stays.
func (m Model) find(dir int, match func(row) bool) int {
	for i := m.cursor + dir; i >= 0 && i < len(m.rows); i += dir {
		if match(m.rows[i]) {
			return i
		}
	}
	return m.cursor
}

func (m Model) headerOf(fi int) int {
	for i, r := range m.rows {
		if r.file == fi && r.line == HeaderRow {
			return i
		}
	}
	return 0
}

func (m *Model) keepVisible() {
	h := m.bodyHeight()
	if h <= 0 {
		return
	}
	if m.cursor < m.top {
		m.top = m.cursor
	}
	if m.cursor >= m.top+h {
		m.top = m.cursor - h + 1
	}
	m.top = max(0, min(m.top, max(len(m.rows)-h, 0)))
}
