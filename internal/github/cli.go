package github

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/subprocess"
)

// CLI is the GitHub CLI, gh, which acts on GitHub as its own app with its
// own login, which dockhand doesn't read here. An organization that
// refuses dockhand's app may take it (D8). Only its documented commands
// are run.
type CLI struct {
	// Path is gh's; found on the PATH when empty.
	Path string
}

// Login is the account gh is signed in as on github.com.
func (c CLI) Login(ctx context.Context) (string, error) {
	out, err := c.run(ctx, "api", "--hostname", "github.com", "user", "--jq", ".login")
	if err != nil {
		return "", err
	}
	login := strings.TrimSpace(string(out))
	if login == "" || strings.ContainsAny(login, " \t\r\n") {
		return "", fmt.Errorf("the GitHub CLI named no account it's signed in as")
	}
	return login, nil
}

// MarkReady takes a draft pull request out of draft, with gh pr ready.
func (c CLI) MarkReady(ctx context.Context, ref forge.PullRequestRef) error {
	if ref.Forge != forge.GitHub || ref.Number <= 0 || !ValidRepositoryName(ref.Repository) {
		return fmt.Errorf("github: invalid pull-request reference")
	}
	_, err := c.run(ctx, "pr", "ready", strconv.Itoa(ref.Number), "--repo", ref.Repository)
	return err
}

// run runs gh, saying what it said when it fails.
func (c CLI) run(ctx context.Context, args ...string) ([]byte, error) {
	path := c.Path
	if path == "" {
		found, err := exec.LookPath("gh")
		if err != nil {
			return nil, errors.New("the GitHub CLI isn't installed")
		}
		path = found
	}
	result, err := subprocess.Run(ctx, subprocess.Spec{Tool: "gh", Path: path, Args: args, Limit: 1 << 20})
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		said := strings.TrimSpace(string(result.Stderr))
		if said == "" {
			said = err.Error()
		}
		return nil, fmt.Errorf("gh %s: %s", strings.Join(args[:2], " "), said)
	}
	return result.Output, nil
}
