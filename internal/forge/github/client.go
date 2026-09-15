// Package github adapts GitHub repositories and publications to forge contracts.
package github

import githubapi "github.com/herbygillot/dockhand/internal/github"

type Client struct{ *githubapi.Client }
