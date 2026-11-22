package forge

import "errors"

var ErrStaleHead = errors.New("the pull request changed since this review was started")

type FileStatus string

const (
	FileAdded    FileStatus = "added"
	FileModified FileStatus = "modified"
	FileRemoved  FileStatus = "removed"
	FileRenamed  FileStatus = "renamed"
)

// FilePatch.PatchMissing is true when GitHub sent no patch at all, which the
// counts cannot tell: a binary file is +0 -0 with no patch.
type FilePatch struct {
	Path         string     `json:"path"`
	PreviousPath string     `json:"previous_path,omitempty"`
	Status       FileStatus `json:"status"`
	Additions    int        `json:"additions"`
	Deletions    int        `json:"deletions"`
	Patch        string     `json:"patch,omitempty"`
	PatchMissing bool       `json:"patch_missing,omitempty"`
}

// Diff.Truncated means GitHub stopped listing files (it lists at most 3000).
type Diff struct {
	PR        PRID        `json:"pr"`
	HeadSHA   SHA         `json:"head_sha"`
	BaseRef   string      `json:"base_ref"`
	HeadRef   string      `json:"head_ref"`
	Files     []FilePatch `json:"files"`
	Truncated bool        `json:"truncated,omitempty"`
}

type ReviewEvent string

const (
	EventComment        ReviewEvent = "COMMENT"
	EventApprove        ReviewEvent = "APPROVE"
	EventRequestChanges ReviewEvent = "REQUEST_CHANGES"
)

type Side string

const (
	SideLeft  Side = "LEFT"
	SideRight Side = "RIGHT"
)

// ReviewComment.StartLine is 0 for a comment on a single line.
type ReviewComment struct {
	Path      string `json:"path"`
	Line      int    `json:"line"`
	Side      Side   `json:"side"`
	StartLine int    `json:"start_line,omitempty"`
	StartSide Side   `json:"start_side,omitempty"`
	Body      string `json:"body"`
}

type Review struct {
	CommitSHA SHA             `json:"commit_sha"`
	Event     ReviewEvent     `json:"event"`
	Body      string          `json:"body,omitempty"`
	Comments  []ReviewComment `json:"comments,omitempty"`
}
