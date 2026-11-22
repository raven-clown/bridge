package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"

	"github.com/raven-clown/bridge/forge"
	"github.com/raven-clown/bridge/forge/forgetest"
	"github.com/raven-clown/bridge/inbox"
	"github.com/raven-clown/bridge/store"
)

var (
	repoA = forge.RepoID{Host: "github.com", Owner: "o", Name: "a"}
	now   = time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
)

func ref(n int) forge.ItemID { return forge.ItemID{Repo: repoA, Number: n} }

type harness struct {
	t    *testing.T
	fake *forgetest.Fake
	opts wireOptions
}

func newHarness(t *testing.T, configYAML string) *harness {
	t.Helper()
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfg, []byte(configYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	f := forgetest.New()
	env := map[string]string{"BRIDGE_TOKEN": "ghp_SECRETVALUE"}
	return &harness{t: t, fake: f, opts: wireOptions{
		forge:      f,
		configPath: cfg,
		stateDir:   filepath.Join(dir, "state"),
		env:        func(k string) string { return env[k] },
		now:        func() time.Time { return now },
	}}
}

func (h *harness) run(args ...string) (string, string, error) {
	h.t.Helper()
	var out, errOut bytes.Buffer
	root := newRootWith(&out, h.opts)
	root.SetErr(&errOut)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), errOut.String(), err
}

func TestParseRef(t *testing.T) {
	tests := []struct {
		in      string
		want    forge.ItemID
		wantErr bool
	}{
		{in: "https://github.com/o/a/pull/12", want: ref(12)},
		{in: "https://github.com/o/a/issues/12", want: ref(12)},
		{in: "https://github.com/o/a/issues/12#issuecomment-1", want: ref(12)},
		{in: "o/a#12", want: ref(12)},
		{in: "ghe.corp/t/x#3", wantErr: true},
		{in: "https://github.com/o/a/pull/12/files", want: ref(12)},
		{in: "https://github.com/o/a/pull/12/commits", want: ref(12)},
		{in: "https://github.com/o/a/pull/12/checks?x=1", want: ref(12)},
		{in: "https://gitlab.com/o/a/issues/1", wantErr: true},
		{in: "o/a", wantErr: true},
		{in: "o/a#x", wantErr: true},
		{in: "https://github.com/o/a", wantErr: true},
	}
	for _, tt := range tests {
		got, err := parseRef(tt.in)
		if (err != nil) != tt.wantErr || (!tt.wantErr && got != tt.want) {
			t.Errorf("parseRef(%q) = %v, %v", tt.in, got, err)
		}
	}
}

func TestParseRefOtherHostSaysUnsupported(t *testing.T) {
	_, err := parseRef("https://gitlab.com/o/a/issues/1")
	if err == nil || !strings.Contains(err.Error(), "gitlab.com") || !strings.Contains(err.Error(), "not supported yet") {
		t.Fatalf("got %v", err)
	}
}

func TestWireWarnsAboutMissingRoots(t *testing.T) {
	h := newHarness(t, "roots: [\""+filepath.ToSlash(filepath.Join(t.TempDir(), "nope"))+"\", \"~/src\"]\n")
	_, errOut, err := h.run("inbox")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errOut, "does not exist") || !strings.Contains(errOut, "~ is not expanded") {
		t.Fatalf("stderr:\n%s", errOut)
	}
}

