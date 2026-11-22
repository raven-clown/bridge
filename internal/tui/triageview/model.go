// Package triageview is the side panel that labels, assigns, sets a
// milestone, moves on a project and closes or reopens one or many items.
package triageview

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/raven-clown/bridge/config"
	"github.com/raven-clown/bridge/forge"
	"github.com/raven-clown/bridge/internal/termsafe"
	"github.com/raven-clown/bridge/internal/tui/msgs"
	"github.com/raven-clown/bridge/internal/tui/style"
	"github.com/raven-clown/bridge/store"
	"github.com/raven-clown/bridge/triage"
)

type Deps struct {
	Forge  forge.Forge
	Store  *store.Store
	Config config.Config
}

type (
	metaMsg struct {
		repo forge.RepoID
		meta forge.RepoMeta
		err  error
	}
	appliedMsg []triage.Result
)

const (
	rowLabels = iota
	rowMilestone
	rowAssignees
	rowProject
	rowState
)

var rowNames = []string{"Labels", "Milestone", "Assignees", "Project", "State"}

const (
	markNone = iota
	markAdd
	markRemove
)

type pickerKind int

const (
	pickLabels pickerKind = iota
	pickMilestone
	pickAssignees
	pickProject
	pickColumn
)

type picker struct {
	kind    pickerKind
	options []string
	cursor  int
	filter  string
}

func (p picker) visible() []string {
	if p.filter == "" {
		return p.options
	}
	var out []string
	for _, o := range p.options {
		if strings.Contains(strings.ToLower(o), strings.ToLower(p.filter)) {
			out = append(out, o)
		}
	}
	return out
}

const clearMilestone = "(clear)"

type Model struct {
	ids           []forge.ItemID
	deps          Deps
	width, height int

	metas   map[forge.RepoID]forge.RepoMeta
	metaErr error

	row    int
	picker *picker

	labels    map[string]int
	assignees map[string]int
	milestone string
	project   *forge.Project
	column    *forge.FieldOption
	state     *forge.ItemState

	confirming bool
	applying   bool
	results    []triage.Result
}

func New(ids []forge.ItemID, d Deps, width, height int) Model {
	return Model{
		ids: ids, deps: d, width: width, height: height,
		metas: map[forge.RepoID]forge.RepoMeta{}, labels: map[string]int{}, assignees: map[string]int{},
	}
}

func (m Model) SetSize(width, height int) Model {
	m.width, m.height = width, height
	return m
}

func (m Model) repos() []forge.RepoID {
	var out []forge.RepoID
	for _, id := range m.ids {
		if !slices.Contains(out, id.Repo) {
			out = append(out, id.Repo)
		}
	}
	return out
}

// Init loads fresh metadata for each repository, falling back to the cache
// when the forge cannot be reached.
func (m Model) Init() tea.Cmd {
	var cmds []tea.Cmd
	d := m.deps
	for _, r := range m.repos() {
		cmds = append(cmds, func() tea.Msg {
			meta, err := d.Forge.RepoMeta(context.Background(), r)
			if err == nil {
				_ = d.Store.SaveMeta(r, meta)
				return metaMsg{repo: r, meta: meta}
			}
			if cached, ok, _ := d.Store.LoadMeta(r); ok {
				return metaMsg{repo: r, meta: cached}
			}
			return metaMsg{repo: r, err: err}
		})
	}
	return tea.Batch(cmds...)
}

func (m Model) loaded() bool { return len(m.metas) == len(m.repos()) && m.metaErr == nil }

// common keeps the names present in every repository's metadata.
func (m Model) common(names func(forge.RepoMeta) []string) []string {
	var out []string
	for i, r := range m.repos() {
		got := names(m.metas[r])
		if i == 0 {
			out = slices.Clone(got)
			continue
		}
		out = slices.DeleteFunc(out, func(s string) bool { return !slices.Contains(got, s) })
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func (m Model) projectMeta() forge.RepoMeta { return m.metas[m.repos()[0]] }

func emit(msg tea.Msg) tea.Cmd { return func() tea.Msg { return msg } }

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case metaMsg:
		if !slices.Contains(m.repos(), msg.repo) {
			return m, nil
		}
		if msg.err != nil {
			m.metaErr = fmt.Errorf("%s: %w", msg.repo, msg.err)
			return m, nil
		}
		m.metas[msg.repo] = msg.meta
		return m, nil
	case appliedMsg:
		m.applying, m.results = false, msg
		return m, emit(msgs.Status{Text: "triage applied"})
	case tea.KeyMsg:
		return m.key(msg)
	}
	return m, nil
}

