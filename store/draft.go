package store

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/raven-clown/bridge/forge"
)

type replyDraft struct {
	ReplyTo forge.CommentID `json:"reply_to,omitempty"`
	Body    string          `json:"body"`
}

func replyPath(id forge.ItemID) string {
	return filepath.Join("drafts", "reply-"+itemKey(id)+".json")
}

func (s *Store) SaveReplyDraft(id forge.ItemID, replyTo forge.CommentID, body string) error {
	return s.writeJSON(replyPath(id), replyDraft{ReplyTo: replyTo, Body: body})
}

func (s *Store) LoadReplyDraft(id forge.ItemID) (replyTo forge.CommentID, body string, ok bool, err error) {
	var d replyDraft
	if err := s.readJSON(replyPath(id), &d); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", "", false, nil
		}
		return "", "", false, err
	}
	return d.ReplyTo, d.Body, true, nil
}

func (s *Store) DeleteReplyDraft(id forge.ItemID) error {
	err := os.Remove(filepath.Join(s.dir, replyPath(id)))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
