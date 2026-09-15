package github

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/herbygillot/dockhand/internal/credential"
	"github.com/herbygillot/dockhand/internal/forge"
)

var ErrAuthentication = forge.ErrAuthentication

// ErrNoCredentials permits anonymous public reads; invalid or inaccessible credentials do not.
var ErrNoCredentials = fmt.Errorf("%w: no credential available", ErrAuthentication)

type TokenSource interface {
	Token(context.Context) (Token, error)
}

type TokenSourceFunc func(context.Context) (Token, error)

func (f TokenSourceFunc) Token(ctx context.Context) (Token, error) { return f(ctx) }

type SystemCredentials struct {
	Store credential.Store
	Key   credential.Key
}

func (s SystemCredentials) Token(ctx context.Context) (Token, error) {
	if token := strings.TrimSpace(os.Getenv("GH_TOKEN")); token != "" {
		return resolvedToken(token, SourceGHEnvironment)
	}
	if token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); token != "" {
		return resolvedToken(token, SourceGitHubEnvironment)
	}
	if s.Store != nil {
		token, err := s.Store.Get(ctx, s.Key)
		if err == nil {
			return resolvedToken(token, SourceKeychain)
		}
		if !errors.Is(err, credential.ErrNotFound) {
			return Token{}, fmt.Errorf("github: reading saved credential: %w", err)
		}
	}
	path, err := exec.LookPath("gh")
	if err != nil {
		return Token{}, fmt.Errorf("%w: run dockhand auth login, set GH_TOKEN or GITHUB_TOKEN, or authenticate with the GitHub CLI", ErrNoCredentials)
	}
	output, err := exec.CommandContext(ctx, path, "auth", "token", "--hostname", "github.com").Output()
	if err != nil {
		if ctx.Err() != nil {
			return Token{}, ctx.Err()
		}
		return Token{}, fmt.Errorf("%w: run dockhand auth login, set GH_TOKEN or GITHUB_TOKEN, or run gh auth login", ErrNoCredentials)
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
		remedy = "run dockhand auth login to replace it, or dockhand auth logout to remove it"
	case SourceGitHubCLI:
		remedy = "run gh auth login --hostname github.com to replace it"
	default:
		s = SourceExplicit
	}
	return fmt.Errorf("%w: GitHub rejected the credential from %s; %s; no alternative credential was tried", ErrAuthentication, s, remedy)
}
