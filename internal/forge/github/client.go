package github

import (
	"context"
	"errors"
	"net/http"

	"github.com/herbygillot/dockhand/v2/internal/model"
	"github.com/herbygillot/dockhand/v2/internal/publish"
	"github.com/herbygillot/dockhand/v2/internal/upstream"
)

var ErrNotImplemented = errors.New("github: forge adapter is not implemented")

type Config struct {
	BaseURL string
	Token   string
}

type Client struct {
	HTTP   *http.Client
	Config Config
}

func (c *Client) Releases(ctx context.Context, repository string) ([]upstream.Release, error) {
	return nil, ErrNotImplemented
}
func (c *Client) Find(ctx context.Context, repository, branch string) (publish.Observation, error) {
	return publish.Observation{}, ErrNotImplemented
}
func (c *Client) Observe(ctx context.Context, ref model.PullRequestRef) (publish.Observation, error) {
	return publish.Observation{}, ErrNotImplemented
}
func (c *Client) Create(ctx context.Context, request publish.Request) (publish.Observation, error) {
	return publish.Observation{}, ErrNotImplemented
}
func (c *Client) Update(ctx context.Context, request publish.Request) (publish.Observation, error) {
	return publish.Observation{}, ErrNotImplemented
}

var _ publish.Forge = (*Client)(nil)
var _ upstream.ReleaseReader = (*Client)(nil)
