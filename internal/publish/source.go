package publish

import (
	"context"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/herbygillot/dockhand/v2/internal/record"
)

// UntrackedSource derives a single-commit contribution and its port directory.
// Plan separately confirms that this parent belongs to the selected upstream.
func (s *Service) UntrackedSource(ctx context.Context, source record.Source) (record.Source, string, error) {
	if s == nil || s.Repo == nil {
		return record.Source{}, "", fmt.Errorf("publish: Git is required")
	}
	base, err := s.Repo.SingleParent(ctx, string(source.Commit))
	if err != nil {
		return record.Source{}, "", fmt.Errorf("%w: %v", ErrPrecondition, err)
	}
	source.Base = record.ObjectID(base)
	_, paths, err := s.Repo.Contribution(ctx, base, string(source.Commit))
	if err != nil {
		return record.Source{}, "", fmt.Errorf("%w: %v", ErrPrecondition, err)
	}
	var directory string
	for _, name := range paths {
		parts := strings.Split(name, "/")
		if !fs.ValidPath(name) || len(parts) < 3 {
			return record.Source{}, "", fmt.Errorf("%w: %s is outside a port directory", ErrPrecondition, name)
		}
		current := path.Join(parts[0], parts[1])
		if directory != "" && directory != current {
			return record.Source{}, "", fmt.Errorf("%w: publication currently requires changes in one port directory", ErrPrecondition)
		}
		directory = current
	}
	if directory == "" {
		return record.Source{}, "", fmt.Errorf("%w: contribution is empty", ErrPrecondition)
	}
	return source, path.Join(directory, "Portfile"), nil
}

func (s *Service) SourceContent(ctx context.Context, source record.Source, targets []record.Target) (record.PublicationContent, error) {
	if len(targets) != 1 {
		return record.PublicationContent{}, fmt.Errorf("%w: publication currently requires one verified target", ErrPrecondition)
	}
	message, paths, err := s.Repo.Contribution(ctx, string(source.Base), string(source.Commit))
	if err != nil {
		return record.PublicationContent{}, fmt.Errorf("%w: %v", ErrPrecondition, err)
	}
	directory := path.Dir(targets[0].Portfile) + "/"
	if len(paths) == 0 {
		return record.PublicationContent{}, fmt.Errorf("%w: contribution is empty", ErrPrecondition)
	}
	for _, name := range paths {
		if !strings.HasPrefix(name, directory) {
			return record.PublicationContent{}, fmt.Errorf("%w: %s is outside the verified port directory", ErrPrecondition, name)
		}
	}
	lines := strings.SplitN(strings.TrimSpace(message), "\n", 2)
	title, body := strings.TrimSpace(lines[0]), ""
	if len(lines) == 2 {
		body = strings.TrimSpace(lines[1])
	}
	if title == "" {
		return record.PublicationContent{}, fmt.Errorf("%w: commit title is empty", ErrPrecondition)
	}
	return record.PublicationContent{Head: source.Commit, Title: title, Body: body}, nil
}
