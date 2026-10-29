package comparison

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	archivefile "github.com/herbygillot/dockhand/internal/archive"
	"github.com/herbygillot/dockhand/internal/comparison/comparisontest"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/assess"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/project"
)

// treeFiles are trees' files by path, for the collector's Files.
type treeFiles map[model.ObjectID]map[string]string

func (f treeFiles) File(_ context.Context, tree model.ObjectID, path string) ([]byte, bool, error) {
	data, ok := f[tree][path]
	return []byte(data), ok, nil
}

// The patches a revision drops are checked against its source too, read
// from the base's tree: libuv's #34620 dropped patch-libuv-legacy.diff,
// which no longer applied, and another (the libuv run's finding 2). A
// patch whose file isn't in the tree is unchecked, with why, and an
// archive a kept reading spared fetching is fetched for the check.
func TestTheBasesPatchesARevisionDropsAreChecked(t *testing.T) {
	t.Parallel()
	p := comparisontest.NewPlanner(t)
	legacy := "--- src/unix/core.c\n+++ src/unix/core.c\n@@ -1 +1 @@\n-legacy\n+legacy, patched\n"
	still := "--- src/uv.c\n+++ src/uv.c\n@@ -1 +1 @@\n-uv\n+uv, patched\n"
	before, after := model.ObjectID("before"), model.ObjectID("after")
	files := treeFiles{before: {"devel/libuv/files/patch-legacy.diff": legacy, "devel/libuv/files/patch-still.diff": still}, after: {"devel/libuv/Portfile": "name libuv\n"}}
	options := map[string]string{"worksrcdir": "libuv-1.52.1", "patch.pre_args": "-p0", "extract.rename": "0"}
	p.Add(after, comparisontest.PlannedPort{Info: macports.PortInfo{Name: "libuv", Version: "1.52.1", Options: options}, Archives: map[string]map[string]string{"libuv-1.52.1.tar.gz": {"src/unix/core.c": "rewritten\n", "src/uv.c": "uv\n"}}})
	info, plan, err := p.ArchivePlan(t.Context(), model.Source{Tree: after}, "devel/libuv", "libuv")
	require.NoError(t, err)
	had := macports.PortInfo{Name: "libuv", Options: map[string]string{"patchfiles": "patch-legacy.diff patch-still.diff patch-lost.diff"}}
	directory := t.TempDir()
	c := Collector{Files: files}
	patches := c.patchesFor(t.Context(), patchRequest{infos: [2]macports.PortInfo{had, info}, sources: [2]model.Source{{Tree: before}, {Tree: after}}, portdirs: [2]string{"devel/libuv", "devel/libuv"}, plan: plan, scratch: directory})
	byName := map[string]assess.Patch{}
	for _, patch := range patches {
		byName[patch.Name] = patch
	}
	require.Len(t, byName, 3)
	require.True(t, byName["patch-legacy.diff"].Dropped)
	require.True(t, byName["patch-legacy.diff"].Checked)
	require.False(t, byName["patch-legacy.diff"].Applies)
	require.True(t, byName["patch-still.diff"].Applies)
	require.False(t, byName["patch-lost.diff"].Checked)
	require.Equal(t, "devel/libuv/files/patch-lost.diff isn't in the tree", byName["patch-lost.diff"].Detail)
	require.Equal(t, int64(1), p.Fetches.Load(), "fetched for the check, as no reading fetched it")
}

