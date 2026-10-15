package github

import (
	"context"

	"github.com/herbygillot/dockhand/internal/forge"
	githubapi "github.com/herbygillot/dockhand/internal/github"
)

type Client struct {
	*githubapi.Client
	// GitExecutable reads tags with plain git when the API cannot; empty
	// finds git on PATH.
	GitExecutable string
}

// Login is who the client acts as, asked of GitHub, and where its token
// comes from (githubapi.Client.LoginStatus).
func (c *Client) Login(ctx context.Context) (forge.Login, error) {
	status, err := c.LoginStatus(ctx)
	if err != nil {
		return forge.Login{}, err
	}
	return forge.Login{Account: status.Account, Source: string(status.Source), From: status.Source.Words(), RenewsUntil: status.RenewsUntil}, nil
}
