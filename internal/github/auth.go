package github

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/credential"
	"github.com/herbygillot/dockhand/internal/fetch"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/subprocess"
)

var ErrAuthentication = forge.ErrAuthentication

// ErrNoCredentials permits anonymous public reads; invalid or inaccessible credentials do not.
var ErrNoCredentials = fmt.Errorf("%w: no credential available", ErrAuthentication)

type TokenSource interface {
	Token(context.Context) (Token, error)
}

// TokenSourceFunc is a TokenSource written as a function, for tests that
// script a credential.
type TokenSourceFunc func(context.Context) (Token, error)

func (f TokenSourceFunc) Token(ctx context.Context) (Token, error) { return f(ctx) }

// CredentialKey is where dockhand keeps its GitHub login in the system
// credential store.
var CredentialKey = credential.Key{Service: "github.com/herbygillot/dockhand", Account: "github.com"}

type SystemCredentials struct {
	Store credential.Store
	Key   credential.Key
	// Lock is the file processes renewing the login take in turn;
	// LoginLock where empty.
	Lock string
	// Flow renews the login; GitHub's device flow where nil.
	Flow *DeviceFlow
	// renewed is a login renewed that the store didn't take, used until
	// it does; nil for credentials that keep none.
	renewed *pendingLogin
}

// SystemClient is GitHub as the person's login reaches it: GH_TOKEN or
// GITHUB_TOKEN where set, else the login dockhand keeps in the store. It
// is the one way dockhand makes that client.
func SystemClient(store credential.Store) *Client {
	return &Client{HTTP: fetch.Client, Credentials: SystemCredentials{Store: store, Key: CredentialKey, renewed: &pendingLogin{}}}
}

func (s SystemCredentials) Token(ctx context.Context) (Token, error) {
	if token := strings.TrimSpace(os.Getenv("GH_TOKEN")); token != "" {
		return resolvedToken(token, SourceGHEnvironment)
	}
	if token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); token != "" {
		return resolvedToken(token, SourceGitHubEnvironment)
	}
	if s.Store != nil {
		token, found, err := s.login(ctx, Token{})
		if found || err != nil {
			return token, err
		}
	}
	path, err := exec.LookPath("gh")
	if err != nil {
		return Token{}, fmt.Errorf("%w: run dockhand setup github, set GH_TOKEN or GITHUB_TOKEN, or authenticate with the GitHub CLI", ErrNoCredentials)
	}
	result, err := subprocess.Run(ctx, subprocess.Spec{Tool: "gh", Path: path, Args: []string{"auth", "token", "--hostname", "github.com"}, Limit: 1 << 16})
	output := result.Output
	if err != nil {
		if ctx.Err() != nil {
			return Token{}, ctx.Err()
		}
		return Token{}, fmt.Errorf("%w: run dockhand setup github, set GH_TOKEN or GITHUB_TOKEN, or run gh auth login", ErrNoCredentials)
	}
	return resolvedToken(strings.TrimSpace(string(output)), SourceGitHubCLI)
}

func validToken(token string) (string, error) {
	if token == "" || strings.ContainsAny(token, " \t\r\n") {
		return "", fmt.Errorf("%w: the discovered credential is empty or malformed", ErrAuthentication)
	}
	return token, nil
}

type CredentialSource string

const (
	SourceExplicit          CredentialSource = "explicit credential"
	SourceGHEnvironment     CredentialSource = "GH_TOKEN"
	SourceGitHubEnvironment CredentialSource = "GITHUB_TOKEN"
	SourceKeychain          CredentialSource = "Dockhand macOS Keychain"
	SourceGitHubCLI         CredentialSource = "GitHub CLI"
)

type Token struct {
	Secret string           `json:"-"`
	Source CredentialSource `json:"source"`
	// Expiry is when the token stops working; zero for one that doesn't
	// expire, or whose source doesn't say.
	Expiry time.Time `json:"-"`
}

func resolvedToken(secret string, source CredentialSource) (Token, error) {
	value, err := validToken(secret)
	if err != nil {
		return Token{}, fmt.Errorf("%w (source: %s)", err, source)
	}
	return Token{Secret: value, Source: source}, nil
}

func (s CredentialSource) rejected() error {
	remedy := "replace the explicitly configured credential"
	switch s {
	case SourceGHEnvironment, SourceGitHubEnvironment:
		remedy = fmt.Sprintf("replace or unset %s; it takes precedence over saved logins", s)
	case SourceKeychain:
		remedy = "run dockhand setup github to replace it, or dockhand setup github --logout to remove it"
	case SourceGitHubCLI:
		remedy = "run gh auth login --hostname github.com to replace it"
	default:
		s = SourceExplicit
	}
	return fmt.Errorf("%w: GitHub rejected the credential from %s; %s; no alternative credential was tried", ErrAuthentication, s, remedy)
}

// Renew is a token after GitHub rejected one: the login renewed, where the
// rejected token was its own, else whatever Token gives now.
func (s SystemCredentials) Renew(ctx context.Context, rejected Token) (Token, error) {
	if rejected.Source != SourceKeychain || s.Store == nil || os.Getenv("GH_TOKEN") != "" || os.Getenv("GITHUB_TOKEN") != "" {
		return s.Token(ctx)
	}
	token, found, err := s.login(ctx, rejected)
	if !found && err == nil {
		return s.Token(ctx)
	}
	return token, err
}
