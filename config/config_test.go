package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/raven-clown/bridge/forge"
)

func noEnv(string) string { return "" }

func envMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func repo(s string) forge.RepoID {
	r, err := forge.ParseRepoID(s)
	if err != nil {
		panic(err)
	}
	return r
}

func TestLoadFull(t *testing.T) {
	c, err := Load("testdata/full.yaml", noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Roots) != 2 || c.Roots[0] != filepath.Clean(filepath.FromSlash("D:/workspace")) {
		t.Errorf("roots = %v", c.Roots)
	}
	if c.Depth != 3 || c.RefreshSeconds != 120 || c.Auth.Source != "env" {
		t.Errorf("depth %d refresh %d source %q", c.Depth, c.RefreshSeconds, c.Auth.Source)
	}
	if len(c.Repos) != 2 || c.Repos[1] != "ghe.corp/team/tool" {
		t.Errorf("repos = %v", c.Repos)
	}
	d := c.Policy["default"]
	if d.Sidebar.Project != "Roadmap" || d.Sidebar.Milestone != "current" || d.Release.Tag != "v{version}" {
		t.Errorf("default policy = %+v", d)
	}
	if c.Mute.Kinds[0] != "pushed-branch" || c.Mute.Authors[0] != "dependabot[bot]" {
		t.Errorf("mute = %+v", c.Mute)
	}
	if !c.Confirm.Comment || c.Confirm.Merge || !c.Confirm.Release {
		t.Errorf("confirm = %+v", c.Confirm)
	}
}

func TestLoadMissingFileGivesDefaults(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "nope.yaml"), noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if c.Depth != 2 || c.RefreshSeconds != 90 || c.Auth.Source != "keyring" || len(c.Roots) != 0 {
		t.Fatalf("defaults = %+v", c)
	}
	p := c.PolicyFor(repo("a/b"))
	if p.Merge.Method != "squash" || p.Merge.Title != "{title} (#{number})" || *p.Merge.Body != "" {
		t.Fatalf("default merge = %+v", p.Merge)
	}
	if !*p.Merge.DeleteBranch || !*p.Merge.RequireGreen {
		t.Fatalf("default merge bools = %v %v", *p.Merge.DeleteBranch, *p.Merge.RequireGreen)
	}
	if c.Confirm.Comment || c.Confirm.Triage || !c.Confirm.Merge || !c.Confirm.Close {
		t.Fatalf("default confirm = %+v", c.Confirm)
	}
}

func TestLoadRejectsUnknownKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.yaml")
	if err := os.WriteFile(path, []byte("depht: 3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path, noEnv); err == nil || !strings.Contains(err.Error(), "depht") {
		t.Fatalf("got %v", err)
	}
}

