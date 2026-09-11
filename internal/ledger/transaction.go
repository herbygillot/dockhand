package ledger

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/git"
)

type transactionContextKey struct{}

func (s *Store) Update(ctx context.Context, change func(context.Context, *Transaction) error) error {
	if ctx.Value(transactionContextKey{}) != nil {
		return ErrNestedTransaction
	}
	if change == nil {
		return errors.New("ledger: missing transaction callback")
	}
	ctx, cancel := context.WithTimeout(ctx, s.options.OperationTimeout)
	defer cancel()
	lock, err := s.lock(ctx)
	if err != nil {
		return err
	}
	defer lock.Close()

	before, err := s.read(ctx)
	if errors.Is(err, ErrNoState) {
		before = Snapshot{State: NewState()}
	} else if err != nil {
		return err
	}
	original, err := Encode(before.State)
	if err != nil {
		return err
	}
	tx := &Transaction{Version: before.Version, State: before.State}
	ctx = context.WithValue(ctx, transactionContextKey{}, true)
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := change(ctx, tx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := Encode(tx.State)
	if err != nil {
		return err
	}
	for _, ref := range tx.Refs {
		if ref.Name == StateRef || strings.HasPrefix(ref.Name, PinsPrefix) || ref.Name == NotesRef {
			return fmt.Errorf("ledger: ref %s is managed by the store", ref.Name)
		}
	}
	pins, pinsAdded, err := s.sourcePins(ctx, tx.State)
	if err != nil {
		return err
	}
	changed := before.Version == "" || !bytes.Equal(original, data) || len(tx.Refs) != 0
	if !changed && !pinsAdded {
		return nil
	}
	version := string(before.Version)
	if changed {
		blob, err := s.repo.WriteBlob(ctx, data)
		if err != nil {
			return err
		}
		tree, err := s.repo.WriteTree(ctx, []git.TreeEntry{{Name: stateFile, Mode: 0o100644, Type: "blob", Object: blob}})
		if err != nil {
			return err
		}
		var parents []string
		if before.Version != "" {
			parents = []string{string(before.Version)}
		}
		identity := git.Signature{Name: "Dockhand", Email: "dockhand@localhost", When: time.Now().UTC()}
		version, err = s.repo.WriteCommit(ctx, git.Commit{Tree: tree, Parents: parents, Message: "dockhand: update ledger\n", Author: identity, Committer: identity})
		if err != nil {
			return err
		}
	}
	refs := []git.RefChange{{Name: StateRef, Expected: git.RefValue{Exists: before.Version != "", Object: string(before.Version)}, Desired: git.RefValue{Exists: true, Object: version}}}
	refs = append(refs, pins...)
	refs = append(refs, tx.Refs...)
	if err := s.repo.UpdateRefs(ctx, refs); err != nil {
		if errors.Is(err, git.ErrRefUpdateUncertain) {
			return fmt.Errorf("%w: %w", ErrCommitUncertain, err)
		}
		if errors.Is(err, git.ErrRefConflict) {
			return fmt.Errorf("%w: %w", ErrConflict, err)
		}
		return err
	}
	return nil
}
