package tree

import "github.com/herbygillot/dockhand/internal/macports/portindex"

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

func (t *Tree) buildDependents() (portindex.Reverse, error) {
	idx, err := t.lockedIndex()
	if err != nil {
		return portindex.Reverse{}, err
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
