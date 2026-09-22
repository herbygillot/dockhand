package publish

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/herbygillot/dockhand/internal/git/changeset"
	"github.com/herbygillot/dockhand/internal/record"
)

// UntrackedSource derives a single-commit contribution and its port directory.
// Plan separately confirms that this parent belongs to the selected upstream.
func (s *Service) UntrackedSource(ctx context.Context, source record.Source) (record.Source, string, error) {
	if s == nil || s.Repo == nil {
		return record.Source{}, "", fmt.Errorf("publish: Git is required")
	}
	commit, err := changeset.DeriveSingleCommit(ctx, s.Repo, source)
	if err != nil {
		return record.Source{}, "", fmt.Errorf("%w: %v", ErrPrecondition, err)
	}
	source = commit.Source
	scope, err := changeset.ScopeOf(commit.Paths)
	if err != nil {
		return record.Source{}, "", fmt.Errorf("%w: the contribution %s", ErrPrecondition, strings.TrimPrefix(err.Error(), changeset.ErrScope.Error()+": "))
	}
	return source, scope.Portfile(), nil
}

func (s *Service) SourceContent(ctx context.Context, source record.Source, targets []record.Target) (record.PublicationContent, error) {
	if len(targets) != 1 {
		return record.PublicationContent{}, fmt.Errorf("%w: publication currently requires one verified target", ErrPrecondition)
	}
	commit, err := changeset.ReadSingleCommit(ctx, s.Repo, source)
	if err != nil {
		return record.PublicationContent{}, fmt.Errorf("%w: %v", ErrPrecondition, err)
	}
	scope := changeset.Scope{Directory: path.Dir(targets[0].Portfile)}
	if len(commit.Paths) == 0 {
		return record.PublicationContent{}, fmt.Errorf("%w: contribution is empty", ErrPrecondition)
	}
	for _, name := range commit.Paths {
		if !scope.Within(name) {
			return record.PublicationContent{}, fmt.Errorf("%w: %s is outside the verified port directory and %s", ErrPrecondition, name, changeset.Resources)
		}
	}
	lines := strings.SplitN(strings.TrimSpace(commit.Message), "\n", 2)
	title, body := strings.TrimSpace(lines[0]), ""
	if len(lines) == 2 {
		body = strings.TrimSpace(lines[1])
	}
	if title == "" {
		return record.PublicationContent{}, fmt.Errorf("%w: commit title is empty", ErrPrecondition)
	}
	return record.PublicationContent{Head: source.Commit, Title: title, Body: body}, nil
}
