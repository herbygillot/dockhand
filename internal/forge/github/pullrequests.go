package github

import (
	"context"
	"errors"

	"github.com/herbygillot/dockhand/v2/internal/forge"
	"github.com/herbygillot/dockhand/v2/internal/record"
)

var ErrNotImplemented = errors.New("github: pull-request operations are not implemented")

func (c *Client) Find(ctx context.Context, repository, branch string) (forge.PullRequestObservation, error) {
	return forge.PullRequestObservation{}, ErrNotImplemented
}
func (c *Client) Observe(ctx context.Context, ref record.PullRequestRef) (forge.PullRequestObservation, error) {
	return forge.PullRequestObservation{}, ErrNotImplemented
}
func (c *Client) Create(ctx context.Context, request forge.PullRequestInput) (forge.PullRequestObservation, error) {
	return forge.PullRequestObservation{}, ErrNotImplemented
}
func (c *Client) Update(ctx context.Context, request forge.PullRequestInput) (forge.PullRequestObservation, error) {
	return forge.PullRequestObservation{}, ErrNotImplemented
}
