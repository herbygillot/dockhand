package github

import (
	"context"
	"errors"
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
	HTTP        *http.Client
	Config      Config
	Credentials TokenSource

	once       sync.Once
	sdk        *gh.Client
	initErr    error
	authMu     sync.Mutex
	authSDK    *gh.Client
	authSource CredentialSource
}

// API uses available credentials for public reads, allowing anonymous reads only when none exist.
func (c *Client) API(ctx context.Context) (*gh.Client, error) {
	if c == nil {
		return nil, fmt.Errorf("github: client is required")
	}
	c.authMu.Lock()
	authenticated := c.authSDK
	c.authMu.Unlock()
	if authenticated != nil {
		return authenticated, nil
	}
	if c.Config.Token != "" || c.Credentials != nil || c.Config.BaseURL == "" {
		api, err := c.AuthenticatedAPI(ctx)
		if err == nil {
			return api, nil
		}
		if !errors.Is(err, ErrNoCredentials) {
			return nil, err
		}
	}
	c.once.Do(func() { c.sdk, c.initErr = c.newAPI(c.Config.Token, SourceExplicit) })
	return c.sdk, c.initErr
}

// AuthenticatedAPI shares one initialized credential and SDK client between adapters.
func (c *Client) AuthenticatedAPI(ctx context.Context) (*gh.Client, error) {
	if c == nil {
		return nil, fmt.Errorf("github: client is required")
	}
	c.authMu.Lock()
	defer c.authMu.Unlock()
	if c.authSDK != nil {
		return c.authSDK, nil
	}
	token := c.Config.Token
	origin := SourceExplicit
	if token == "" {
		source := c.Credentials
		if source == nil && c.Config.BaseURL == "" {
			source = SystemCredentials{}
		}
		if source == nil {
			return nil, fmt.Errorf("%w: configure an explicit credential for this GitHub API", ErrAuthentication)
		}
		resolved, err := source.Token(ctx)
		if err != nil {
			return nil, err
		}
		token, origin = resolved.Secret, resolved.Source
		if origin == "" {
			origin = SourceExplicit
		}
	}
	token, err := validToken(token)
	if err != nil {
		return nil, err
	}
	api, err := c.newAPI(token, origin)
	if err != nil {
		return nil, err
	}
	c.authSDK, c.authSource = api, origin
	return c.authSDK, nil
}

func (c *Client) Authenticate(ctx context.Context) error {
	_, err := c.AuthenticatedUser(ctx)
	return err
}

func (c *Client) AuthenticatedUser(ctx context.Context) (string, error) {
	client, err := c.AuthenticatedAPI(ctx)
	if err != nil {
		return "", err
	}
	user, _, err := client.Users.Get(ctx, "")
	if err != nil {
		return "", fmt.Errorf("github: checking authenticated user: %w", RateLimitError(err))
	}
	if user == nil || user.GetLogin() == "" {
		return "", fmt.Errorf("%w: GitHub returned no authenticated user", ErrAuthentication)
	}
	return user.GetLogin(), nil
}

func (c *Client) newAPI(token string, source CredentialSource) (*gh.Client, error) {
	client := http.Client{}
	if c.HTTP != nil {
		client = *c.HTTP
	}
	transport := client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	client.Transport = redirectTransport{next: transport, source: source, authenticated: token != ""}
	options := []gh.ClientOptionsFunc{gh.WithHTTPClient(&client)}
	if c.Config.BaseURL != "" {
		options = append(options, gh.WithURLs(&c.Config.BaseURL, nil))
	}
	if token != "" {
		options = append(options, gh.WithAuthToken(token))
	}
	return gh.NewClient(options...)
}

func (c *Client) CredentialSource() CredentialSource {
	c.authMu.Lock()
	defer c.authMu.Unlock()
	return c.authSource
}
