// Package forge holds the domain types shared by every package and the
// Forge interface that a hosting service implements.
package forge

import (
	"fmt"
	"strings"
	"time"
)

type RepoID struct {
	Host  string `json:"host"`
	Owner string `json:"owner"`
	Name  string `json:"name"`
}

const DefaultHost = "github.com"

// ParseRepoID accepts "owner/name" (github.com) and "host/owner/name".
func ParseRepoID(s string) (RepoID, error) {
	parts := strings.Split(s, "/")
	for _, p := range parts {
		if p == "" {
			return RepoID{}, fmt.Errorf("invalid repository %q: want owner/name or host/owner/name", s)
		}
	}
	switch len(parts) {
	case 2:
		return RepoID{Host: DefaultHost, Owner: parts[0], Name: parts[1]}, nil
	case 3:
		return RepoID{Host: parts[0], Owner: parts[1], Name: parts[2]}, nil
	}
	return RepoID{}, fmt.Errorf("invalid repository %q: want owner/name or host/owner/name", s)
}

func (r RepoID) String() string { return r.Owner + "/" + r.Name }

type ItemID struct {
	Repo   RepoID `json:"repo"`
	Number int    `json:"number"`
}

func (i ItemID) String() string { return fmt.Sprintf("%s#%d", i.Repo, i.Number) }

type (
	PRID          = ItemID
	CommentID     string
	SHA           string
	MilestoneID   int
	ProjectID     string
	ProjectItemID string
	FieldID       string
)

type InboxKind int

const (
	OwnPR InboxKind = iota
	IncomingPR
	IncomingIssue
	ReviewRequest
	Mention
	PushedBranch
)

var kindNames = [...]string{"own-pr", "incoming-pr", "incoming-issue", "review-request", "mention", "pushed-branch"}

func (k InboxKind) String() string {
	if int(k) < 0 || int(k) >= len(kindNames) {
		return fmt.Sprintf("kind(%d)", int(k))
	}
	return kindNames[k]
}

func ParseInboxKind(s string) (InboxKind, bool) {
	for i, n := range kindNames {
		if n == s {
			return InboxKind(i), true
		}
	}
	return 0, false
}

type User struct {
	Login string `json:"login"`
	Bot   bool   `json:"bot,omitempty"`
}

type Label struct {
	Name  string `json:"name"`
	Color string `json:"color,omitempty"`
}

type Milestone struct {
	ID     string     `json:"id"`
	Number int        `json:"number"`
	Title  string     `json:"title"`
	Due    *time.Time `json:"due,omitempty"`
	Open   bool       `json:"open"`
}

type Project struct {
	ID     ProjectID      `json:"id"`
	Number int            `json:"number"`
	Title  string         `json:"title"`
	Fields []ProjectField `json:"fields,omitempty"`
}

type ProjectField struct {
	ID      FieldID       `json:"id"`
	Name    string        `json:"name"`
	Options []FieldOption `json:"options,omitempty"`
}

type FieldOption struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ProjectRef struct {
	ProjectID ProjectID     `json:"project_id"`
	ItemID    ProjectItemID `json:"item_id"`
	Column    string        `json:"column,omitempty"`
}

type ProjectItem struct {
	ID     ProjectItemID     `json:"id"`
	Item   ItemID            `json:"item"`
	Title  string            `json:"title"`
	Fields map[string]string `json:"fields,omitempty"`
}

type FieldValue struct {
	OptionID string     `json:"option_id,omitempty"`
	Text     string     `json:"text,omitempty"`
	Number   *float64   `json:"number,omitempty"`
	Date     *time.Time `json:"date,omitempty"`
}

type CheckState int

const (
	ChecksNone CheckState = iota
	ChecksPending
	ChecksAwaitingApproval
	ChecksFailing
	ChecksPassing
)

type Checks struct {
	State  CheckState `json:"state"`
	Failed int        `json:"failed,omitempty"`
	Total  int        `json:"total,omitempty"`
}

type ReviewState int

