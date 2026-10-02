package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	gh "github.com/google/go-github/v91/github"

	"github.com/herbygillot/dockhand/internal/fetch"
)

type Config struct {
	BaseURL string
	Token   string
	// CloneURL is where repositories are read with plain git, when the API
	// cannot answer; empty is https://github.com.
	CloneURL string
}

// Configure a Client before its first API operation; it is then safe to share.
type Client struct {
	HTTP        *http.Client
	Config      Config
	Credentials TokenSource

	once        sync.Once
	sdk         *gh.Client
	initErr     error
	authMu      sync.Mutex
	authSDK     *gh.Client
	credentials *credentials
	// anonymousUntil is when finding no credentials stops being
	// remembered, so an anonymous read doesn't ask the keychain and gh
	// again each time (the code-organization review's finding 45); now
	// is the clock, time.Now where nil.
	anonymousUntil time.Time
	now            func() time.Time
}

// anonymousFor is how long API remembers finding no credentials: long
// enough that outdated's reads don't each run security and gh, short
// enough that a login made meanwhile is soon used. AuthenticatedAPI,
// which needs one, always looks.
const anonymousFor = 5 * time.Minute

func (c *Client) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

// API uses available credentials for public reads, allowing anonymous reads only when none exist.
func (c *Client) API(ctx context.Context) (*gh.Client, error) {
	if c == nil {
		return nil, fmt.Errorf("github: client is required")
	}
	c.authMu.Lock()
	authenticated, anonymous := c.authSDK, c.clock().Before(c.anonymousUntil)
	c.authMu.Unlock()
	if authenticated != nil {
		return authenticated, nil
	}
	if !anonymous && (c.Config.Token != "" || c.Credentials != nil || c.Config.BaseURL == "") {
		api, err := c.AuthenticatedAPI(ctx)
		if err == nil {
			return api, nil
		}
		if !errors.Is(err, ErrNoCredentials) {
			return nil, err
		}
		c.authMu.Lock()
		c.anonymousUntil = c.clock().Add(anonymousFor)
		c.authMu.Unlock()
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
	var source TokenSource = staticToken(c.Config.Token)
	if c.Config.Token == "" {
		source = c.Credentials
		if source == nil && c.Config.BaseURL == "" {
			source = SystemCredentials{}
		}
		if source == nil {
			return nil, fmt.Errorf("%w: configure an explicit credential for this GitHub API", ErrAuthentication)
		}
	}
	// The token is asked for now, so a client with none says so here, and
	// then again for each request, as its source has it then.
	held := &credentials{source: source, now: c.now}
	if _, err := held.current(ctx); err != nil {
		return nil, err
	}
	api, err := c.newAuthenticatedAPI(held)
	if err != nil {
		return nil, err
	}
	c.authSDK, c.credentials = api, held
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

// newAPI is an SDK client carrying token, a configured one or none.
func (c *Client) newAPI(token string, source CredentialSource) (*gh.Client, error) {
	return c.sdkClient(token != "", func() CredentialSource { return source }, func(transport http.RoundTripper) http.RoundTripper { return transport }, token)
}

// newAuthenticatedAPI is an SDK client whose requests each carry the
// token held asks its source for.
func (c *Client) newAuthenticatedAPI(held *credentials) (*gh.Client, error) {
	return c.sdkClient(true, held.lastSource, func(transport http.RoundTripper) http.RoundTripper {
		return credentialTransport{next: transport, credentials: held}
	}, "")
}

// sdkClient is go-github over dockhand's transports: GitHub's rate limits
// read, redirects held to the API's origin, and the credential, where
// carry adds one; token is a fixed one go-github carries itself.
func (c *Client) sdkClient(authenticated bool, source func() CredentialSource, carry func(http.RoundTripper) http.RoundTripper, token string) (*gh.Client, error) {
	client := http.Client{}
	if c.HTTP != nil {
		client = *c.HTTP
	}
	transport := client.Transport
	if transport == nil {
		transport = fetch.Transport
	}
	// The transport reads GitHub's rate limits (rateLimited), so go-github's
	// own check, which refuses before the transport could wait, is off.
	client.Transport = rateLimited{
		next:   redirectTransport{next: carry(transport), source: source, authenticated: authenticated},
		limits: newRateLimits(authenticated),
	}
	options := []gh.ClientOptionsFunc{gh.WithHTTPClient(&client), gh.WithDisableRateLimitCheck()}
	if c.Config.BaseURL != "" {
		options = append(options, gh.WithURLs(&c.Config.BaseURL, nil))
	}
	if token != "" {
		options = append(options, gh.WithAuthToken(token))
	}
	return gh.NewClient(options...)
}

// CredentialSource is where the token the client last carried came from;
// empty before it has asked for one.
func (c *Client) CredentialSource() CredentialSource {
	c.authMu.Lock()
	held := c.credentials
	c.authMu.Unlock()
	if held == nil {
		return ""
	}
	return held.lastSource()
}