// A revision's archives are paired by the port's source set: one the base
// fetched and the candidate doesn't is represented, unfetched, where it
// had been read and dropped, and the rest are paired by what they are,
// never by the order the plans name them (the architecture review's
// finding 2, its second probe).
func TestARemovedArchiveIsRepresented(t *testing.T) {
	t.Parallel()
	p := comparisontest.NewPlanner(t)
	p.Add("base", comparisontest.PlannedPort{Info: macports.PortInfo{Name: "demo"}, Archives: map[string]map[string]string{
		"main-1.0.tar.gz": {"LICENSE": "MIT\n"}, "extra-1.0.tar.gz": {"LICENSE": "GPL\n"}, "aaa-docs.tar.gz": {"README": "old\n"},
	}})
	p.Add("candidate", comparisontest.PlannedPort{Info: macports.PortInfo{Name: "demo"}, Archives: map[string]map[string]string{
		"main-1.1.tar.gz": {"LICENSE": "MIT\n"}, "zzz-docs.tar.gz": {"README": "new\n"},
	}})
	c := Collector{Readings: project.Cache{Directory: t.TempDir()}}
	var infos [2]macports.PortInfo
	var plans [2][]macports.Distfile
	for i, tree := range []model.ObjectID{"base", "candidate"} {
		var err error
		infos[i], plans[i], err = p.ArchivePlan(t.Context(), model.Source{Tree: tree}, "devel/demo", "demo")
		require.NoError(t, err)
	}
	pairs, unpaired, _, problem, _ := c.readPlans(t.Context(), infos, plans, true, t.TempDir())
	require.Empty(t, problem)
	require.Len(t, pairs, 1)
	require.Equal(t, macports.SourceMatch{Before: "main-1.0.tar.gz", After: "main-1.1.tar.gz", Status: macports.SourceMatched, Basis: macports.ByPattern}, pairs[0].Match)
	require.Equal(t, []macports.SourceMatch{
		{Before: "aaa-docs.tar.gz", Status: macports.SourceUncertain}, {Before: "extra-1.0.tar.gz", Status: macports.SourceUncertain},
		{After: "zzz-docs.tar.gz", Status: macports.SourceUncertain},
	}, unpaired, "two of the base's left and one of the candidate's: which replaces which isn't guessed")
	require.EqualValues(t, 2, p.Fetches.Load(), "only what's compared is fetched")

	result := assess.Assess(assess.Input{Port: infos[1], Base: infos[0], Pairs: pairs, Unpaired: unpaired})
	held := result.Holds()
	require.Len(t, held, 1)
	require.Contains(t, held[0], "zzz-docs.tar.gz corresponds to none of the base's archives (aaa-docs.tar.gz, extra-1.0.tar.gz) by name")
}

// A distfile MacPorts doesn't extract is read as the file it is, and
// compared as one, holding nothing for being no archive: bashunit fetches
// a script, with extract.only empty, which read as "archive/tar: invalid
// tar header" and held its update (field testing at 285a496f).
func TestADistfileMacPortsDoesntExtractIsComparedAsAFile(t *testing.T) {
	t.Parallel()
	p := comparisontest.NewPlanner(t)
	only := map[string]string{"extract.only": ""}
	p.Add("base", comparisontest.PlannedPort{Info: macports.PortInfo{Name: "bashunit", Options: only}, Archives: map[string]map[string]string{"bashunit": {"": "#!/bin/bash\necho 0.51.0\n"}}})
	p.Add("candidate", comparisontest.PlannedPort{Info: macports.PortInfo{Name: "bashunit", Options: only}, Archives: map[string]map[string]string{"bashunit": {"": "#!/bin/bash\necho 0.52.0\n"}}})
	c := Collector{Readings: project.Cache{Directory: t.TempDir()}}
	var infos [2]macports.PortInfo
	var plans [2][]macports.Distfile
	for i, tree := range []model.ObjectID{"base", "candidate"} {
		var err error
		infos[i], plans[i], err = p.ArchivePlan(t.Context(), model.Source{Tree: tree}, "devel/bashunit", "bashunit")
		require.NoError(t, err)
	}
	pairs, unpaired, fetched, problem, _ := c.readPlans(t.Context(), infos, plans, true, t.TempDir())
	require.Empty(t, problem)
	require.Empty(t, unpaired)
	require.Len(t, pairs, 1)
	require.Equal(t, project.Flat, pairs[0].After.Layout)
	require.Empty(t, fetched, "what MacPorts doesn't extract isn't patched")
	result := assess.Assess(assess.Input{Port: infos[1], Base: infos[0], Pairs: pairs})
	require.False(t, result.Held())
}

// A binary distfile is said by its own name and what it is: the line read
// "1password-cli ships source-906474379 is a macOS installer package",
// the temporary file's name in a sentence that didn't parse (field
// testing at 5eb101a3).
func TestABinaryDistfileIsSaidByItsName(t *testing.T) {
	err := fmt.Errorf("reading: %w", &archivefile.BinaryError{File: "op_apple_universal_v2.31.0.pkg", Kind: "a macOS installer package"})
	require.Equal(t, "1password-cli ships a macOS installer package (op_apple_universal_v2.31.0.pkg), with no source to compare", binaryReason("1password-cli", err))
}
