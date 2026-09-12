package ledger

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/git"
	"github.com/herbygillot/dockhand/v2/internal/lock"
	"github.com/herbygillot/dockhand/v2/internal/record"
)

var (
	ErrNotImplemented    = errors.New("ledger: note export is not implemented")
	ErrNoState           = errors.New("ledger: state ref does not exist")
	ErrConflict          = errors.New("ledger: transaction precondition failed")
	ErrCommitUncertain   = errors.New("ledger: transaction outcome is uncertain")
	ErrNestedTransaction = errors.New("ledger: nested transactions are not allowed")
	ErrInvalidState      = errors.New("ledger: invalid state")
)

const stateFile = "state.json"

type Options struct {
	WriterLock       *lock.File
	LockTimeout      time.Duration
	OperationTimeout time.Duration
}

type Store struct {
	repo    git.Repository
	options Options
}

func New(repo *git.Repository, options Options) (*Store, error) {
	if repo == nil || !filepath.IsAbs(repo.CommonDir) || !filepath.IsAbs(repo.Root) {
		return nil, errors.New("ledger: an opened repository is required")
	}
	if options.WriterLock.Path() == "" {
		return nil, errors.New("ledger: an initialized writer lock is required")
	}
	if options.LockTimeout < 0 || options.OperationTimeout < 0 {
		return nil, errors.New("ledger: timeouts must not be negative")
	}
	if options.LockTimeout == 0 {
		options.LockTimeout = 5 * time.Second
	}
	if options.OperationTimeout == 0 {
		options.OperationTimeout = 30 * time.Second
	}
	return &Store{repo: *repo, options: options}, nil
}

type Transaction struct {
	Version record.ObjectID
	State   State
	Refs    []git.RefChange
}

func (s *Store) Read(ctx context.Context) (Snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, s.options.OperationTimeout)
	defer cancel()
	return s.read(ctx)
}

func (s *Store) read(ctx context.Context) (Snapshot, error) {
	ref, err := s.repo.ReadRef(ctx, StateRef)
	if err != nil {
		return Snapshot{}, err
	}
	if !ref.Exists {
		return Snapshot{}, ErrNoState
	}
	kind, err := s.repo.ObjectType(ctx, ref.Object)
	if err != nil {
		return Snapshot{}, err
	}
	if kind != "commit" {
		return Snapshot{}, fmt.Errorf("%w: state ref points to %s", ErrInvalidState, kind)
	}
	entries, err := s.repo.ReadTree(ctx, ref.Object)
	if err != nil {
		return Snapshot{}, err
	}
	if len(entries) != 1 || entries[0].Name != stateFile || entries[0].Mode != 0o100644 || entries[0].Type != "blob" {
		return Snapshot{}, fmt.Errorf("%w: expected only %s in ledger tree", ErrInvalidState, stateFile)
	}
	data, err := s.repo.ReadBlob(ctx, entries[0].Object)
	if err != nil {
		return Snapshot{}, err
	}
	state, err := Decode(data)
	if err != nil {
		return Snapshot{}, fmt.Errorf("ledger: reading %s: %w", ref.Object, err)
	}
	return Snapshot{Version: record.ObjectID(ref.Object), State: state}, nil
}

func (s *Store) ExportNote(ctx context.Context, revision record.RevisionID) error {
	return ErrNotImplemented
}