func (m Model) key(k tea.KeyMsg) (Model, tea.Cmd) {
	if m.results != nil || m.metaErr != nil {
		if k.String() == "esc" || k.String() == "enter" || k.String() == "q" {
			return m, emit(msgs.TriageDone{})
		}
		return m, nil
	}
	if m.applying {
		return m, nil
	}
	if m.confirming {
		switch k.String() {
		case "y":
			m.confirming = false
			return m.apply()
		case "n", "esc":
			m.confirming = false
		}
		return m, nil
	}
	if m.picker != nil {
		return m.pickerKey(k)
	}
	switch k.String() {
	case "j", "down":
		m.row = min(m.row+1, len(rowNames)-1)
	case "k", "up":
		m.row = max(m.row-1, 0)
	case "esc", "q":
		return m, emit(msgs.TriageDone{})
	case "a":
		if m.change().State != nil && *m.change().State == forge.StateClosed && m.deps.Config.Confirm.Close {
			m.confirming = true
			return m, nil
		}
		return m.apply()
	case "enter":
		if !m.loaded() {
			return m, nil
		}
		return m.openRow(), nil
	}
	return m, nil
}

func (m Model) openRow() Model {
	switch m.row {
	case rowLabels:
		m.picker = &picker{kind: pickLabels, options: m.common(func(r forge.RepoMeta) []string {
			var out []string
			for _, l := range r.Labels {
				out = append(out, l.Name)
			}
			return out
		})}
	case rowMilestone:
		opts := m.common(func(r forge.RepoMeta) []string {
			var out []string
			for _, ms := range r.Milestones {
				if ms.Open {
					out = append(out, ms.Title)
				}
			}
			return out
		})
		m.picker = &picker{kind: pickMilestone, options: append(opts, clearMilestone)}
	case rowAssignees:
		m.picker = &picker{kind: pickAssignees, options: m.common(func(r forge.RepoMeta) []string {
			var out []string
			for _, u := range r.Assignable {
				out = append(out, u.Login)
			}
			return out
		})}
	case rowProject:
		meta := m.projectMeta()
		if meta.ProjectsErr != nil {
			return m
		}
		var opts []string
		for _, p := range meta.Projects {
			opts = append(opts, p.Title)
		}
		m.picker = &picker{kind: pickProject, options: opts}
	case rowState:
		switch {
		case m.state == nil:
			s := forge.StateClosed
			m.state = &s
		case *m.state == forge.StateClosed:
			s := forge.StateOpen
			m.state = &s
		default:
			m.state = nil
		}
	}
	return m
}

func statusField(p *forge.Project) *forge.ProjectField {
	for i := range p.Fields {
		if strings.EqualFold(p.Fields[i].Name, "Status") {
			return &p.Fields[i]
		}
	}
	return nil
}

func (m Model) pickerKey(k tea.KeyMsg) (Model, tea.Cmd) {
	p := *m.picker
	vis := p.visible()
	switch k.Type {
	case tea.KeyEsc:
		m.picker = nil
		return m, nil
	case tea.KeyDown:
		p.cursor = min(p.cursor+1, max(len(vis)-1, 0))
	case tea.KeyUp:
		p.cursor = max(p.cursor-1, 0)
	case tea.KeyBackspace:
		if p.filter != "" {
			p.filter = p.filter[:len(p.filter)-1]
			p.cursor = 0
		}
	case tea.KeySpace:
		if len(vis) > 0 && (p.kind == pickLabels || p.kind == pickAssignees) {
			marks := m.labels
			if p.kind == pickAssignees {
				marks = m.assignees
			}
			name := vis[p.cursor]
			marks[name] = (marks[name] + 1) % 3
		}
	case tea.KeyEnter:
		if len(vis) == 0 {
			m.picker = nil
			return m, nil
		}
		choice := vis[p.cursor]
		switch p.kind {
		case pickMilestone:
			m.milestone = choice
		case pickProject:
			meta := m.projectMeta()
			for i := range meta.Projects {
				if meta.Projects[i].Title == choice {
					proj := meta.Projects[i]
					m.project, m.column = &proj, nil
					if f := statusField(&proj); f != nil && len(f.Options) > 0 {
						var cols []string
						for _, o := range f.Options {
							cols = append(cols, o.Name)
						}
						m.picker = &picker{kind: pickColumn, options: cols}
						return m, nil
					}
				}
			}
		case pickColumn:
			if f := statusField(m.project); f != nil {
				for i := range f.Options {
					if f.Options[i].Name == choice {
						opt := f.Options[i]
						m.column = &opt
					}
				}
			}
		}
		m.picker = nil
		return m, nil
	case tea.KeyRunes:
		p.filter += string(k.Runes)
		p.cursor = 0
	}
	m.picker = &p
	return m, nil
}

