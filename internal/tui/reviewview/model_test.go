package reviewview

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/raven-clown/bridge/config"
	"github.com/raven-clown/bridge/forge"
	"github.com/raven-clown/bridge/forge/forgetest"
	"github.com/raven-clown/bridge/internal/tui/msgs"
	"github.com/raven-clown/bridge/internal/tui/tuitest"
	"github.com/raven-clown/bridge/review"
	"github.com/raven-clown/bridge/store"
)

func TestMain(m *testing.M) {
	tuitest.NoColor()
	os.Exit(m.Run())
}

var (
	now = time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	pr  = forge.PRID{Repo: forge.RepoID{Host: "github.com", Owner: "o", Name: "a"}, Number: 42}
)

const mainPatch = `@@ -1,3 +1,4 @@ package main
 package main
-import "fmt"
+import (
+	"fmt"

@@ -10,2 +11,2 @@ func main() {
-	fmt.Println("hi")
+	fmt.Println("hello")
 }`

func sampleDiff() forge.Diff {
	return forge.Diff{PR: pr, HeadSHA: "head1", BaseRef: "main", HeadRef: "feat/x", Files: []forge.FilePatch{
		{Path: "main.go", Status: forge.FileModified, Additions: 3, Deletions: 2, Patch: mainPatch},
		{Path: "go.sum", Status: forge.FileModified, Additions: 2, Deletions: 0, Patch: "@@ -1 +1,3 @@\n a v1\n+b v1\n+c v1"},
		{Path: "util.go", Status: forge.FileAdded, Additions: 2, Patch: "@@ -0,0 +1,2 @@\n+package main\n+func util() {}"},
	}}
}

type env struct {
	f   *forgetest.Fake
	s   *store.Store
	d   Deps
	dir string
}

