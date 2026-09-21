package app

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/workflow"
)

// AdoptRequest names a branch a person prepared, to be tracked as a contribution.
type AdoptRequest struct {
	Branch string
	// Target names the port when the changed directory's main port is not
	// the one meant; empty infers it from the directory.
	Target string
	DryRun bool
	Squash bool
	// PullRequest adopts an open pull request's head instead of a local branch.
	PullRequest *record.PullRequestRef
	// KeepBody leaves the adopted pull request's body entirely its author's.
	KeepBody bool
}

// Adopt fetches master, so the branch's base can be checked against it, and
// records the branch as a contribution, or with DryRun only says what it
// would record.
func (s *Services) Adopt(ctx context.Context, request AdoptRequest) (workflow.AdoptResult, error) {
	platform, err := s.ports.NativePlatform(ctx)
	if err != nil {
		return workflow.AdoptResult{}, err
	}
	master, err := preparationSource(ctx, s.Workflow.Repo)
	if err != nil {
		return workflow.AdoptResult{}, err
	}
	return s.Workflow.AdoptContribution(ctx, workflow.AdoptRequest{Branch: request.Branch, Target: request.Target, Upstream: master.Commit, Platform: platform, DryRun: request.DryRun, Squash: request.Squash, PullRequest: request.PullRequest, KeepBody: request.KeepBody})
}

// ParsePullRequest reads a pull request as a number on the ports repository
// or as its GitHub URL.
func ParsePullRequest(value string) (record.PullRequestRef, error) {
	value = strings.TrimSpace(value)
	if number, err := strconv.Atoi(strings.TrimPrefix(value, "#")); err == nil && number > 0 {
		return record.PullRequestRef{Forge: forge.GitHub, Repository: macports.PortsRepository, Number: number, URL: "https://github.com/" + macports.PortsRepository + "/pull/" + strconv.Itoa(number)}, nil
	}
	if match := pullRequestURL.FindStringSubmatch(value); match != nil {
		number, _ := strconv.Atoi(match[2])
		return record.PullRequestRef{Forge: forge.GitHub, Repository: match[1], Number: number, URL: "https://github.com/" + match[1] + "/pull/" + match[2]}, nil
	}
	return record.PullRequestRef{}, fmt.Errorf("--pr takes a pull request number on %s or a GitHub pull request URL, not %q", macports.PortsRepository, value)
}

var pullRequestURL = regexp.MustCompile(`^https://github\.com/([^/\s]+/[^/\s]+)/pull/([1-9][0-9]*)(?:[/#?].*)?$`)
