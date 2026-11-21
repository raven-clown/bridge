package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/raven-clown/bridge/forge"
	"github.com/raven-clown/bridge/inbox"
)

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func age(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", max(int(d.Minutes()), 0))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

func urgencyGlyph(u inbox.Urgency) string {
	switch {
	case u >= inbox.UrgencyUnanswered:
		return "!!"
	case u >= inbox.UrgencyUntriagedIssue:
		return "!"
	}
	return "·"
}

func checksGlyph(c *forge.Checks) string {
	if c == nil {
		return ""
	}
	switch c.State {
	case forge.ChecksPassing:
		return "✓"
	case forge.ChecksFailing:
		return "✗"
	case forge.ChecksPending:
		return "◌"
	case forge.ChecksAwaitingApproval:
		return "⏸"
	}
	return ""
}

func itemRef(it inbox.Item) string {
	if it.Kind == forge.PushedBranch {
		return it.Branch
	}
	return "#" + strconv.Itoa(it.ID.Number)
}

// parseRef accepts an issue or pull request URL, owner/name#n or
// host/owner/name#n.
func parseRef(s string) (forge.ItemID, error) {
	bad := fmt.Errorf("cannot read %q: want a URL, owner/name#number or host/owner/name#number", s)
	if strings.Contains(s, "://") {
		u, err := url.Parse(s)
		if err != nil {
			return forge.ItemID{}, bad
		}
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) != 4 || (parts[2] != "pull" && parts[2] != "issues") {
			return forge.ItemID{}, bad
		}
		n, err := strconv.Atoi(parts[3])
		if err != nil || n <= 0 {
			return forge.ItemID{}, bad
		}
		return forge.ItemID{Repo: forge.RepoID{Host: u.Hostname(), Owner: parts[0], Name: parts[1]}, Number: n}, nil
	}
	repo, num, ok := strings.Cut(s, "#")
	if !ok {
		return forge.ItemID{}, bad
	}
	r, err := forge.ParseRepoID(repo)
	if err != nil {
		return forge.ItemID{}, bad
	}
	n, err := strconv.Atoi(num)
	if err != nil || n <= 0 {
		return forge.ItemID{}, bad
	}
	return forge.ItemID{Repo: r, Number: n}, nil
}

func issueURL(id forge.ItemID) string {
	return fmt.Sprintf("https://%s/%s/%s/issues/%d", id.Repo.Host, id.Repo.Owner, id.Repo.Name, id.Number)
}
