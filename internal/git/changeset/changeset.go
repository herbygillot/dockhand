package changeset

import (
	"context"
	"fmt"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/record"
)

type Snapshot struct {
	Branch         string
	Commit         record.ObjectID
	Tree           record.ObjectID
	Head           record.ObjectID
	ModifiedPaths  []string
	UntrackedPaths []string
	checkout       bool
}

func CaptureCheckout(ctx context.Context, repo *git.Repository) (Snapshot, error) {
	if repo == nil {
		return Snapshot{}, fmt.Errorf("changeset: Git repository required")
	}
	captured, err := repo.CaptureCheckout(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	result := Snapshot{
		Branch:         captured.Branch,
		Tree:           record.ObjectID(captured.Tree),
		Head:           record.ObjectID(captured.Head),
		ModifiedPaths:  captured.ModifiedPaths,
		UntrackedPaths: captured.Untracked,
		checkout:       true,
	}
	if captured.ModifiedFiles == 0 {
		result.Commit = result.Head
	}
	return result, nil
}

func CaptureBranch(ctx context.Context, repo *git.Repository, branch string) (Snapshot, error) {
	if repo == nil {
		return Snapshot{}, fmt.Errorf("changeset: Git repository required")
	}
	commit, tree, err := repo.Branch(ctx, branch)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{
		Branch: branch,
		Commit: record.ObjectID(commit),
		Tree:   record.ObjectID(tree),
		Head:   record.ObjectID(commit),
	}, nil
}

func (s Snapshot) Source(base record.ObjectID) record.Source {
	return record.Source{Commit: s.Commit, Tree: s.Tree, Base: base}
}

func (s Snapshot) Provenance() *record.Checkout {
	if !s.checkout {
		return nil
	}
	return &record.Checkout{
		Branch:        s.Branch,
		Head:          s.Head,
		ModifiedFiles: len(s.ModifiedPaths),
	}
}

type Delta struct {
	Base      record.ObjectID
	Candidate record.ObjectID
	Paths     []string
}

func Between(ctx context.Context, repo *git.Repository, base, candidate record.ObjectID) (Delta, error) {
	if repo == nil {
		return Delta{}, fmt.Errorf("changeset: Git repository required")
	}
	paths, err := repo.ChangedPaths(ctx, string(base), string(candidate))
	if err != nil {
		return Delta{}, err
	}
	return Delta{Base: base, Candidate: candidate, Paths: paths}, nil
}

type Commit struct {
	Source  record.Source
	Message string
	Paths   []string
}

func DeriveSingleCommit(ctx context.Context, repo *git.Repository, source record.Source) (Commit, error) {
	if repo == nil {
		return Commit{}, fmt.Errorf("changeset: Git repository required")
	}
	base, err := repo.SingleParent(ctx, string(source.Commit))
	if err != nil {
		return Commit{}, err
	}
	source.Base = record.ObjectID(base)
	return readSingleCommit(ctx, repo, source, source.Base)
}

func ReadSingleCommit(ctx context.Context, repo *git.Repository, source record.Source) (Commit, error) {
	if repo == nil {
		return Commit{}, fmt.Errorf("changeset: Git repository required")
	}
	parent, err := repo.SingleParent(ctx, string(source.Commit))
	if err != nil {
		return Commit{}, err
	}
	return readSingleCommit(ctx, repo, source, record.ObjectID(parent))
}

func readSingleCommit(ctx context.Context, repo *git.Repository, source record.Source, parent record.ObjectID) (Commit, error) {
	if parent != source.Base {
		return Commit{}, fmt.Errorf("changeset: candidate must be exactly one commit above the recorded base")
	}
	message, err := repo.CommitMessage(ctx, string(source.Commit))
	if err != nil {
		return Commit{}, err
	}
	delta, err := Between(ctx, repo, source.Base, source.Commit)
	if err != nil {
		return Commit{}, err
	}
	return Commit{Source: source, Message: message, Paths: delta.Paths}, nil
}
