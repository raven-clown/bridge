package forge

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var (
	ErrNotFound      = errors.New("not found")
	ErrForbidden     = errors.New("forbidden")
	ErrProjectsScope = errors.New("projects need the read:project scope")
)

type RateLimitedError struct {
	RetryAt time.Time
}

func (e *RateLimitedError) Error() string {
	return fmt.Sprintf("rate limited until %s", e.RetryAt.Format(time.Kitchen))
}

// Forge is a code hosting service. Review and ship methods join when their
// features do.
type Forge interface {
	WhoAmI(ctx context.Context) (User, error)

	ListMyPRs(ctx context.Context, since *time.Time) (ListResult[Item], error)
	ListIncoming(ctx context.Context, repos []RepoID, since *time.Time) (ListResult[Item], error)
	ListReviewRequests(ctx context.Context) (ListResult[Item], error)
	ListMentions(ctx context.Context, since *time.Time) (ListResult[CommentRef], error)
	ListBranches(ctx context.Context, repo RepoID) ([]Branch, error)
	PRChecks(ctx context.Context, pr PRID) (Checks, error)

	Thread(ctx context.Context, item ItemID) (Thread, error)
	Comment(ctx context.Context, item ItemID, body string, replyTo *CommentID) (CommentID, error)

	RepoMeta(ctx context.Context, repo RepoID) (RepoMeta, error)
	SetLabels(ctx context.Context, item ItemID, add, remove []string) error
	SetMilestone(ctx context.Context, item ItemID, milestone *MilestoneID) error
	SetAssignees(ctx context.Context, item ItemID, add, remove []string) error
	SetState(ctx context.Context, item ItemID, state ItemState) error
	CreateIssue(ctx context.Context, repo RepoID, issue NewIssue) (ItemID, error)
	ListProjects(ctx context.Context, owner string) ([]Project, error)
	ProjectItems(ctx context.Context, project ProjectID) ([]ProjectItem, error)
	AddToProject(ctx context.Context, project ProjectID, item ItemID) (ProjectItemID, error)
	SetProjectField(ctx context.Context, project ProjectID, item ProjectItemID, field FieldID, value FieldValue) error
}