func newEnv(t *testing.T, diff forge.Diff) env {
	t.Helper()
	f := forgetest.New()
	f.Diffs[pr] = diff
	dir := filepath.Join(t.TempDir(), "state")
	s, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(filepath.Join(t.TempDir(), "none.yaml"), func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	return env{f: f, s: s, dir: dir, d: Deps{Forge: f, Store: s, Config: cfg, Now: func() time.Time { return now }}}
}

func (e env) open(width, height int) Model {
	m := New(pr, e.d, width, height)
	return settle(m, m.Init())
}

func settle(m Model, cmd tea.Cmd) Model {
	queue := tuitest.Run(cmd)
	for len(queue) > 0 {
		var next tea.Cmd
		m, next = m.Update(queue[0])
		queue = append(queue[1:], tuitest.Run(next)...)
	}
	return m
}

func press(m Model, keys ...string) (Model, []tea.Msg) {
	var out []tea.Msg
	for _, k := range keys {
		var cmd tea.Cmd
		m, cmd = m.Update(tuitest.Key(k))
		queue := tuitest.Run(cmd)
		for len(queue) > 0 {
			out = append(out, queue[0])
			var next tea.Cmd
			m, next = m.Update(queue[0])
			queue = append(queue[1:], tuitest.Run(next)...)
		}
	}
	return m, out
}

func TestReviewGolden(t *testing.T) {
	m := newEnv(t, sampleDiff()).open(120, 30)
	tuitest.Golden(t, "review_basic", m.View())
}

func TestReviewNarrowHidesFileList(t *testing.T) {
	m := newEnv(t, sampleDiff()).open(80, 30)
	if strings.Contains(m.View(), "Files") {
		t.Fatalf("file list shown at 80 columns:\n%s", m.View())
	}
	m, _ = press(m, "f")
	if !strings.Contains(m.View(), "Files") {
		t.Fatalf("f did not show the file list:\n%s", m.View())
	}
}

func TestReviewHunkAndFileJumps(t *testing.T) {
	m := newEnv(t, sampleDiff()).open(120, 30)
	m, _ = press(m, "]")
	if path, line := m.Cursor(); path != "main.go" || line != 0 {
		t.Fatalf("first ] at %s:%d, want main.go hunk 0", path, line)
	}
	m, _ = press(m, "]")
	if path, line := m.Cursor(); path != "main.go" || line != 6 {
		t.Fatalf("second ] at %s:%d, want main.go hunk at 6", path, line)
	}
	m, _ = press(m, "}")
	if path, line := m.Cursor(); path != "go.sum" || line != HeaderRow {
		t.Fatalf("} at %s:%d, want go.sum header", path, line)
	}
	m, _ = press(m, "{")
	if path, line := m.Cursor(); path != "main.go" || line != HeaderRow {
		t.Fatalf("{ at %s:%d, want main.go header", path, line)
	}
}

func TestReviewExpandCollapsed(t *testing.T) {
	m := newEnv(t, sampleDiff()).open(120, 30)
	if strings.Contains(m.View(), "c v1") || !strings.Contains(m.View(), "lock file, z to expand") {
		t.Fatalf("go.sum not collapsed:\n%s", m.View())
	}
	m, _ = press(m, "}", "z")
	if !strings.Contains(m.View(), "c v1") {
		t.Fatalf("z did not expand go.sum:\n%s", m.View())
	}
}

func TestReviewMissingPatchShown(t *testing.T) {
	d := sampleDiff()
	d.Files = append(d.Files, forge.FilePatch{Path: "logo.png", Status: forge.FileAdded, PatchMissing: true})
	m := newEnv(t, d).open(120, 40)
	if !strings.Contains(m.View(), "logo.png") || !strings.Contains(m.View(), "binary or too large to show") {
		t.Fatalf("view:\n%s", m.View())
	}
	m, _ = press(m, "G", "z")
	if !strings.Contains(m.View(), "GitHub did not send") {
		t.Fatalf("z on a left-out file should explain:\n%s", m.View())
	}
}

func TestReviewViewedPersists(t *testing.T) {
	e := newEnv(t, sampleDiff())
	m := e.open(120, 30)
	_, _ = press(m, "x")
	d, ok, err := review.LoadDraft(e.s, pr)
	if err != nil || !ok || !d.IsViewed("main.go") {
		t.Fatalf("viewed not saved: %+v ok %v err %v", d, ok, err)
	}
	m = e.open(120, 30)
	if !strings.Contains(m.View(), "[viewed]") {
		t.Fatalf("viewed mark lost on reopen:\n%s", m.View())
	}
}

func TestReviewEscapesDiffText(t *testing.T) {
	d := sampleDiff()
	d.Files[0].Patch = "@@ -1 +1 @@\n-a\n+b\x1b]0;pwned\x07"
	d.Files[0].Path = "x\x1b[2J.go"
	m := newEnv(t, d).open(120, 30)
	if strings.ContainsAny(m.View(), "\x07") || strings.Contains(m.View(), "\x1b]") || strings.Contains(m.View(), "\x1b[2J") {
		t.Fatalf("raw control characters: %q", m.View())
	}
}

func TestReviewTruncatedBanner(t *testing.T) {
	d := sampleDiff()
	d.Truncated = true
	m := newEnv(t, d).open(120, 30)
	if !strings.Contains(m.View(), "GitHub lists at most 3000 files") {
		t.Fatalf("view:\n%s", m.View())
	}
}

func TestReviewStaleDraftBanner(t *testing.T) {
	e := newEnv(t, sampleDiff())
	if err := review.SaveDraft(e.s, review.Draft{PR: pr, HeadSHA: "older"}); err != nil {
		t.Fatal(err)
	}
	if m := e.open(120, 30); !strings.Contains(m.View(), "changed since this draft") {
		t.Fatalf("view:\n%s", m.View())
	}
}

func typeText(m Model, s string) Model {
	for _, k := range tuitest.Type(s) {
		m, _ = m.Update(k)
	}
	return m
}

func draftOf(t *testing.T, e env) review.Draft {
	t.Helper()
	d, _, err := review.LoadDraft(e.s, pr)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// Rows of sampleDiff: 0 main.go header, 1 hunk, 2 context (1/1), 3 removed (2),
// 4 added (new 2), 5 added (new 3), 6 context (3/4), 7 hunk, ...

func TestCommentOnLineSaved(t *testing.T) {
	e := newEnv(t, sampleDiff())
	m := e.open(120, 30)
	m, _ = press(m, "j", "j", "j", "j", "c")
	if !m.Editing() {
		t.Fatal("editor not open")
	}
	m = typeText(m, "why parens")
	m, _ = press(m, "ctrl+s")
	d := draftOf(t, e)
	if len(d.Comments) != 1 || d.Comments[0] != (review.DraftComment{Path: "main.go", Line: 2, Side: forge.SideRight, Body: "why parens"}) {
		t.Fatalf("draft = %+v", d.Comments)
	}
	if !strings.Contains(m.View(), "why parens") || !strings.Contains(m.View(), "you (draft)") {
		t.Fatalf("comment not shown inline:\n%s", m.View())
	}
}

func TestRangeComment(t *testing.T) {
	e := newEnv(t, sampleDiff())
	m := e.open(120, 30)
	m, _ = press(m, "j", "j", "j", "j", "v", "j", "j", "c")
	m = typeText(m, "block")
	_, _ = press(m, "ctrl+s")
	c := draftOf(t, e).Comments[0]
	if c.StartLine != 2 || c.Line != 4 || c.Side != forge.SideRight || c.StartSide != forge.SideRight {
		t.Fatalf("range = %+v", c)
	}
}

func TestRangeAcrossSidesRefused(t *testing.T) {
	e := newEnv(t, sampleDiff())
	m := e.open(120, 30)
	m, _ = press(m, "j", "j", "j", "v", "j", "c")
	if m.Editing() || !strings.Contains(m.View(), "one side") {
		t.Fatalf("view:\n%s", m.View())
	}
}

func TestRangeAcrossFilesRefused(t *testing.T) {
	e := newEnv(t, sampleDiff())
	m := e.open(120, 30)
	m, _ = press(m, "j", "j", "j", "j", "v", "}", "}", "j", "j", "c")
	if m.Editing() || !strings.Contains(m.View(), "one file") {
		t.Fatalf("view:\n%s", m.View())
	}
	if len(draftOf(t, e).Comments) != 0 {
		t.Fatal("draft comment added")
	}
}

func TestCommentOnMissingFileRefused(t *testing.T) {
	d := sampleDiff()
	d.Files = append(d.Files, forge.FilePatch{Path: "logo.png", Status: forge.FileAdded, PatchMissing: true})
	m := newEnv(t, d).open(120, 40)
	m, _ = press(m, "G", "c")
	if m.Editing() || !strings.Contains(m.View(), "cannot comment") || !strings.Contains(m.View(), "binary or too large") {
		t.Fatalf("view:\n%s", m.View())
	}
}

func TestEditAndDeleteDraftComment(t *testing.T) {
	e := newEnv(t, sampleDiff())
	m := e.open(120, 30)
	m, _ = press(m, "j", "j", "j", "j", "c")
	m = typeText(m, "first")
	m, _ = press(m, "ctrl+s", "e")
	m = typeText(m, " edited")
	m, _ = press(m, "ctrl+s")
	if got := draftOf(t, e).Comments; len(got) != 1 || got[0].Body != "first edited" {
		t.Fatalf("after edit: %+v", got)
	}
	_, _ = press(m, "D")
	if got := draftOf(t, e).Comments; len(got) != 0 {
		t.Fatalf("after delete: %+v", got)
	}
}

func TestSubmitApproveConfirms(t *testing.T) {
	e := newEnv(t, sampleDiff())
	m := e.open(120, 30)
	m, _ = press(m, "j", "j", "j", "j", "c")
	m = typeText(m, "nit")
	m, _ = press(m, "ctrl+s", "S", "j", "enter")
	if !strings.Contains(m.View(), "submit approve with 1 comment? y/n") {
		t.Fatalf("no confirmation:\n%s", m.View())
	}
	_, out := press(m, "y")
	got := e.f.Reviews[pr]
	if len(got) != 1 || got[0].Event != forge.EventApprove || len(got[0].Comments) != 1 {
		t.Fatalf("reviews = %+v", got)
	}
	back := false
	for _, msg := range out {
		back = back || msg == (msgs.Back{})
	}
	if !back {
		t.Fatalf("no Back after submitting: %+v", out)
	}
}

func TestRequestChangesAsksForSummary(t *testing.T) {
	e := newEnv(t, sampleDiff())
	m := e.open(120, 30)
	m, _ = press(m, "S", "j", "j", "enter")
	if !m.Editing() || !strings.Contains(m.View(), "Summary") {
		t.Fatalf("summary editor not opened:\n%s", m.View())
	}
	if len(e.f.Reviews[pr]) != 0 {
		t.Fatal("submitted without a summary")
	}
	m = typeText(m, "please fix")
	m, _ = press(m, "ctrl+s", "y")
	if got := e.f.Reviews[pr]; len(got) != 1 || got[0].Event != forge.EventRequestChanges || got[0].Body != "please fix" {
		t.Fatalf("reviews = %+v\n%s", got, m.View())
	}
}

func TestSubmitStaleOffersChoice(t *testing.T) {
	e := newEnv(t, sampleDiff())
	stale := review.Draft{PR: pr, HeadSHA: "older", Summary: "s"}
	stale.Add(review.DraftComment{Path: "main.go", Line: 2, Side: forge.SideRight, Body: "keep this"})
	if err := review.SaveDraft(e.s, stale); err != nil {
		t.Fatal(err)
	}
	m := e.open(120, 30)
	m, _ = press(m, "S", "j", "enter", "y")
	if !strings.Contains(m.View(), "the pull request changed") || len(e.f.Reviews[pr]) != 0 {
		t.Fatalf("no stale prompt:\n%s", m.View())
	}
	_, _ = press(m, "y")
	if got := e.f.Reviews[pr]; len(got) != 1 || got[0].CommitSHA != "head1" {
		t.Fatalf("reviews = %+v", got)
	}
}

func TestOrphanedCommentsShownAndDeletable(t *testing.T) {
	e := newEnv(t, sampleDiff())
	d := review.Draft{PR: pr, HeadSHA: "head1"}
	d.Add(review.DraftComment{Path: "main.go", Line: 50, Side: forge.SideRight, Body: "moved away"})
	d.Add(review.DraftComment{Path: "gone.go", Line: 1, Side: forge.SideRight, Body: "file removed"})
	if err := review.SaveDraft(e.s, d); err != nil {
		t.Fatal(err)
	}
	m := e.open(120, 40)
	view := m.View()
	if !strings.Contains(view, "line 50 is no longer in the diff") || !strings.Contains(view, "moved away") ||
		!strings.Contains(view, "gone.go is no longer in this pull request") || !strings.Contains(view, "file removed") {
		t.Fatalf("orphans not shown:\n%s", view)
	}
	m, _ = press(m, "D")
	if got := draftOf(t, e).Comments; len(got) != 1 {
		t.Fatalf("D on the header did not delete an orphan: %+v", got)
	}
	_, _ = press(m, "D")
	if got := draftOf(t, e).Comments; len(got) != 0 {
		t.Fatalf("second D: %+v", got)
	}
}

func TestRebaseDraftClearsStale(t *testing.T) {
	e := newEnv(t, sampleDiff())
	if err := review.SaveDraft(e.s, review.Draft{PR: pr, HeadSHA: "older"}); err != nil {
		t.Fatal(err)
	}
	m := e.open(120, 30)
	if !strings.Contains(m.View(), "u moves the draft") {
		t.Fatalf("banner does not offer u:\n%s", m.View())
	}
	m, _ = press(m, "u")
	if strings.Contains(m.View(), "changed since this draft") || draftOf(t, e).HeadSHA != "head1" {
		t.Fatalf("draft not moved:\n%s", m.View())
	}
}

func TestRangeAcrossHunksRefused(t *testing.T) {
	e := newEnv(t, sampleDiff())
	m := e.open(120, 30)
	m, _ = press(m, "j", "j", "j", "j", "v", "j", "j", "j", "j", "j", "c")
	if m.Editing() || !strings.Contains(m.View(), "one hunk") {
		t.Fatalf("view:\n%s", m.View())
	}
}

func TestBlankCommentNotSaved(t *testing.T) {
	e := newEnv(t, sampleDiff())
	m := e.open(120, 30)
	m, _ = press(m, "j", "j", "j", "j", "c")
	m = typeText(m, "   ")
	m, _ = press(m, "ctrl+s")
	if got := draftOf(t, e).Comments; len(got) != 0 {
		t.Fatalf("blank comment saved: %+v", got)
	}
	if !strings.Contains(m.View(), "empty comment") {
		t.Fatalf("view:\n%s", m.View())
	}
}

func TestTypedCommentSurvivesEsc(t *testing.T) {
	e := newEnv(t, sampleDiff())
	m := e.open(120, 30)
	m, _ = press(m, "j", "j", "j", "j", "c")
	m = typeText(m, "half written")
	m, _ = press(m, "esc")
	if p := draftOf(t, e).Pending; p == nil || p.Body != "half written" {
		t.Fatalf("pending = %+v", p)
	}
	m, _ = press(m, "c")
	if !strings.Contains(m.View(), "half written") {
		t.Fatalf("pending text not restored:\n%s", m.View())
	}
	_, _ = press(m, "ctrl+s")
	d := draftOf(t, e)
	if d.Pending != nil || len(d.Comments) != 1 || d.Comments[0].Body != "half written" {
		t.Fatalf("draft = %+v", d)
	}
}

func TestTypedSummarySurvivesReopen(t *testing.T) {
	e := newEnv(t, sampleDiff())
	m := e.open(120, 30)
	m, _ = press(m, "s")
	_ = typeText(m, "summary so far")
	m = e.open(120, 30)
	m, _ = press(m, "s")
	if !strings.Contains(m.View(), "summary so far") {
		t.Fatalf("summary not restored:\n%s", m.View())
	}
}

func TestUnreadableDraftIsNotOverwritten(t *testing.T) {
	e := newEnv(t, sampleDiff())
	path := filepath.Join(e.dir, "drafts", "review-github.com__o__a__42.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := e.open(120, 30)
	if !strings.Contains(m.View(), "could not be read") {
		t.Fatalf("no warning:\n%s", m.View())
	}
	_, _ = press(m, "x")
	if b, _ := os.ReadFile(path); string(b) != "{not json" {
		t.Fatalf("the unreadable draft was overwritten: %q", b)
	}
}

func TestReviewKeys(t *testing.T) {
	m := newEnv(t, sampleDiff()).open(120, 30)
	_, out := press(m, "q")
	if len(out) != 1 || out[0] != (msgs.Back{}) {
		t.Fatalf("q: %+v", out)
	}
	_, out = press(m, "o")
	if len(out) != 1 || out[0] != (msgs.OpenURL{URL: "https://github.com/o/a/pull/42/files"}) {
		t.Fatalf("o: %+v", out)
	}
}

func TestReviewScrollsToCursor(t *testing.T) {
	d := sampleDiff()
	var b strings.Builder
	b.WriteString("@@ -0,0 +1,80 @@\n")
	for i := range 80 {
		b.WriteString("+line " + string(rune('a'+i%26)) + "\n")
	}
	d.Files = append(d.Files, forge.FilePatch{Path: "long.go", Status: forge.FileAdded, Additions: 80, Patch: strings.TrimSuffix(b.String(), "\n")})
	m := newEnv(t, d).open(120, 20)
	m, _ = press(m, "G")
	view := m.View()
	if n := len(strings.Split(view, "\n")); n > 20 {
		t.Fatalf("%d lines tall", n)
	}
	if !strings.Contains(view, "> ") {
		t.Fatalf("cursor not visible after G:\n%s", view)
	}
}