const (
	ReviewNone ReviewState = iota
	ReviewCommented
	ReviewApproved
	ReviewChangesRequested
)

type Mergeable int

const (
	MergeableUnknown Mergeable = iota
	MergeableClean
	MergeableConflicting
	MergeableBlocked
)

type Item struct {
	ID            ItemID       `json:"id"`
	IsPR          bool         `json:"is_pr"`
	Title         string       `json:"title"`
	Author        User         `json:"author"`
	Labels        []Label      `json:"labels,omitempty"`
	Milestone     *Milestone   `json:"milestone,omitempty"`
	Assignees     []User       `json:"assignees,omitempty"`
	Projects      []ProjectRef `json:"projects,omitempty"`
	Checks        *Checks      `json:"checks,omitempty"`
	Review        *ReviewState `json:"review,omitempty"`
	Mergeable     *Mergeable   `json:"mergeable,omitempty"`
	Head          string       `json:"head,omitempty"`
	UpdatedAt     time.Time    `json:"updated_at"`
	LastCommentBy string       `json:"last_comment_by,omitempty"`
	LastCommentAt time.Time    `json:"last_comment_at,omitzero"`
	URL           string       `json:"url,omitempty"`
}

type PullRequest = Item

type CommentRef struct {
	Item    ItemID    `json:"item"`
	Title   string    `json:"title"`
	IsPR    bool      `json:"is_pr"`
	Comment CommentID `json:"comment"`
	Author  User      `json:"author"`
	At      time.Time `json:"at"`
	Excerpt string    `json:"excerpt,omitempty"`
	URL     string    `json:"url,omitempty"`
}

type Branch struct {
	Name     string    `json:"name"`
	Head     SHA       `json:"head"`
	PushedAt time.Time `json:"pushed_at"`
	HasPR    bool      `json:"has_pr"`
}

// Thread holds the newest comments; HiddenComments counts older ones that
// were not fetched.
type Thread struct {
	Item           Item           `json:"item"`
	Body           string         `json:"body"`
	Comments       []Comment      `json:"comments,omitempty"`
	HiddenComments int            `json:"hidden_comments,omitempty"`
	ReviewThreads  []ReviewThread `json:"review_threads,omitempty"`
	Events         []Event        `json:"events,omitempty"`
}

// Comment.Review is set for a pull request review summary: approved,
// changes_requested, commented or dismissed.
type Comment struct {
	ID      CommentID `json:"id"`
	Author  User      `json:"author"`
	At      time.Time `json:"at"`
	Body    string    `json:"body"`
	ReplyTo CommentID `json:"reply_to,omitempty"`
	Review  string    `json:"review,omitempty"`
}

// ReviewThread.ID is what Comment takes as replyTo to answer inside it.
type ReviewThread struct {
	ID       CommentID `json:"id"`
	Path     string    `json:"path"`
	Line     int       `json:"line"`
	Resolved bool      `json:"resolved"`
	Comments []Comment `json:"comments"`
}

type Event struct {
	At     time.Time `json:"at"`
	Actor  User      `json:"actor"`
	Kind   string    `json:"kind"`
	Detail string    `json:"detail,omitempty"`
}

type RepoMeta struct {
	Labels      []Label     `json:"labels,omitempty"`
	Milestones  []Milestone `json:"milestones,omitempty"`
	Assignable  []User      `json:"assignable,omitempty"`
	Projects    []Project   `json:"projects,omitempty"`
	ProjectsErr error       `json:"-"`
}

type ItemState int

const (
	StateOpen ItemState = iota
	StateClosed
)

type NewIssue struct {
	Title     string       `json:"title"`
	Body      string       `json:"body,omitempty"`
	Labels    []string     `json:"labels,omitempty"`
	Assignees []string     `json:"assignees,omitempty"`
	Milestone *MilestoneID `json:"milestone,omitempty"`
}

type ListResult[T any] struct {
	Items     []T  `json:"items"`
	Truncated bool `json:"truncated,omitempty"`
}
