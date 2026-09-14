// Package github adapts GitHub repository and publication observations to forge contracts.
package github

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	gh "github.com/google/go-github/v91/github"
	"github.com/herbygillot/dockhand/v2/internal/forge"
)

type Config struct {
	BaseURL string
	Token   string
}

// Configure a Client before its first API operation; it is then safe to share.
type Client struct {
	HTTP        *http.Client
	Config      Config
	Credentials TokenSource

	once    sync.Once
	sdk     *gh.Client
	initErr error
	authMu  sync.Mutex
	authSDK *gh.Client
}

func (c *Client) api() (*gh.Client, error) {
	if c == nil {
		return nil, fmt.Errorf("github: client is required")
	}
	c.authMu.Lock()
	authenticated := c.authSDK
	c.authMu.Unlock()
	if authenticated != nil {
		return authenticated, nil
	}
	c.once.Do(func() { c.sdk, c.initErr = c.newAPI(c.Config.Token) })
	return c.sdk, c.initErr
}

func (c *Client) authenticatedAPI(ctx context.Context) (*gh.Client, error) {
	if c == nil {
		return nil, fmt.Errorf("github: client is required")
	}
	c.authMu.Lock()
	defer c.authMu.Unlock()
	if c.authSDK != nil {
		return c.authSDK, nil
	}
	token := c.Config.Token
	if token == "" {
		source := c.Credentials
		if source == nil && c.Config.BaseURL == "" {
			source = SystemCredentials{}
		}
		if source == nil {
			return nil, fmt.Errorf("%w: configure an explicit credential for this GitHub API", ErrAuthentication)
		}
		var err error
		token, err = source.Token(ctx)
		if err != nil {
			return nil, err
		}
	}
	token, err := validToken(token)
	if err != nil {
		return nil, err
	}
	api, err := c.newAPI(token)
	if err != nil {
		return nil, err
	}
	c.authSDK = api
	return c.authSDK, nil
}

func (c *Client) Authenticate(ctx context.Context) error {
	_, err := c.AuthenticatedUser(ctx)
	return err
}

func (c *Client) AuthenticatedUser(ctx context.Context) (string, error) {
	client, err := c.authenticatedAPI(ctx)
	if err != nil {
		return "", err
	}
	user, response, err := client.Users.Get(ctx, "")
	if err != nil {
		if response != nil && response.StatusCode == http.StatusUnauthorized {
			return "", fmt.Errorf("%w: GitHub rejected the configured credential: %w", forge.ErrAuthentication, err)
		}
		return "", fmt.Errorf("github: checking authenticated user: %w", err)
	}
	if user == nil || user.GetLogin() == "" {
		return "", fmt.Errorf("%w: GitHub returned no authenticated user", ErrAuthentication)
	}
	return user.GetLogin(), nil
}

func (c *Client) newAPI(token string) (*gh.Client, error) {
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
	if token != "" {
		options = append(options, gh.WithAuthToken(token))
	}
	return gh.NewClient(options...)
}
