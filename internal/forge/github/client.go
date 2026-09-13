// Package github adapts GitHub repository and publication observations to forge contracts.
package github

import (
	"fmt"
	"net/http"
	"sync"

	gh "github.com/google/go-github/v91/github"
)

type Config struct {
	BaseURL string
	Token   string
}

// Configure a Client before its first API operation; it is then safe to share.
type Client struct {
	HTTP   *http.Client
	Config Config

	once    sync.Once
	sdk     *gh.Client
	initErr error
}

func (c *Client) api() (*gh.Client, error) {
	if c == nil {
		return nil, fmt.Errorf("github: client is required")
	}
	c.once.Do(func() { c.sdk, c.initErr = c.newAPI() })
	return c.sdk, c.initErr
}

func (c *Client) newAPI() (*gh.Client, error) {
	client := http.Client{}
	if c.HTTP != nil {
		client = *c.HTTP
	}
	transport := client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	client.Transport = redirectTransport{next: transport}
	options := []gh.ClientOptionsFunc{gh.WithHTTPClient(&client)}
	if c.Config.BaseURL != "" {
		options = append(options, gh.WithURLs(&c.Config.BaseURL, nil))
	}
	if c.Config.Token != "" {
		options = append(options, gh.WithAuthToken(c.Config.Token))
	}
	return gh.NewClient(options...)
}
