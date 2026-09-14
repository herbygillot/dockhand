package github

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/herbygillot/dockhand/v2/internal/forge"
)

var ErrAuthentication = forge.ErrAuthentication

type TokenSource interface {
	Token(context.Context) (string, error)
}

type TokenSourceFunc func(context.Context) (string, error)

func (f TokenSourceFunc) Token(ctx context.Context) (string, error) { return f(ctx) }

type SystemCredentials struct{}

func (SystemCredentials) Token(ctx context.Context) (string, error) {
	if token := strings.TrimSpace(os.Getenv("GH_TOKEN")); token != "" {
		return validToken(token)
	}
	if token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); token != "" {
		return validToken(token)
	}
	path, err := exec.LookPath("gh")
	if err != nil {
		return "", fmt.Errorf("%w: set GH_TOKEN or GITHUB_TOKEN, or authenticate with the GitHub CLI", ErrAuthentication)
	}
	output, err := exec.CommandContext(ctx, path, "auth", "token", "--hostname", "github.com").Output()
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("%w: set GH_TOKEN or GITHUB_TOKEN, or run gh auth login", ErrAuthentication)
	}
	return validToken(strings.TrimSpace(string(output)))
}

func validToken(token string) (string, error) {
	if token == "" || strings.ContainsAny(token, " \t\r\n") {
		return "", fmt.Errorf("%w: the discovered credential is empty or malformed", ErrAuthentication)
	}
	return token, nil
}
