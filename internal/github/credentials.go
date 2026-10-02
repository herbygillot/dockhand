package github

import (
	"context"
	"io"
	"net/http"
	"sync"
	"time"
)

// Renewer is a TokenSource that, told the token GitHub rejected, can find
// another: a login it can refresh. A source that isn't one is asked again
// as it was, which finds a login saved since.
type Renewer interface {
	Renew(ctx context.Context, rejected Token) (Token, error)
}

// renewBefore is how long before a token expires it's asked for anew.
const renewBefore = 5 * time.Minute

// credentials is the token a client's requests carry, asked of its source
// as they need it, rather than once when the client was made: serve made
// its client once, and a new auth login, or a login renewed, reached it
// only when it was started again (the GitHub auth flow review's plan,
// step 1, 2026-10-02).
type credentials struct {
	source TokenSource
	now    func() time.Time

	mu    sync.Mutex
	token Token
	have  bool
	// last is the source of the token last carried, for CredentialSource.
	last CredentialSource
}

func (c *credentials) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

// current is the token to carry: the one held, until it's within
// renewBefore of expiring, else the source's.
func (c *credentials) current(ctx context.Context) (Token, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.have && (c.token.Expiry.IsZero() || c.clock().Add(renewBefore).Before(c.token.Expiry)) {
		return c.token, nil
	}
	return c.ask(ctx, func(ctx context.Context) (Token, error) { return c.source.Token(ctx) })
}

// renew is the token to carry after GitHub rejected one: the held one,
// where another request already replaced the rejected token, else the
// source's anew.
func (c *credentials) renew(ctx context.Context, rejected Token) (Token, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.have && c.token.Secret != rejected.Secret {
		return c.token, nil
	}
	c.have = false
	if renewer, ok := c.source.(Renewer); ok {
		return c.ask(ctx, func(ctx context.Context) (Token, error) { return renewer.Renew(ctx, rejected) })
	}
	return c.ask(ctx, func(ctx context.Context) (Token, error) { return c.source.Token(ctx) })
}

// ask asks the source, holding what it gives; c.mu is held.
func (c *credentials) ask(ctx context.Context, from func(context.Context) (Token, error)) (Token, error) {
	token, err := from(ctx)
	if err != nil {
		return Token{}, err
	}
	if token.Secret, err = validToken(token.Secret); err != nil {
		return Token{}, err
	}
	if token.Source == "" {
		token.Source = SourceExplicit
	}
	c.token, c.have, c.last = token, true, token.Source
	return token, nil
}

func (c *credentials) lastSource() CredentialSource {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.last
}

// renewable reports a source another token may come from after a
// rejection: dockhand's login, or gh's. A token in the environment, or one
// configured, is the person's to replace.
func renewable(source CredentialSource) bool {
	return source == SourceKeychain || source == SourceGitHubCLI
}

// staticToken is a configured token, which nothing renews.
type staticToken string

func (s staticToken) Token(context.Context) (Token, error) {
	return Token{Secret: string(s), Source: SourceExplicit}, nil
}

// credentialTransport carries the credential's token on each request, and
// where GitHub rejects a token its source may replace, asks for another
// and tries the request once more with it. A 401 means GitHub did
// nothing, so a write is tried again as safely as a read, where its body
// can be read again.
type credentialTransport struct {
	next        http.RoundTripper
	credentials *credentials
}

func (t credentialTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	token, err := t.credentials.current(req.Context())
	if err != nil {
		return nil, err
	}
	response, err := t.next.RoundTrip(carrying(req, token, req.Body))
	if err != nil || response.StatusCode != http.StatusUnauthorized || !renewable(token.Source) || req.Body != nil && req.GetBody == nil {
		return response, err
	}
	next, renewErr := t.credentials.renew(req.Context(), token)
	if renewErr != nil || next.Secret == token.Secret {
		return response, err
	}
	body := req.Body
	if req.GetBody != nil {
		if body, err = req.GetBody(); err != nil {
			return response, nil
		}
	}
	response.Body.Close()
	return t.next.RoundTrip(carrying(req, next, body))
}

// carrying is req, a copy, with the token as its authorization and body
// as its body.
func carrying(req *http.Request, token Token, body io.ReadCloser) *http.Request {
	carried := req.Clone(req.Context())
	carried.Body = body
	carried.Header.Set("Authorization", "Bearer "+token.Secret)
	return carried
}
