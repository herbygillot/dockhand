package github

import githubapi "github.com/herbygillot/dockhand/internal/github"

type Client struct {
	*githubapi.Client
	// GitExecutable reads tags with plain git when the API cannot; empty
	// finds git on PATH.
	GitExecutable string
}
