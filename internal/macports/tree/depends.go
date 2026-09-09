package tree

import (
	"errors"
	"fmt"

	"github.com/herbygillot/dockhand/internal/macports/portindex"
)

// Dependents returns the tree's reverse dependency index: for each port
// name, lowercased, every port declaring it under depends_lib,
// depends_build or depends_run.
//
// It is built once per Tree, from one sequential pass over the whole
// PortIndex, and cached — hit or miss — for the Tree's lifetime. That
// pass reads the entire index (25.6 MB and 41630 entries on the
// maintainer's tree), which is why nothing asks for it unless a caller
// actually wants dependents; resolution alone never does.
//
// A dependent is reported at its own portdir, which for a subport is
// the parent's directory. That is the unit a cohort stages and edits: a
// subport has no directory of its own, and 51.8% of indexed names match
// no portdir basename anywhere in the tree, so the mapping cannot be
// derived from a name and is taken from the index's own portdir field.
//
// An index that cannot be walked comes back as an error, never as a
// partial map. A reverse index missing rows is a cohort missing members
// with nothing said about it, which is the one outcome a proposal must
// not produce.
func (t *Tree) Dependents() (portindex.Reverse, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.depsLoaded {
		t.deps, t.depsErr = t.buildDependents()
		t.depsLoaded = true
	}
	return t.deps, t.depsErr
}

// IndexReady reports whether the ports index this tree needs is there
// and openable, WITHOUT paying for the survey that reads it. It is the
// preflight for a road that will ask for dependents later.
//
// IT EXISTS BECAUSE THE CHEAP QUESTION WAS ASKED AFTER THE EXPENSIVE
// WORK. The dependent survey runs at SETTLE — a passing build proposes
// its cohort — so a bump on a tree with no PortIndex spent a full VM
// build first and only then discovered a missing file. Measured: 99
// seconds of clean delve build, then "tree has no PortIndex". The build
// was not wasted in the sense of being wrong, and it was entirely wasted
// in the sense that a stat would have said so before it started.
//
// It opens the index rather than stat-ing the file, and that is
// deliberate: opening is what the survey will do, so a preflight that
// merely stat-ed could pass and leave the same road failing later for a
// reason it had promised to have checked. The Tree CACHES the open, so
// the work is moved rather than repeated — on a tree that has an index,
// this reads the quick accelerator the survey was going to read anyway.
func (t *Tree) IndexReady() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, err := t.lockedIndex()
	return t.needsIndex(err)
}

// needsIndex is the sentence a missing index gets, wherever it is met.
//
// THE REMEDY TRAVELS WITH THE REFUSAL, because the sentinel alone names
// a missing file: "portindex: tree has no PortIndex: /path". It says
// nothing about what wanted the index, and nothing about `portindex`
// being the one command that makes one.
//
// It is one function because the two callers must say the SAME thing.
// The preflight is where most people will meet this now, and a preflight
// whose message was thinner than the late failure's would have made the
// earlier answer the worse one — which is the opposite of the point.
func (t *Tree) needsIndex(err error) error {
	if err == nil {
		return nil
	}
	if !errors.Is(err, portindex.ErrNoIndex) {
		return err
	}
	return fmt.Errorf("%w: dependent analysis needs it — run `portindex %s`", err, t.root)
}

func (t *Tree) buildDependents() (portindex.Reverse, error) {
	idx, err := t.lockedIndex()
	if err != nil {
		// THE REMEDY TRAVELS WITH THE REFUSAL, because the sentinel alone
		// names a missing file and not what wanted it or how to make one.
		// A person met this AFTER a passing VM build — the dependent
		// survey runs once the port is installed — and read "portindex:
		// tree has no PortIndex: /path", which says nothing about the
		// build having succeeded, nothing about what is now blocked, and
		// nothing about `portindex` being the one command that fixes it.
		//
		// It is wrapped here rather than at the sentinel because this is
		// where the NEED is known: indexLookup wants a name and says so
		// in its own words, and this wants the dependent graph.
		return portindex.Reverse{}, t.needsIndex(err)
	}
	return idx.Dependents()
}

// Maintained returns the tree's maintainer index: normalized maintainer
// key to the ports naming it, sorted. Built and cached on the same
// terms as Dependents, off the same kind of full pass.
//
// A port whose maintainers field is nomaintainer appears under no key.
// That is the field's meaning, and it covers better than a third of the
// tree — a member annotated "nomaintainer" in a cohort is one nobody
// can be asked about, which is the opposite of a maintainer named
// "nomaintainer".
func (t *Tree) Maintained() (map[string][]string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.maintLoaded {
		t.maint, t.maintErr = t.buildMaintained()
		t.maintLoaded = true
	}
	return t.maint, t.maintErr
}

func (t *Tree) buildMaintained() (map[string][]string, error) {
	idx, err := t.lockedIndex()
	if err != nil {
		return nil, err
	}
	return idx.ByMaintainer()
}
