package cmdtest

import (
	"context"
	"errors"

	"github.com/herbygillot/dockhand/internal/forge"
)

// SignedIn stands in for the GitHub CLI: signed in as Account, it records
// what it marks ready; with no login, it isn't installed.
type SignedIn struct {
	Account string
	Readied *[]int
}

func (c SignedIn) Login(context.Context) (string, error) {
	if c.Account == "" {
		return "", errors.New("the GitHub CLI isn't installed")
	}
	return c.Account, nil
}

func (c SignedIn) MarkReady(_ context.Context, ref forge.PullRequestRef) error {
	*c.Readied = append(*c.Readied, ref.Number)
	return nil
}

func (c SignedIn) MarkDraft(context.Context, forge.PullRequestRef) error { return nil }
