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
	f *forgetest.Fake
	s *store.Store
	d Deps
}

func newEnv(t *testing.T, diff forge.Diff) env {
	t.Helper()
	f := forgetest.New()
	f.Diffs[pr] = diff
	s, err := store.Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(filepath.Join(t.TempDir(), "none.yaml"), func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	return env{f: f, s: s, d: Deps{Forge: f, Store: s, Config: cfg, Now: func() time.Time { return now }}}
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
