package store

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/raven-clown/bridge/forge"
)

func reviewPath(pr forge.PRID) string {
	return filepath.Join("drafts", "review-"+itemKey(pr)+".json")
}

// SaveReviewDraft takes any value so the store need not import the review
// package, which imports the store.
func (s *Store) SaveReviewDraft(pr forge.PRID, v any) error {
	return s.writeJSON(reviewPath(pr), v)
}

func (s *Store) LoadReviewDraft(pr forge.PRID, v any) (bool, error) {
	if err := s.readJSON(reviewPath(pr), v); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (s *Store) DeleteReviewDraft(pr forge.PRID) error {
	err := os.Remove(filepath.Join(s.dir, reviewPath(pr)))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
