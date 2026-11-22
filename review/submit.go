package review

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/raven-clown/bridge/forge"
	"github.com/raven-clown/bridge/store"
)

// GitHub requires a body for COMMENT and REQUEST_CHANGES, even with inline
// comments; an approval may be empty.
var ErrNeedsSummary = errors.New("a comment or a request for changes needs a summary")

// Submit sends the draft as a review. Against a moved head it returns
// forge.ErrStaleHead unless force is set, since the commented lines may no
// longer exist. The draft is deleted only after GitHub accepts the review.
func Submit(ctx context.Context, f forge.Forge, s *store.Store, d Draft, event forge.ReviewEvent, force bool) error {
	if event != forge.EventApprove && strings.TrimSpace(d.Summary) == "" {
		return ErrNeedsSummary
	}
	diff, err := f.PRDiff(ctx, d.PR)
	if err != nil {
		return err
	}
	// Any review of a moved head asks first, an approval most of all: it
	// would approve commits the reviewer never saw.
	if d.Stale(diff.HeadSHA) && !force {
		return forge.ErrStaleHead
	}
	// The review is tied to the commit that was looked at; only a forced
	// submit takes the new head.
	commit := d.HeadSHA
	if force || commit == "" {
		commit = diff.HeadSHA
	}
	rv := forge.Review{CommitSHA: commit, Event: event, Body: d.Summary}
	for _, c := range d.Comments {
		rv.Comments = append(rv.Comments, forge.ReviewComment{
			Path: c.Path, Line: c.Line, Side: c.Side, StartLine: c.StartLine, StartSide: c.StartSide, Body: c.Body,
		})
	}
	if err := f.SubmitReview(ctx, d.PR, rv); err != nil {
		return staleOr(ctx, f, d.PR, commit, force, err)
	}
	return s.DeleteReviewDraft(d.PR)
}

// staleOr decides what a 422 means by looking at the head again: if someone
// pushed between the check and the submit, the comments may no longer fit;
// otherwise GitHub's own message is the answer.
func staleOr(ctx context.Context, f forge.Forge, pr forge.PRID, sent forge.SHA, force bool, err error) error {
	var api *forge.APIError
	if force || !errors.As(err, &api) || api.Status != http.StatusUnprocessableEntity {
		return err
	}
	if now, derr := f.PRDiff(ctx, pr); derr == nil && now.HeadSHA != sent {
		return errors.Join(forge.ErrStaleHead, err)
	}
	return err
}
