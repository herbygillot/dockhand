package eval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
)

// memoEvaluate evaluates target's baseline in a session of its own through
// the evaluator's memo, as a look's probe does.
func memoEvaluate(t *testing.T, e *Evaluator, tree macports.Tree, target model.Target) macports.Snapshot {
	t.Helper()
	session, err := e.Open(t.Context(), tree)
	require.NoError(t, err)
	defer session.Close()
	source, err := tree.Select(target)
	require.NoError(t, err)
	snapshot, err := session.Memoized().Evaluate(t.Context(), source)
	require.NoError(t, err)
	return snapshot
}

// A memo answers a baseline evaluation again, in a session of its own,
// while what it read is as it was; a change to a file it sourced, the
// PortGroup it read from _resources, a glob's answer, or a host file it
// asked after, each evaluates it afresh, and keeps it anew.
func TestAMemoAnswersAnEvaluationWhileWhatItReadIsUnchanged(t *testing.T) {
	t.Parallel()
	e := liveEvaluator(t)
	e.Memo = &Memo{Directory: t.TempDir()}
	tree := fixtureTree(t)
	host := filepath.Join(t.TempDir(), "flag")
	putFile(t, tree.Root(), "devel/fixture/files/metadata.tcl", "set snapshot_minor 5\nset globbed [llength [glob -nocomplain -directory ${filespath} *.patch]]\nset flagged [file exists "+host+"]\n")
	target := model.Target{Name: "fixture", Portfile: "devel/fixture/Portfile"}

	first := memoEvaluate(t, e, tree, target)
	require.Equal(t, "7.5", first.Ports["fixture"].Version)
	require.Equal(t, "11.2", first.Ports["fixture-child"].Version)
	require.Equal(t, MemoCounts{Misses: 1}, e.Memo.Counts())
	slots, err := filepath.Glob(filepath.Join(e.Memo.Directory, "*", "*.json"))
	require.NoError(t, err)
	require.Len(t, slots, 1)
	var slot memoSlot
	data, err := os.ReadFile(slots[0])
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &slot))
	var inputs []string
	for _, input := range slot.Inputs {
		inputs = append(inputs, input.Command+" "+input.Path)
	}
	require.Subset(t, inputs, []string{"path devel/fixture/Portfile", "path devel/fixture/files/metadata.tcl", "path _resources/port1.0/group/dockhand-fixture-1.0.tcl", "glob devel/fixture/files/*.patch", "path " + host})

	// The kept slot is what answers, its root the evaluation's own: marked,
	// the mark comes back.
	port := slot.Ports["fixture"]
	port.Version = "kept"
	slot.Ports["fixture"] = port
	data, err = json.Marshal(slot)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(slots[0], data, 0o644))
	again := memoEvaluate(t, e, tree, target)
	require.Equal(t, "kept", again.Ports["fixture"].Version)
	require.Equal(t, filepath.Join(tree.Root(), "devel/fixture/files"), again.Ports["fixture"].Options["filespath"])
	require.Equal(t, first.Ports["fixture"].Options, again.Ports["fixture"].Options)
	require.Equal(t, MemoCounts{Hits: 1, Misses: 1}, e.Memo.Counts())

	for _, change := range []struct {
		name string
		make func()
	}{
		{"a file it sourced", func() {
			putFile(t, tree.Root(), "devel/fixture/files/metadata.tcl", "set snapshot_minor 6\nset globbed [llength [glob -nocomplain -directory ${filespath} *.patch]]\nset flagged [file exists "+host+"]\n")
		}},
		{"its PortGroup", func() {
			putFile(t, tree.Root(), "_resources/port1.0/group/dockhand-fixture-1.0.tcl", "set snapshot_major 8\n")
		}},
		{"a glob's answer", func() { putFile(t, tree.Root(), "devel/fixture/files/fix.patch", "") }},
		{"a host file it asked after", func() { require.NoError(t, os.WriteFile(host, nil, 0o644)) }},
	} {
		change.make()
		before := e.Memo.Counts()
		require.NotEqual(t, "kept", memoEvaluate(t, e, tree, target).Ports["fixture"].Version, change.name)
		require.Equal(t, before.Misses+1, e.Memo.Counts().Misses, change.name)
		require.NotEqual(t, "kept", memoEvaluate(t, e, tree, target).Ports["fixture"].Version, change.name)
		require.Equal(t, before.Hits+1, e.Memo.Counts().Hits, "%s: kept anew", change.name)
	}
	require.Equal(t, "8.6", memoEvaluate(t, e, tree, target).Ports["fixture"].Version)
}

