package github

import (
	"context"
	"errors"

	"github.com/raven-clown/bridge/forge"
)

func (c *Client) PRDiff(context.Context, forge.PRID) (forge.Diff, error) {
	return forge.Diff{}, errors.New("not implemented")
}

func (c *Client) SubmitReview(context.Context, forge.PRID, forge.Review) error {
	return errors.New("not implemented")
}