func TestInboxTableAndJSON(t *testing.T) {
	h := newHarness(t, "repos: [o/a]\n")
	h.fake.Incoming = []forge.Item{
		{ID: ref(1), Title: "First issue", Author: forge.User{Login: "x"}, UpdatedAt: now.Add(-2 * time.Hour)},
		{ID: ref(2), IsPR: true, Title: "A pull request", Author: forge.User{Login: "y"}, UpdatedAt: now.Add(-3 * 24 * time.Hour), Checks: &forge.Checks{State: forge.ChecksPassing}},
	}
	h.fake.ReviewRequests = []forge.Item{{ID: ref(3), IsPR: true, Title: "Please review", Author: forge.User{Login: "z"}, UpdatedAt: now}}

	out, _, err := h.run("inbox")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 4 {
		t.Fatalf("want header + 3 rows, got:\n%s", out)
	}
	if !strings.Contains(lines[1], "o/a") || !strings.Contains(lines[1], "#3") || !strings.Contains(lines[1], "review-request") {
		t.Errorf("first row is not the review request:\n%s", out)
	}
	if !strings.Contains(out, "2h") || !strings.Contains(out, "3d") || !strings.Contains(out, "✓") {
		t.Errorf("missing age or checks glyph:\n%s", out)
	}

	out, _, err = h.run("inbox", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var res inbox.Result
	if err := json.Unmarshal([]byte(out), &res); err != nil || len(res.Items) != 3 {
		t.Fatalf("json: %v items %d\n%s", err, len(res.Items), out)
	}
}

func TestInboxFilters(t *testing.T) {
	h := newHarness(t, "repos: [o/a, o/b]\n")
	h.fake.Incoming = []forge.Item{
		{ID: ref(1), Title: "issue a"},
		{ID: forge.ItemID{Repo: forge.RepoID{Host: "github.com", Owner: "o", Name: "b"}, Number: 2}, IsPR: true, Title: "pr b"},
	}
	out, _, err := h.run("inbox", "--repo", "o/b")
	if err != nil || strings.Contains(out, "issue a") || !strings.Contains(out, "pr b") {
		t.Fatalf("repo filter:\n%s %v", out, err)
	}
	out, _, err = h.run("inbox", "--kind", "incoming-issue")
	if err != nil || !strings.Contains(out, "issue a") || strings.Contains(out, "pr b") {
		t.Fatalf("kind filter:\n%s %v", out, err)
	}
	if _, _, err := h.run("inbox", "--kind", "nope"); err == nil {
		t.Fatal("unknown kind: want error")
	}
}

func TestInboxWarnsOnProblems(t *testing.T) {
	h := newHarness(t, "repos: [o/a]\n")
	h.fake.Incoming = []forge.Item{{ID: ref(1), Title: "x"}}
	h.fake.Err["ListMentions"] = errors.New("boom")
	_, errOut, err := h.run("inbox")
	if err != nil || !strings.Contains(errOut, "warning: mentions: boom") {
		t.Fatalf("stderr %q err %v", errOut, err)
	}
}

func TestInboxExitCodeAllFailed(t *testing.T) {
	h := newHarness(t, "repos: [o/a]\n")
	for _, m := range []string{"ListMyPRs", "ListIncoming", "ListReviewRequests", "ListMentions"} {
		h.fake.Err[m] = errors.New("down")
	}
	_, _, err := h.run("inbox")
	var ee *exitError
	if !errors.As(err, &ee) || ee.code != 2 {
		t.Fatalf("got %v", err)
	}
}

func TestInboxOfflineUsesCachedLoginAndItems(t *testing.T) {
	h := newHarness(t, "repos: [o/a]\n")
	h.fake.Incoming = []forge.Item{{ID: ref(1), Title: "Seen before", UpdatedAt: now}}
	if _, _, err := h.run("inbox"); err != nil {
		t.Fatal(err)
	}
	for _, m := range []string{"WhoAmI", "Writable", "ListMyPRs", "ListIncoming", "ListReviewRequests", "ListMentions"} {
		h.fake.Err[m] = errors.New("offline")
	}
	out, errOut, err := h.run("inbox")
	if err != nil || !strings.Contains(out, "Seen before") || !strings.Contains(errOut, "offline") {
		t.Fatalf("err %v\nout:\n%s\nstderr:\n%s", err, out, errOut)
	}
}

func TestOfflineWithoutCachedLoginFails(t *testing.T) {
	h := newHarness(t, "")
	h.fake.Err["WhoAmI"] = errors.New("offline")
	if _, _, err := h.run("inbox"); err == nil || !strings.Contains(err.Error(), "offline") {
		t.Fatalf("got %v", err)
	}
}

func gitRepo(t *testing.T, dir, remote string) {
	t.Helper()
	r, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.CreateRemote(&gitconfig.RemoteConfig{Name: "origin", URLs: []string{remote}}); err != nil {
		t.Fatal(err)
	}
}

func TestRepoCacheRescansWhenRootsChange(t *testing.T) {
	h := newHarness(t, "")
	first, second := t.TempDir(), t.TempDir()
	gitRepo(t, filepath.Join(first, "a"), "https://github.com/o/a.git")
	gitRepo(t, filepath.Join(second, "b"), "https://github.com/o/b.git")

	write := func(root string) {
		yaml := "roots: [\"" + filepath.ToSlash(root) + "\"]\ndepth: 1\n"
		if err := os.WriteFile(h.opts.configPath, []byte(yaml), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	names := func() []string {
		a, err := wire(t.Context(), h.opts)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, r := range a.repos {
			out = append(out, r.Repo.Name)
		}
		return out
	}
	write(first)
	if got := names(); len(got) != 1 || got[0] != "a" {
		t.Fatalf("first roots: %v", got)
	}
	write(second)
	if got := names(); len(got) != 1 || got[0] != "b" {
		t.Fatalf("after changing roots: %v", got)
	}
}

func TestUnreadableRepoCacheRescans(t *testing.T) {
	h := newHarness(t, "")
	root := t.TempDir()
	gitRepo(t, filepath.Join(root, "a"), "https://github.com/o/a.git")
	if err := os.WriteFile(h.opts.configPath, []byte("roots: [\""+filepath.ToSlash(root)+"\"]\ndepth: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(h.opts.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveCache("repos", []string{"an old format"}, now); err != nil {
		t.Fatal(err)
	}
	a, err := wire(t.Context(), h.opts)
	if err != nil || len(a.repos) != 1 {
		t.Fatalf("repos %v err %v", a, err)
	}
}

func TestThreadCommand(t *testing.T) {
	h := newHarness(t, "")
	h.fake.Threads[ref(5)] = forge.Thread{
		Item:     forge.Item{ID: ref(5), Title: "Crash on start", Author: forge.User{Login: "x"}, Labels: []forge.Label{{Name: "bug"}}},
		Body:     "It crashes.",
		Comments: []forge.Comment{{ID: "C1", Author: forge.User{Login: "y"}, At: now.Add(-time.Hour), Body: "Same here."}},
	}
	out, _, err := h.run("thread", "o/a#5")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"o/a#5", "Crash on start", "bug", "It crashes.", "y", "Same here."} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	out, _, err = h.run("thread", "o/a#5", "--json")
	var th forge.Thread
	if err != nil || json.Unmarshal([]byte(out), &th) != nil || th.Body != "It crashes." {
		t.Fatalf("json: %v\n%s", err, out)
	}
}

func TestOutputEscapesControlCharacters(t *testing.T) {
	h := newHarness(t, "repos: [o/a]\n")
	evil := "\x1b]0;pwned\x07\x1b[2J"
	h.fake.Threads[ref(5)] = forge.Thread{
		Item:          forge.Item{ID: ref(5), Title: "t" + evil, Author: forge.User{Login: "x"}, Labels: []forge.Label{{Name: "l" + evil}}},
		Body:          "b" + evil,
		Comments:      []forge.Comment{{Author: forge.User{Login: "y" + evil}, Body: "c" + evil}},
		ReviewThreads: []forge.ReviewThread{{Path: "p" + evil, Comments: []forge.Comment{{Body: "r" + evil}}}},
	}
	h.fake.Incoming = []forge.Item{{ID: ref(6), Title: "i" + evil, Author: forge.User{Login: "z" + evil}}}
	for _, args := range [][]string{{"thread", "o/a#5"}, {"inbox"}} {
		out, _, err := h.run(args...)
		if err != nil {
			t.Fatal(err)
		}
		if strings.ContainsAny(out, "\x1b\x07") {
			t.Fatalf("%v printed a raw control character:\n%q", args, out)
		}
	}
}

func TestThreadCommandShowsHiddenAndReviews(t *testing.T) {
	h := newHarness(t, "")
	h.fake.Threads[ref(5)] = forge.Thread{
		Item:           forge.Item{ID: ref(5), Title: "Long"},
		HiddenComments: 12,
		Comments:       []forge.Comment{{Author: forge.User{Login: "dave"}, At: now, Body: "Ship it", Review: "changes_requested"}},
	}
	out, _, err := h.run("thread", "o/a#5")
	if err != nil || !strings.Contains(out, "12 earlier comments are not shown") || !strings.Contains(out, "dave, changes requested") {
		t.Fatalf("err %v\n%s", err, out)
	}
}

func TestErrorTextEscapes(t *testing.T) {
	got := errorText(errors.New("bad \x1b]0;pwned\x07 thing"))
	if strings.ContainsAny(got, "\x1b\x07") || !strings.HasPrefix(got, "bridge: bad") {
		t.Fatalf("got %q", got)
	}
}

func TestTriageAssignMeResolvesToLogin(t *testing.T) {
	h := newHarness(t, "")
	h.fake.Me = "raven"
	h.fake.Threads[ref(1)] = forge.Thread{Item: forge.Item{ID: ref(1), Assignees: []forge.User{{Login: "raven"}}}}
	h.fake.Threads[ref(2)] = forge.Thread{Item: forge.Item{ID: ref(2)}}
	if _, _, err := h.run("triage", "o/a#2", "--assign", "me"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.run("triage", "o/a#1", "--unassign", "me"); err != nil {
		t.Fatal(err)
	}
	two, _ := h.fake.Thread(t.Context(), ref(2))
	one, _ := h.fake.Thread(t.Context(), ref(1))
	if len(two.Item.Assignees) != 1 || two.Item.Assignees[0].Login != "raven" || len(one.Item.Assignees) != 0 {
		t.Fatalf("#2 %+v, #1 %+v", two.Item.Assignees, one.Item.Assignees)
	}
}

func TestTriageMultipleRefs(t *testing.T) {
	h := newHarness(t, "")
	for _, n := range []int{1, 2} {
		h.fake.Threads[ref(n)] = forge.Thread{Item: forge.Item{ID: ref(n)}}
	}
	out, _, err := h.run("triage", "o/a#1", "https://github.com/o/a/issues/2", "--label", "bug", "--assign", "me")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(out, "ok ") != 2 {
		t.Fatalf("output:\n%s", out)
	}
	for _, n := range []int{1, 2} {
		th, _ := h.fake.Thread(t.Context(), ref(n))
		if len(th.Item.Labels) != 1 || len(th.Item.Assignees) != 1 {
			t.Errorf("#%d: %+v", n, th.Item)
		}
	}
}

func TestTriageFailureExitsOne(t *testing.T) {
	h := newHarness(t, "")
	h.fake.Threads[ref(1)] = forge.Thread{Item: forge.Item{ID: ref(1)}}
	out, _, err := h.run("triage", "o/a#1", "o/a#9", "--close")
	var ee *exitError
	if !errors.As(err, &ee) || ee.code != 1 || !strings.Contains(out, "ok o/a#1") || !strings.Contains(out, "fail o/a#9") {
		t.Fatalf("out:\n%s err %v", out, err)
	}
	if h.fake.States[ref(1)] != forge.StateClosed {
		t.Fatal("#1 not closed")
	}
}

func TestTriageProjectColumn(t *testing.T) {
	h := newHarness(t, "")
	h.fake.Threads[ref(1)] = forge.Thread{Item: forge.Item{ID: ref(1)}}
	h.fake.Projects["o"] = []forge.Project{{ID: "PVT_1", Title: "Board", Fields: []forge.ProjectField{
		{ID: "F_status", Name: "Status", Options: []forge.FieldOption{{ID: "O_todo", Name: "Todo"}, {ID: "O_done", Name: "Done"}}},
	}}}
	if _, _, err := h.run("triage", "o/a#1", "--project", "board/done"); err != nil {
		t.Fatal(err)
	}
	items := h.fake.ProjectItemsBy["PVT_1"]
	if len(items) != 1 || items[0].Fields["F_status"] != "O_done" {
		t.Fatalf("items = %+v", items)
	}
	if _, _, err := h.run("triage", "o/a#1", "--project", "board/Nope"); err == nil {
		t.Fatal("unknown column: want error")
	}
}

func TestTriageNeedsAChange(t *testing.T) {
	h := newHarness(t, "")
	if _, _, err := h.run("triage", "o/a#1"); err == nil {
		t.Fatal("want error")
	}
}

func TestIssueNewUsesSidebarDefaults(t *testing.T) {
	h := newHarness(t, "policy:\n  default:\n    sidebar:\n      labels: [needs-review]\n      assignee: me\n")
	out, _, err := h.run("issue", "new", "o/a", "--title", "Add a thing", "--body", "please")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "https://github.com/o/a/issues/") {
		t.Fatalf("out = %q", out)
	}
	for id, th := range h.fake.Threads {
		if th.Item.Title == "Add a thing" {
			if len(th.Item.Labels) != 1 || th.Item.Labels[0].Name != "needs-review" || th.Item.Assignees[0].Login != "me" || th.Body != "please" {
				t.Fatalf("%v: %+v", id, th)
			}
			return
		}
	}
	t.Fatal("issue not created")
}

func TestIssueNewBodyFile(t *testing.T) {
	h := newHarness(t, "")
	path := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(path, []byte("from file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.run("issue", "new", "o/a", "--title", "T", "--body-file", path); err != nil {
		t.Fatal(err)
	}
	for _, th := range h.fake.Threads {
		if th.Item.Title == "T" && th.Body == "from file" {
			return
		}
	}
	t.Fatal("body not read from file")
}

func TestAuthStatusHidesToken(t *testing.T) {
	h := newHarness(t, "")
	out, _, err := h.run("auth", "status")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "ghp_SECRETVALUE") || !strings.Contains(out, "me") || !strings.Contains(out, "BRIDGE_TOKEN") {
		t.Fatalf("out = %q", out)
	}
}
