package change

import (
	"errors"
	"fmt"
	"io/fs"
)

// Tree is a working tree a prepared change can be written into, in the
// package's own path space.
//
// IT IS AN INTERFACE BECAUSE OF THIS PACKAGE'S OWN RULE: "nothing in
// this package accepts a host path, and nothing hands one to git". A
// File.Path is tree-relative; turning one into a directory a person
// actually has is the boundary, and the boundary belongs to the caller
// that holds a repository. So the join lives out there and the POLICY —
// what applying a change means, and what happens when part of it fails
// — lives here, where it can be decided once and tested without a disk.
type Tree interface {
	// Read returns a path's current bytes and whether it exists at all.
	// The two are separate answers: "absent" is what a later restore has
	// to put back, and an empty file is not absent.
	Read(path string) (content []byte, exists bool, err error)
	Write(path string, content []byte, mode fs.FileMode) error
	Remove(path string) error
}

// ErrPartiallyApplied is an application that failed and could not put
// back what it had already changed. It is its own error because the two
// outcomes are not the same fact: a refusal leaves a tree as it was, and
// this leaves one a person has to look at.
var ErrPartiallyApplied = errors.New("change: applied in part, and the tree could not be restored")

// ApplyTo writes a prepared change into a tree, ALL OR NOTHING.
//
// The alternative was the loop this replaces: write each file, return on
// the first error, and leave the rest of the tree in whatever state that
// reached. For a working tree under git that is recoverable, which is
// why it survived — but a half-applied change is a change a person can
// commit by accident, and "recoverable" is a worse contract than
// "unchanged".
//
// Restoration is bounded and that is what makes it affordable: the file
// set is enumerated in Files, it is a Portfile and its patches rather
// than a tree, and the prior state of each is one read. plan.Apply used
// to do this for the Portfile ALONE and had no production caller; a
// prepared change carries auxiliary files too, so covering the whole set
// is the point rather than a flourish.
//
// A restore that itself fails is reported as ErrPartiallyApplied
// wrapping both errors, because at that point the honest thing is to say
// which files moved and stop claiming anything about the tree.
func (p Prepared) ApplyTo(t Tree) error {
	type prior struct {
		path    string
		content []byte
		existed bool
	}
	undo := make([]prior, 0, len(p.Files))

	restore := func() error {
		var errs []error
		// Backwards: the last write is the first put back, so a path
		// touched twice ends at the state it started in.
		for i := len(undo) - 1; i >= 0; i-- {
			u := undo[i]
			if !u.existed {
				if err := t.Remove(u.path); err != nil {
					errs = append(errs, fmt.Errorf("removing %s: %w", u.path, err))
				}
				continue
			}
			if err := t.Write(u.path, u.content, 0o644); err != nil {
				errs = append(errs, fmt.Errorf("restoring %s: %w", u.path, err))
			}
		}
		return errors.Join(errs...)
	}

	fail := func(err error) error {
		if rerr := restore(); rerr != nil {
			return fmt.Errorf("%w: %w (restoring: %w)", ErrPartiallyApplied, err, rerr)
		}
		return err
	}

	for _, f := range p.Files {
		content, existed, err := t.Read(f.Path)
		if err != nil {
			return fail(fmt.Errorf("reading %s: %w", f.Path, err))
		}
		undo = append(undo, prior{path: f.Path, content: content, existed: existed})

		if f.Delete {
			if !existed {
				continue // already gone is the state asked for
			}
			if err := t.Remove(f.Path); err != nil {
				return fail(fmt.Errorf("removing %s: %w", f.Path, err))
			}
			continue
		}
		mode := f.Mode
		if mode == 0 {
			mode = 0o644
		}
		if err := t.Write(f.Path, f.Content, mode); err != nil {
			return fail(fmt.Errorf("writing %s: %w", f.Path, err))
		}
	}
	return nil
}
