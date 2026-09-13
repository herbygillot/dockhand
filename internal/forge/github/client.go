// Package github implements GitHub repository access and defines the PR adapter.
// It owns protocol details and returns forge observations without applying version policy.
package github

import "net/http"

type Config struct {
	BaseURL string
	Token   string
}

type Client struct {
	HTTP   *http.Client
	Config Config
}
