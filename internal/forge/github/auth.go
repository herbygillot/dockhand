package github

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/herbygillot/dockhand/v2/internal/credential"
	"github.com/herbygillot/dockhand/v2/internal/forge"
)

var ErrAuthentication = forge.ErrAuthentication

type TokenSource interface {
	Token(context.Context) (string, error)
}

type TokenSourceFunc func(context.Context) (string, error)

func (f TokenSourceFunc) Token(ctx context.Context) (string, error) { return f(ctx) }

type SystemCredentials struct {
	Store credential.Store
	Key   credential.Key
}

func (s SystemCredentials) Token(ctx context.Context) (string, error) {
	if token := strings.TrimSpace(os.Getenv("GH_TOKEN")); token != "" {
		return validToken(token)
	}
	if token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); token != "" {
		return validToken(token)
	}
	if s.Store != nil {
		token, err := s.Store.Get(ctx, s.Key)
		if err == nil {
			return validToken(token)
		}
		if !errors.Is(err, credential.ErrNotFound) {
			return "", fmt.Errorf("github: reading saved credential: %w", err)
		}
	}
	path, err := exec.LookPath("gh")
	if err != nil {
		return "", fmt.Errorf("%w: run dockhand auth login, set GH_TOKEN or GITHUB_TOKEN, or authenticate with the GitHub CLI", ErrAuthentication)
	}
	output, err := exec.CommandContext(ctx, path, "auth", "token", "--hostname", "github.com").Output()
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("%w: run dockhand auth login, set GH_TOKEN or GITHUB_TOKEN, or run gh auth login", ErrAuthentication)
	}
	return validToken(strings.TrimSpace(string(output)))
}

func validToken(token string) (string, error) {
	if token == "" || strings.ContainsAny(token, " \t\r\n") {
		return "", fmt.Errorf("%w: the discovered credential is empty or malformed", ErrAuthentication)
	}
	return token, nil
}
