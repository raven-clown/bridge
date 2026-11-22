package review

import (
	"slices"

	"github.com/raven-clown/bridge/forge"
	"github.com/raven-clown/bridge/store"
)

type DraftComment struct {
	Path      string     `json:"path"`
	Line      int        `json:"line"`
	Side      forge.Side `json:"side"`
	StartLine int        `json:"start_line,omitempty"`
	StartSide forge.Side `json:"start_side,omitempty"`
	Body      string     `json:"body"`
}

// Draft.HeadSHA is the commit the comments were written against, so a push
// during the review is noticed before submitting.
type Draft struct {
	PR       forge.PRID     `json:"pr"`
	HeadSHA  forge.SHA      `json:"head_sha"`
	Comments []DraftComment `json:"comments,omitempty"`
	Summary  string         `json:"summary,omitempty"`
	Viewed   []string       `json:"viewed,omitempty"`
	Pending  *Pending       `json:"pending,omitempty"`
}

// Pending is text still in the editor, saved as it is typed so a closed
// editor, ctrl+c or a crash never loses it. Kind is "comment" or "summary";
// Edit is the index of the comment being changed, or -1.
type Pending struct {
	Kind   string       `json:"kind"`
	Target DraftComment `json:"target"`
	Edit   int          `json:"edit"`
	Body   string       `json:"body"`
}

func (d Draft) Stale(current forge.SHA) bool { return d.HeadSHA != current }

func (d *Draft) Add(c DraftComment) { d.Comments = append(d.Comments, c) }

func (d *Draft) Remove(i int) {
	if i >= 0 && i < len(d.Comments) {
		d.Comments = slices.Delete(d.Comments, i, i+1)
	}
}

func (d Draft) IsViewed(path string) bool { return slices.Contains(d.Viewed, path) }

func (d *Draft) ToggleViewed(path string) {
	if i := slices.Index(d.Viewed, path); i >= 0 {
		d.Viewed = slices.Delete(d.Viewed, i, i+1)
		return
	}
	d.Viewed = append(d.Viewed, path)
}

func LoadDraft(s *store.Store, pr forge.PRID) (Draft, bool, error) {
	var d Draft
	ok, err := s.LoadReviewDraft(pr, &d)
	return d, ok, err
}

func SaveDraft(s *store.Store, d Draft) error { return s.SaveReviewDraft(d.PR, d) }
