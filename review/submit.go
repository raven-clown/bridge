package review

import (
	"context"
	"errors"
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
	if d.Stale(diff.HeadSHA) && !force {
		return forge.ErrStaleHead
	}
	rv := forge.Review{CommitSHA: diff.HeadSHA, Event: event, Body: d.Summary}
	for _, c := range d.Comments {
		rv.Comments = append(rv.Comments, forge.ReviewComment{
			Path: c.Path, Line: c.Line, Side: c.Side, StartLine: c.StartLine, StartSide: c.StartSide, Body: c.Body,
		})
	}
	if err := f.SubmitReview(ctx, d.PR, rv); err != nil {
		return err
	}
	return s.DeleteReviewDraft(d.PR)
}
