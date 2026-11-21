// Package thread loads a conversation and posts replies, keeping a local
// draft until the forge accepts the reply.
package thread

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/raven-clown/bridge/forge"
	"github.com/raven-clown/bridge/store"
)

var ErrEmptyBody = errors.New("the reply is empty")

func Load(ctx context.Context, f forge.Forge, s *store.Store, id forge.ItemID) (forge.Thread, error) {
	th, err := f.Thread(ctx, id)
	if err != nil {
		return forge.Thread{}, err
	}
	if err := s.MarkSeen(id, time.Now()); err != nil {
		return th, err
	}
	return th, nil
}

// Reply saves the draft before posting, so a failed post never loses text.
func Reply(ctx context.Context, f forge.Forge, s *store.Store, id forge.ItemID, replyTo forge.CommentID, body string) (forge.CommentID, error) {
	if strings.TrimSpace(body) == "" {
		return "", ErrEmptyBody
	}
	if err := s.SaveReplyDraft(id, replyTo, body); err != nil {
		return "", err
	}
	var to *forge.CommentID
	if replyTo != "" {
		to = &replyTo
	}
	cid, err := f.Comment(ctx, id, body, to)
	if err != nil {
		return "", err
	}
	if err := s.DeleteReplyDraft(id); err != nil {
		return cid, err
	}
	return cid, s.MarkAnswered(id, time.Now())
}