// A host program a port ran is an input by its answer, asked again; one
// that can't be asked again as it was, named relatively, isn't kept; and
// a torn slot is a miss.
func TestAMemoKeepsOnlyWhatItCanTellTheChangeOf(t *testing.T) {
	t.Parallel()
	e := liveEvaluator(t)
	e.Memo = &Memo{Directory: t.TempDir()}
	tree := fixtureTree(t)
	putFile(t, tree.Root(), "devel/fixture/files/metadata.tcl", "set snapshot_minor [string length [exec /usr/bin/uname -s]]\n")
	target := model.Target{Name: "fixture", Portfile: "devel/fixture/Portfile"}
	memoEvaluate(t, e, tree, target)
	memoEvaluate(t, e, tree, target)
	require.Equal(t, MemoCounts{Hits: 1, Misses: 1}, e.Memo.Counts(), "uname's answer is the same")

	e.Memo = &Memo{Directory: t.TempDir()}
	putFile(t, tree.Root(), "devel/fixture/files/metadata.tcl", "set snapshot_minor [string length [exec uname -s]]\n")
	memoEvaluate(t, e, tree, target)
	memoEvaluate(t, e, tree, target)
	require.Equal(t, MemoCounts{Unkept: 2}, e.Memo.Counts())

	putFile(t, tree.Root(), "devel/fixture/files/metadata.tcl", "set snapshot_minor 5\n")
	memoEvaluate(t, e, tree, target)
	slots, err := filepath.Glob(filepath.Join(e.Memo.Directory, "*", "*.json"))
	require.NoError(t, err)
	require.Len(t, slots, 1)
	require.NoError(t, os.WriteFile(slots[0], []byte("{torn"), 0o644))
	require.Equal(t, "7.5", memoEvaluate(t, e, tree, target).Ports["fixture"].Version)
	require.Equal(t, MemoCounts{Misses: 2, Unkept: 2}, e.Memo.Counts())
}

// A look evaluates in a directory of its own, so a slot kept in one
// answers the same tree in another, globs and filespath alike, where the
// first measure answered none: a glob's matches named the run's directory.
func TestAMemoAnswersTheTreeInAnotherDirectory(t *testing.T) {
	t.Parallel()
	e := liveEvaluator(t)
	e.Memo = &Memo{Directory: t.TempDir()}
	tree := fixtureTree(t)
	putFile(t, tree.Root(), "devel/fixture/files/metadata.tcl", "set snapshot_minor [llength [glob -directory ${filespath} *.tcl]]\n")
	target := model.Target{Name: "fixture", Portfile: "devel/fixture/Portfile"}
	first := memoEvaluate(t, e, tree, target)
	moved := t.TempDir()
	require.NoError(t, os.CopyFS(moved, os.DirFS(tree.Root())))
	other, err := macports.NewTree(tree.Source(), moved, tree.Platform())
	require.NoError(t, err)
	again := memoEvaluate(t, e, other, target)
	require.Equal(t, MemoCounts{Hits: 1, Misses: 1}, e.Memo.Counts())
	require.Equal(t, first.Ports["fixture"].Version, again.Ports["fixture"].Version)
	require.Equal(t, filepath.Join(other.Root(), "devel/fixture/files"), again.Ports["fixture"].Options["filespath"])
}