func marked(marks map[string]int, want int) []string {
	var out []string
	for name, mk := range marks {
		if mk == want {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}

func (m Model) change() triage.Change {
	ch := triage.Change{
		AddLabels: marked(m.labels, markAdd), RemoveLabels: marked(m.labels, markRemove),
		AddAssignees: marked(m.assignees, markAdd), RemoveAssignees: marked(m.assignees, markRemove),
		State: m.state,
	}
	switch m.milestone {
	case "":
	case clearMilestone:
		ch.ClearMilestone = true
	default:
		name := m.milestone
		ch.Milestone = &name
	}
	if m.project != nil {
		mv := &triage.ProjectMove{ProjectID: m.project.ID}
		if f := statusField(m.project); f != nil && m.column != nil {
			mv.FieldID, mv.OptionID = f.ID, m.column.ID
		}
		ch.Project = mv
	}
	return ch
}

func (m Model) apply() (Model, tea.Cmd) {
	ch := m.change()
	if len(ch.AddLabels)+len(ch.RemoveLabels)+len(ch.AddAssignees)+len(ch.RemoveAssignees) == 0 &&
		ch.Milestone == nil && !ch.ClearMilestone && ch.State == nil && ch.Project == nil {
		return m, emit(msgs.Status{Text: "nothing to apply yet"})
	}
	m.applying = true
	f, ids := m.deps.Forge, m.ids
	return m, func() tea.Msg { return appliedMsg(triage.Apply(context.Background(), f, ids, ch)) }
}

func (m Model) noProjects() bool {
	return m.loaded() && errors.Is(m.projectMeta().ProjectsErr, forge.ErrProjectsScope)
}

func (m Model) rowValue(row int) string {
	none := style.Dim.Render("no change")
	signs := func(marks map[string]int) string {
		var parts []string
		for _, n := range marked(marks, markAdd) {
			parts = append(parts, "+"+n)
		}
		for _, n := range marked(marks, markRemove) {
			parts = append(parts, "-"+n)
		}
		if len(parts) == 0 {
			return none
		}
		return termsafe.Line(strings.Join(parts, " "))
	}
	switch row {
	case rowLabels:
		return signs(m.labels)
	case rowAssignees:
		return signs(m.assignees)
	case rowMilestone:
		if m.milestone == "" {
			return none
		}
		return termsafe.Line(m.milestone)
	case rowProject:
		if m.noProjects() {
			return style.Banner.Render("no access")
		}
		if m.project == nil {
			return none
		}
		v := m.project.Title
		if m.column != nil {
			v += " / " + m.column.Name
		}
		return termsafe.Line(v)
	case rowState:
		switch {
		case m.state == nil:
			return none
		case *m.state == forge.StateClosed:
			return "close"
		}
		return "reopen"
	}
	return ""
}

func (m Model) View() string {
	n := len(m.ids)
	noun := "items"
	if n == 1 {
		noun = "item"
	}
	lines := []string{style.Title.Render(fmt.Sprintf("Triage %d %s", n, noun))}

	switch {
	case m.metaErr != nil:
		lines = append(lines, style.Error.Render(termsafe.Line(m.metaErr.Error())), style.Dim.Render("esc close"))
	case m.results != nil:
		for _, r := range m.results {
			if r.Err != nil {
				lines = append(lines, style.Error.Render(termsafe.Line(fmt.Sprintf("fail %s: %v", r.Item, r.Err))))
			} else {
				lines = append(lines, "ok "+r.Item.String())
			}
		}
		lines = append(lines, style.Dim.Render("esc close"))
	case !m.loaded():
		lines = append(lines, style.Dim.Render("loading labels and milestones"))
	default:
		for i, name := range rowNames {
			mark := "  "
			if i == m.row {
				mark = style.Selected.Render("> ")
			}
			lines = append(lines, fmt.Sprintf("%s%-10s %s", mark, name, m.rowValue(i)))
		}
		if m.noProjects() {
			lines = append(lines, style.Banner.Render(forge.ErrProjectsScope.Error()))
		}
		if p := m.picker; p != nil {
			lines = append(lines, "")
			if p.filter != "" {
				lines = append(lines, style.Dim.Render("filter: "+termsafe.Line(p.filter)))
			}
			vis := p.visible()
			if len(vis) == 0 {
				lines = append(lines, style.Dim.Render("  nothing to pick"))
			}
			// Room left after the lines so far, the hint and the border.
			start, end := 0, len(vis)
			if room := m.height - len(lines) - 1 - 2; room > 0 && len(vis) > room {
				start = max(0, min(p.cursor-room/2, len(vis)-room))
				end = start + room
			}
			for i := start; i < end; i++ {
				o := vis[i]
				cur := "  "
				if i == p.cursor {
					cur = style.Selected.Render("> ")
				}
				box := ""
				switch p.kind {
				case pickLabels, pickAssignees:
					marks := m.labels
					if p.kind == pickAssignees {
						marks = m.assignees
					}
					box = [...]string{"[ ] ", "[+] ", "[-] "}[marks[o]]
				}
				lines = append(lines, cur+box+termsafe.Line(o))
			}
			hint := "enter pick · esc done · type to filter"
			if p.kind == pickLabels || p.kind == pickAssignees {
				hint = "space add/remove · esc done · type to filter"
			}
			lines = append(lines, style.Dim.Render(hint))
		} else if m.confirming {
			lines = append(lines, "", style.Banner.Render(fmt.Sprintf("close %d %s? y/n", n, noun)))
		} else if m.applying {
			lines = append(lines, "", style.Dim.Render("applying"))
		} else {
			lines = append(lines, "", style.Dim.Render("enter pick · a apply · esc cancel"))
		}
	}

	// The panel's border takes 2 columns and its padding 2 more.
	content := max(m.width-4, 10)
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, content, "…")
	}
	return style.Panel.Width(content + 2).Render(strings.Join(lines, "\n"))
}
