package config

import (
	"path"
	"sort"
	"strings"

	"github.com/raven-clown/bridge/forge"
)

type Policy struct {
	Merge   MergePolicy     `yaml:"merge"`
	Release ReleasePolicy   `yaml:"release"`
	Sidebar SidebarDefaults `yaml:"sidebar"`
}

// Pointer fields tell "not set, inherit" apart from an explicit false or "".
type MergePolicy struct {
	Method       string  `yaml:"method"`
	Title        string  `yaml:"title"`
	Body         *string `yaml:"body"`
	DeleteBranch *bool   `yaml:"delete_branch"`
	RequireGreen *bool   `yaml:"require_green"`
}

type ReleasePolicy struct {
	Tag  string `yaml:"tag"`
	Bump string `yaml:"bump"`
}

type SidebarDefaults struct {
	Labels    []string `yaml:"labels"`
	Milestone string   `yaml:"milestone"`
	Assignee  string   `yaml:"assignee"`
	Project   string   `yaml:"project"`
}

func ptr[T any](v T) *T { return &v }

func builtinPolicy() Policy {
	return Policy{
		Merge: MergePolicy{
			Method:       "squash",
			Title:        "{title} (#{number})",
			Body:         ptr(""),
			DeleteBranch: ptr(true),
			RequireGreen: ptr(true),
		},
		Release: ReleasePolicy{Tag: "v{version}", Bump: "prompt"},
	}
}

func mergePolicy(dst *Policy, src Policy) {
	set := func(d *string, s string) {
		if s != "" {
			*d = s
		}
	}
	set(&dst.Merge.Method, src.Merge.Method)
	set(&dst.Merge.Title, src.Merge.Title)
	if src.Merge.Body != nil {
		dst.Merge.Body = src.Merge.Body
	}
	if src.Merge.DeleteBranch != nil {
		dst.Merge.DeleteBranch = src.Merge.DeleteBranch
	}
	if src.Merge.RequireGreen != nil {
		dst.Merge.RequireGreen = src.Merge.RequireGreen
	}
	set(&dst.Release.Tag, src.Release.Tag)
	set(&dst.Release.Bump, src.Release.Bump)
	if src.Sidebar.Labels != nil {
		dst.Sidebar.Labels = src.Sidebar.Labels
	}
	set(&dst.Sidebar.Milestone, src.Sidebar.Milestone)
	set(&dst.Sidebar.Assignee, src.Sidebar.Assignee)
	set(&dst.Sidebar.Project, src.Sidebar.Project)
}

func matchRepo(pattern string, repo forge.RepoID) bool {
	ok, err := path.Match(strings.ToLower(pattern), strings.ToLower(repo.String()))
	return err == nil && ok
}

// PolicyFor layers every matching key over default, least specific first,
// where specificity is the number of literal characters in the key.
func (c Config) PolicyFor(repo forge.RepoID) Policy {
	var keys []string
	for k := range c.Policy {
		if k != "default" && matchRepo(k, repo) {
			keys = append(keys, k)
		}
	}
	literal := func(k string) int { return len(k) - strings.Count(k, "*") - strings.Count(k, "?") }
	sort.Slice(keys, func(i, j int) bool { return literal(keys[i]) < literal(keys[j]) })

	p := builtinPolicy()
	mergePolicy(&p, c.Policy["default"])
	for _, k := range keys {
		mergePolicy(&p, c.Policy[k])
	}
	return p
}

func (c Config) Muted(kind forge.InboxKind, repo forge.RepoID, author forge.User) bool {
	for _, k := range c.Mute.Kinds {
		if k == kind.String() {
			return true
		}
	}
	for _, a := range c.Mute.Authors {
		if strings.EqualFold(a, author.Login) {
			return true
		}
	}
	for _, r := range c.Mute.Repos {
		if matchRepo(r, repo) {
			return true
		}
	}
	return false
}