func TestEnvOverride(t *testing.T) {
	c, err := Load("testdata/full.yaml", envMap(map[string]string{
		"BRIDGE_DEPTH":                          "4",
		"BRIDGE_AUTH__SOURCE":                   "device_flow",
		"BRIDGE_POLICY__DEFAULT__MERGE__METHOD": "rebase",
		"BRIDGE_ROOTS":                          "C:/a,C:/b",
		"BRIDGE_CONFIRM__MERGE":                 "true",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Depth != 4 || c.Auth.Source != "device_flow" || c.Policy["default"].Merge.Method != "rebase" {
		t.Fatalf("got depth %d source %q method %q", c.Depth, c.Auth.Source, c.Policy["default"].Merge.Method)
	}
	if len(c.Roots) != 2 || !c.Confirm.Merge {
		t.Fatalf("roots %v confirm %+v", c.Roots, c.Confirm)
	}
}

func TestEnvOverrideOnMissingFile(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "nope.yaml"), envMap(map[string]string{"BRIDGE_POLICY__DEFAULT__MERGE__METHOD": "merge"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.PolicyFor(repo("a/b")).Merge.Method != "merge" {
		t.Fatal("env did not reach the default policy")
	}
}

func TestEnvOverrideBadInt(t *testing.T) {
	_, err := Load("testdata/full.yaml", envMap(map[string]string{"BRIDGE_DEPTH": "deep"}))
	if err == nil || !strings.Contains(err.Error(), "BRIDGE_DEPTH") {
		t.Fatalf("got %v", err)
	}
}

func TestPolicyForPrecedence(t *testing.T) {
	c, err := Load("testdata/full.yaml", noEnv)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		repo, method string
		labels       string
		deleteBranch bool
	}{
		{"raven-clown/airlock", "merge", "needs-review", false},
		{"raven-clown/logbook", "rebase", "needs-review", true},
		{"x/y", "squash", "triage", true},
	}
	for _, tt := range tests {
		p := c.PolicyFor(repo(tt.repo))
		if p.Merge.Method != tt.method || strings.Join(p.Sidebar.Labels, ",") != tt.labels || *p.Merge.DeleteBranch != tt.deleteBranch {
			t.Errorf("%s: method %q labels %v delete %v", tt.repo, p.Merge.Method, p.Sidebar.Labels, *p.Merge.DeleteBranch)
		}
	}
}

func TestPolicyForInherits(t *testing.T) {
	c, err := Load("testdata/full.yaml", noEnv)
	if err != nil {
		t.Fatal(err)
	}
	p := c.PolicyFor(repo("raven-clown/logbook"))
	if p.Merge.Title != "{title} (#{number})" || p.Release.Bump != "prompt" || p.Sidebar.Project != "Roadmap" {
		t.Fatalf("did not inherit: %+v", p)
	}
}

func TestMuted(t *testing.T) {
	c, err := Load("testdata/full.yaml", noEnv)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		kind   forge.InboxKind
		repo   string
		author string
		want   bool
	}{
		{forge.IncomingPR, "owner/archive-2020", "x", true},
		{forge.IncomingPR, "owner/live", "dependabot[bot]", true},
		{forge.PushedBranch, "owner/live", "x", true},
		{forge.IncomingPR, "owner/live", "x", false},
	}
	for _, tt := range tests {
		if got := c.Muted(tt.kind, repo(tt.repo), forge.User{Login: tt.author}); got != tt.want {
			t.Errorf("%v %s %s: got %v", tt.kind, tt.repo, tt.author, got)
		}
	}
}

func TestValidate(t *testing.T) {
	base := func() Config { c, _ := Load(filepath.Join(os.TempDir(), "bridge-none.yaml"), noEnv); return c }
	tests := []struct {
		name  string
		edit  func(*Config)
		field string
	}{
		{"depth", func(c *Config) { c.Depth = 0 }, "depth"},
		{"refresh", func(c *Config) { c.RefreshSeconds = 5 }, "refresh_seconds"},
		{"auth", func(c *Config) { c.Auth.Source = "file" }, "auth.source"},
		{"method", func(c *Config) { p := c.Policy["default"]; p.Merge.Method = "octopus"; c.Policy["default"] = p }, "policy.default.merge.method"},
		{"bump", func(c *Config) { p := c.Policy["default"]; p.Release.Bump = "major"; c.Policy["default"] = p }, "policy.default.release.bump"},
		{"repo", func(c *Config) { c.Repos = []string{"nope"} }, "repos"},
		{"mute kind", func(c *Config) { c.Mute.Kinds = []string{"spam"} }, "mute.kinds"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := base()
			tt.edit(&c)
			err := Validate(c)
			if err == nil || !strings.Contains(err.Error(), tt.field) {
				t.Fatalf("got %v, want error naming %s", err, tt.field)
			}
		})
	}
	if err := Validate(base()); err != nil {
		t.Fatalf("defaults invalid: %v", err)
	}
}

func TestPaths(t *testing.T) {
	tests := []struct {
		goos             string
		env              map[string]string
		home             string
		wantCfg, wantDir string
	}{
		{"windows", map[string]string{"APPDATA": `C:\U\AppData\Roaming`, "LOCALAPPDATA": `C:\U\AppData\Local`}, `C:\U`,
			filepath.Join(`C:\U\AppData\Roaming`, "bridge", "config.yaml"), filepath.Join(`C:\U\AppData\Local`, "bridge")},
		{"linux", map[string]string{}, "/home/u",
			filepath.Join("/home/u", ".config", "bridge", "config.yaml"), filepath.Join("/home/u", ".local", "state", "bridge")},
		{"darwin", map[string]string{"XDG_CONFIG_HOME": "/x/cfg", "XDG_STATE_HOME": "/x/st"}, "/Users/u",
			filepath.Join("/x/cfg", "bridge", "config.yaml"), filepath.Join("/x/st", "bridge")},
		{"windows", map[string]string{"BRIDGE_CONFIG": `D:\c.yaml`, "BRIDGE_STATE_DIR": `D:\st`}, `C:\U`, `D:\c.yaml`, `D:\st`},
	}
	for _, tt := range tests {
		cfg, err := configPath(tt.goos, envMap(tt.env), tt.home)
		if err != nil || cfg != tt.wantCfg {
			t.Errorf("%s config: %q %v, want %q", tt.goos, cfg, err, tt.wantCfg)
		}
		dir, err := stateDir(tt.goos, envMap(tt.env), tt.home)
		if err != nil || dir != tt.wantDir {
			t.Errorf("%s state: %q %v, want %q", tt.goos, dir, err, tt.wantDir)
		}
	}
}
