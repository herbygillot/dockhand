package ledger

import (
	"context"
	"fmt"
	"slices"

	"github.com/herbygillot/dockhand/v2/internal/git"
	"github.com/herbygillot/dockhand/v2/internal/model"
)

const PinsPrefix = "refs/dockhand/objects/"

func (s *Store) sourcePins(ctx context.Context, state State) ([]git.RefChange, bool, error) {
	objects := make(map[string]string)
	add := func(id model.ObjectID, kind string) error {
		if id == "" {
			return nil
		}
		object := string(id)
		if !git.ValidObjectID(object) {
			return fmt.Errorf("%w: invalid source object %q", ErrInvalidState, id)
		}
		if previous, ok := objects[object]; ok && previous != kind {
			return fmt.Errorf("%w: source object %s used as both %s and %s", ErrInvalidState, id, previous, kind)
		}
		objects[object] = kind
		return nil
	}
	addSource := func(source model.Source) error {
		if err := add(source.Commit, "commit"); err != nil {
			return err
		}
		if err := add(source.Base, "commit"); err != nil {
			return err
		}
		return add(source.Tree, "tree")
	}
	for _, revision := range state.Revisions {
		if err := addSource(revision.Source); err != nil {
			return nil, false, err
		}
	}
	for _, job := range state.Jobs {
		if err := addSource(job.Spec.Source); err != nil {
			return nil, false, err
		}
	}
	for _, attempt := range state.Attempts {
		if err := addSource(attempt.Spec.Source); err != nil {
			return nil, false, err
		}
	}
	for _, publication := range state.Publications {
		if err := add(publication.Desired.Head, "commit"); err != nil {
			return nil, false, err
		}
	}
	var ids []string
	for id := range objects {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	if len(ids) == 0 {
		return nil, false, nil
	}
	types, err := s.repo.ObjectTypes(ctx, ids)
	if err != nil {
		return nil, false, err
	}
	currentRefs, err := s.repo.ReadRefs(ctx, PinsPrefix)
	if err != nil {
		return nil, false, err
	}
	var refs []git.RefChange
	added := false
	for _, id := range ids {
		if types[id] != objects[id] {
			return nil, false, fmt.Errorf("%w: source %s is %s, expected %s", ErrInvalidState, id, types[id], objects[id])
		}
		name := PinsPrefix + id
		current := currentRefs[name]
		if current.Exists && current.Object != id {
			return nil, false, fmt.Errorf("%w: pin %s points to %s", ErrInvalidState, name, current.Object)
		}
		added = added || !current.Exists
		refs = append(refs, git.RefChange{Name: name, Expected: current, Desired: git.RefValue{Exists: true, Object: id}})
	}
	return refs, added, nil
}
