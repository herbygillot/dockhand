package github

import (
	"context"
	"fmt"
	"net/http"
	"sync"
)

// Identity is who the person's GitHub login acts as now, and where its
// token comes from.
type Identity struct {
	Account string
	Source  CredentialSource
}

// IdentityWatcher says who the login acts as each time it's asked,
// reading the credential chain afresh, as a client holding a token
// doesn't: a long-running serve kept acting with a token after a logout,
// and could have gone on with the GitHub CLI's, another account's, once it
// expired (the rc6 full stage, D-C2). The account is asked of GitHub only
// when the token changes. With no credential at all it returns
// ErrNoCredentials.
type IdentityWatcher struct {
	Credentials TokenSource
	HTTP        *http.Client
	BaseURL     string

	mu      sync.Mutex
	secret  string
	current Identity
}

// SystemIdentity watches the login SystemClient uses.
func SystemIdentity(credentials SystemCredentials) *IdentityWatcher {
	return &IdentityWatcher{Credentials: credentials}
}

func (w *IdentityWatcher) Current(ctx context.Context) (Identity, error) {
	if w == nil || w.Credentials == nil {
		return Identity{}, fmt.Errorf("github: a credential source is required")
	}
	token, err := w.Credentials.Token(ctx)
	if err != nil {
		return Identity{}, err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if token.Secret == w.secret && w.current.Account != "" {
		return w.current, nil
	}
	client := &Client{HTTP: w.HTTP, Config: Config{BaseURL: w.BaseURL, Token: token.Secret}}
	account, err := client.AuthenticatedUser(ctx)
	if err != nil {
		return Identity{}, err
	}
	w.secret, w.current = token.Secret, Identity{Account: account, Source: token.Source}
	return w.current, nil
}
