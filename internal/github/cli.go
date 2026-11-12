package github

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/filelock"
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
		return "", fmt.Errorf("the GitHub CLI did not report a valid signed-in account")
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

// MarkDraft returns a pull request to draft, with gh pr ready --undo.
func (c CLI) MarkDraft(ctx context.Context, ref forge.PullRequestRef) error {
	if ref.Forge != forge.GitHub || ref.Number <= 0 || !ValidRepositoryName(ref.Repository) {
		return fmt.Errorf("github: invalid pull-request reference")
	}
	_, err := c.run(ctx, "pr", "ready", strconv.Itoa(ref.Number), "--undo", "--repo", ref.Repository)
	return err
}

// run runs gh, saying what it said when it fails.
func (c CLI) run(ctx context.Context, args ...string) ([]byte, error) {
	if c.Path == "" {
		return nil, errors.New("the GitHub CLI isn't installed")
	}
	// A lock the work holds, a submission's head lock, is handed to gh, so
	// a gh pr ready still running after dockhand died keeps it.
	result, err := subprocess.Run(ctx, subprocess.Spec{Tool: "gh", Path: c.Path, Args: args, Limit: 1 << 20, ExtraFiles: filelock.Carried(ctx)})
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
