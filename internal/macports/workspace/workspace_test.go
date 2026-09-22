package workspace_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/workspace"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
)

const portfileA = "PortSystem 1.0\nname a\nversion 1\ncategories devel\n"

type fixture struct {
	t    *testing.T
	root string
	repo *git.Repository
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	f := &fixture{t: t, root: t.TempDir()}
	f.run("init", "-q", "-b", "main")
	f.put("devel/a/Portfile", portfileA)
	f.put("devel/a/files/patch-a.diff", "--- a\n+++ b\n")
	f.put("devel/b/Portfile", strings.Replace(portfileA, "name a", "name b", 1))
	f.put("_resources/port1.0/group/fixture-1.0.tcl", "# group\n")
	f.put("_resources/port1.0/checks/real.list", "list\n")
	require.NoError(t, os.Symlink("real.list", filepath.Join(f.root, "_resources/port1.0/checks/link.list")))
	f.run("add", "-A", ".")
	f.run("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "commit", "-q", "-m", "fixture")
	var err error
	f.repo, err = git.Open(t.Context(), f.root, "")
	require.NoError(t, err)
	return f
}

func (f *fixture) run(args ...string) string {
	f.t.Helper()
	command := exec.CommandContext(f.t.Context(), "git", args...)
	command.Dir = f.root
	out, err := command.CombinedOutput()
	require.NoError(f.t, err, "%s", out)
	return strings.TrimSpace(string(out))
}

func (f *fixture) put(name, text string) {
	f.t.Helper()
	file := filepath.Join(f.root, filepath.FromSlash(name))
	require.NoError(f.t, os.MkdirAll(filepath.Dir(file), 0700))
	require.NoError(f.t, os.WriteFile(file, []byte(text), 0600))
}

func (f *fixture) source() record.Source {
	commit := f.run("rev-parse", "HEAD")
	return record.Source{Commit: record.ObjectID(commit), Tree: record.ObjectID(f.run("rev-parse", "HEAD^{tree}"))}
}

var (
	targetA = record.Target{Name: "a", Portfile: "devel/a/Portfile"}
	targetB = record.Target{Name: "b", Portfile: "devel/b/Portfile"}
)

func exists(root, name string) bool {
	_, err := os.Lstat(filepath.Join(root, filepath.FromSlash(name)))
	return err == nil
}

// A port's scope is its directory and _resources; the rest of the tree
// arrives on request, symlinks as symlinks, and the scope says what is there.
func TestEnsurePortMaterializesOnlyThePortAndSharedResources(t *testing.T) {
	f := newFixture(t)
	w, err := workspace.Open(t.Context(), f.repo, f.source())
	require.NoError(t, err)
	defer w.Close()
	require.False(t, exists(w.Root(), "devel/a/Portfile"), "Open materializes nothing")
	require.NoError(t, w.EnsurePort(t.Context(), targetA))
	for _, name := range []string{"devel/a/Portfile", "devel/a/files/patch-a.diff", "_resources/port1.0/group/fixture-1.0.tcl", "_resources/port1.0/checks/link.list"} {
		require.True(t, exists(w.Root(), name), name)
	}
	require.False(t, exists(w.Root(), "devel/b/Portfile"))
	info, err := os.Lstat(filepath.Join(w.Root(), "_resources/port1.0/checks/link.list"))
	require.NoError(t, err)
	require.NotZero(t, info.Mode()&os.ModeSymlink, "a tree symlink is a symlink")
	require.Equal(t, workspace.Scope{Ports: []string{"devel/a"}}, w.Scope())
	scope, ok := workspace.ScopeOf(w.Root())
	require.True(t, ok)
	require.False(t, scope.All)
	require.True(t, scope.Holds("devel/a"))
	require.False(t, scope.Holds("devel/b"))
	require.NoError(t, w.EnsurePort(t.Context(), targetA), "ensuring again is a no-op")
	require.NoError(t, w.EnsurePort(t.Context(), targetB))
	require.True(t, exists(w.Root(), "devel/b/Portfile"))
	require.Equal(t, []string{"devel/a", "devel/b"}, w.Scope().Ports)
	require.NoError(t, w.EnsureAll(t.Context()))
	require.True(t, w.Scope().All)
	require.NoError(t, w.EnsureAll(t.Context()))
	require.ErrorContains(t, w.EnsurePort(t.Context(), record.Target{Name: "x", Portfile: "Portfile"}), "category/port/Portfile")
	root := w.Root()
	require.NoError(t, w.Close())
	require.False(t, exists(root, "devel/a/Portfile"), "Close removes the directory")
	_, ok = workspace.ScopeOf(root)
	require.False(t, ok)
	_, ok = workspace.ScopeOf(f.root)
	require.False(t, ok, "a directory that is no workspace's is reported as such")
}

// An overlay replaces the edited file and shares every other tracked file
// by hardlink; untracked files in the base stay out of it; its git tree is
// the base's with the edit; and the base is untouched.
func TestOverlaySharesTrackedFilesAndReplacesTheEdited(t *testing.T) {
	f := newFixture(t)
	base, err := workspace.Open(t.Context(), f.repo, f.source())
	require.NoError(t, err)
	defer base.Close()
	require.NoError(t, base.EnsurePort(t.Context(), targetA))
	require.NoError(t, os.WriteFile(filepath.Join(base.Root(), "PortIndex"), []byte("index\n"), 0600), "an untracked file, as index staging writes")
	edited := strings.Replace(portfileA, "version 1", "version 2", 1)
	overlay, err := base.Overlay(t.Context(), []git.FileEdit{{Path: "devel/a/Portfile", After: []byte(edited)}})
	require.NoError(t, err)
	defer overlay.Close()
	require.NotEqual(t, base.Root(), overlay.Root())
	data, err := os.ReadFile(filepath.Join(overlay.Root(), "devel/a/Portfile"))
	require.NoError(t, err)
	require.Equal(t, edited, string(data))
	data, err = os.ReadFile(filepath.Join(base.Root(), "devel/a/Portfile"))
	require.NoError(t, err)
	require.Equal(t, portfileA, string(data), "the base keeps its file")
	baseInfo, err := os.Stat(filepath.Join(base.Root(), "devel/a/files/patch-a.diff"))
	require.NoError(t, err)
	overlayInfo, err := os.Stat(filepath.Join(overlay.Root(), "devel/a/files/patch-a.diff"))
	require.NoError(t, err)
	require.True(t, os.SameFile(baseInfo, overlayInfo), "an unchanged file is the base's inode")
	link, err := os.Lstat(filepath.Join(overlay.Root(), "_resources/port1.0/checks/link.list"))
	require.NoError(t, err)
	require.NotZero(t, link.Mode()&os.ModeSymlink, "a symlink is re-created, not linked to its target")
	require.False(t, exists(overlay.Root(), "PortIndex"), "untracked files are not part of an overlay")
	require.Equal(t, base.Scope(), overlay.Scope())
	require.Same(t, base, overlay.Base())
	tree, err := overlay.Tree(record.Platform{})
	require.NoError(t, err)
	require.Equal(t, base.Root(), tree.Base())
	require.Equal(t, overlay.Root(), tree.Root())
	edits := overlay.Edits()
	require.Len(t, edits, 1)
	require.True(t, edits[0].Before.Exists)
	require.Equal(t, uint32(0100644), edits[0].Mode)
	committed, err := overlay.Commit(t.Context())
	require.NoError(t, err)
	require.NotEqual(t, f.source().Tree, committed.Tree)
	_, content, err := f.repo.File(t.Context(), string(committed.Tree), "devel/a/Portfile")
	require.NoError(t, err)
	require.Equal(t, edited, string(content))
	unchanged, err := base.Commit(t.Context())
	require.NoError(t, err)
	require.Equal(t, f.source().Tree, unchanged.Tree, "a base commits to its own tree")
	_, err = base.Overlay(t.Context(), []git.FileEdit{{Path: "devel/b/Portfile", After: []byte("x")}})
	require.ErrorContains(t, err, "not one", "a file the base does not hold cannot be overlaid")
	_, err = base.Overlay(t.Context(), []git.FileEdit{{Path: "_resources/port1.0/checks/link.list", After: []byte("x")}})
	require.ErrorContains(t, err, "not one", "a symlink cannot be overlaid")
	overlayRoot := overlay.Root()
	require.NoError(t, overlay.Close())
	require.False(t, exists(overlayRoot, "devel/a/Portfile"))
	require.True(t, exists(base.Root(), "devel/a/files/patch-a.diff"), "closing an overlay leaves the base")
}

// An overlay over a wide base holds the edited port and _resources, not the
// rest of the tree: a candidate evaluation reads nothing else, and a
// thousand candidates over a whole tree would link a thousand trees.
func TestOverlayHoldsOnlyTheEditedPortAndResources(t *testing.T) {
	f := newFixture(t)
	base, err := workspace.Open(t.Context(), f.repo, f.source())
	require.NoError(t, err)
	defer base.Close()
	require.NoError(t, base.EnsureAll(t.Context()))
	overlay, err := base.Overlay(t.Context(), []git.FileEdit{{Path: "devel/a/Portfile", After: []byte("edited\n")}})
	require.NoError(t, err)
	defer overlay.Close()
	require.True(t, exists(overlay.Root(), "devel/a/files/patch-a.diff"))
	require.True(t, exists(overlay.Root(), "_resources/port1.0/group/fixture-1.0.tcl"))
	require.False(t, exists(overlay.Root(), "devel/b/Portfile"), "a port the edits do not touch is not linked")
	require.Equal(t, workspace.Scope{Ports: []string{"devel/a"}}, overlay.Scope())
	require.True(t, base.Scope().All, "the base keeps its own scope")
	require.NoError(t, overlay.EnsurePort(t.Context(), targetB))
	require.True(t, exists(overlay.Root(), "devel/b/Portfile"), "ensuring widens the overlay")
	require.Equal(t, []string{"devel/a", "devel/b"}, overlay.Scope().Ports)
}

// Ensuring through an overlay widens the base and links what it gained,
// keeping the overlay's edit.
func TestEnsureOnAnOverlayLinksWhatTheBaseGained(t *testing.T) {
	f := newFixture(t)
	base, err := workspace.Open(t.Context(), f.repo, f.source())
	require.NoError(t, err)
	defer base.Close()
	require.NoError(t, base.EnsurePort(t.Context(), targetA))
	overlay, err := base.Overlay(t.Context(), []git.FileEdit{{Path: "devel/a/Portfile", After: []byte("edited\n")}})
	require.NoError(t, err)
	defer overlay.Close()
	require.NoError(t, overlay.EnsurePort(t.Context(), targetB))
	require.True(t, exists(base.Root(), "devel/b/Portfile"))
	require.True(t, exists(overlay.Root(), "devel/b/Portfile"))
	require.Equal(t, []string{"devel/a", "devel/b"}, overlay.Scope().Ports)
	data, err := os.ReadFile(filepath.Join(overlay.Root(), "devel/a/Portfile"))
	require.NoError(t, err)
	require.Equal(t, "edited\n", string(data))
	require.NoError(t, overlay.EnsureAll(t.Context()))
	require.True(t, overlay.Scope().All)
}

// An overlay evaluates in the session bound to its base, with the
// overlay's files, and the snapshot names the overlay's root.
func TestAnOverlayEvaluatesInTheBaseSession(t *testing.T) {
	executable, err := exec.LookPath("port-tclsh")
	if err != nil {
		t.Skip("MacPorts Tcl required")
	}
	f := newFixture(t)
	base, err := workspace.Open(t.Context(), f.repo, f.source())
	require.NoError(t, err)
	defer base.Close()
	require.NoError(t, base.EnsurePort(t.Context(), targetA))
	evaluator := &eval.Evaluator{Executable: executable}
	batch, err := base.Batch(t.Context(), evaluator)
	require.NoError(t, err)
	again, err := base.Batch(t.Context(), evaluator)
	require.NoError(t, err)
	require.Same(t, batch, again, "one session per base")
	platform, err := evaluator.NativePlatform(t.Context())
	require.NoError(t, err)
	bound, err := base.Context(targetA, platform)
	require.NoError(t, err)
	before, err := batch.EvaluateSelected(t.Context(), bound)
	require.NoError(t, err)
	require.Equal(t, "1", before.Ports["a"].Version)
	require.Equal(t, base.Root(), before.Root)
	overlay, err := base.Overlay(t.Context(), []git.FileEdit{{Path: "devel/a/Portfile", After: []byte(strings.Replace(portfileA, "version 1", "version 2", 1))}})
	require.NoError(t, err)
	defer overlay.Close()
	shared, err := overlay.Batch(t.Context(), evaluator)
	require.NoError(t, err)
	require.Same(t, batch, shared, "an overlay's session is its base's")
	candidate, err := overlay.Context(targetA, platform)
	require.NoError(t, err)
	after, err := batch.EvaluateSelected(t.Context(), candidate)
	require.NoError(t, err)
	require.Equal(t, "2", after.Ports["a"].Version, "the overlay's Portfile is what the session reads")
	require.Equal(t, overlay.Root(), after.Root)
	require.Equal(t, filepath.Join(overlay.Root(), "devel/a/files"), after.Ports["a"].Options["filespath"], "an absolute path names the overlay")
	stranger, err := workspace.Open(t.Context(), f.repo, f.source())
	require.NoError(t, err)
	defer stranger.Close()
	require.NoError(t, stranger.EnsurePort(t.Context(), targetA))
	elsewhere, err := stranger.Context(targetA, platform)
	require.NoError(t, err)
	_, err = batch.EvaluateSelected(t.Context(), elsewhere)
	require.ErrorContains(t, err, "session is bound to", "another base is not served")
	require.NoError(t, base.Close())
	_, err = base.Batch(context.Background(), evaluator)
	require.ErrorContains(t, err, "closed")
}

// A registry hands out one workspace per source: the second holder gets the
// first's, with the scope it ensured, and the workspace closes when the
// last holder releases it. A nil registry shares nothing.
func TestRegistrySharesOneWorkspacePerSource(t *testing.T) {
	f := newFixture(t)
	registry := &workspace.Registry{}
	first, releaseFirst, err := registry.Acquire(t.Context(), f.repo, f.source())
	require.NoError(t, err)
	require.NoError(t, first.EnsurePort(t.Context(), targetA))
	second, releaseSecond, err := registry.Acquire(t.Context(), f.repo, f.source())
	require.NoError(t, err)
	require.Same(t, first, second, "one workspace per source")
	require.True(t, second.Scope().Holds("devel/a"), "the scope the first holder ensured")
	other, releaseOther, err := registry.Acquire(t.Context(), f.repo, record.Source{Tree: f.source().Tree, Base: "0000000000000000000000000000000000000001"})
	require.NoError(t, err)
	require.NotSame(t, first, other, "a different source identity is a different workspace")
	require.NoError(t, releaseFirst())
	require.NoError(t, releaseFirst(), "releasing twice releases once")
	require.DirExists(t, first.Root(), "a holder remains")
	require.NoError(t, releaseSecond())
	require.NoDirExists(t, first.Root(), "the last release closes it")
	require.NoError(t, registry.Close())
	require.NoDirExists(t, other.Root(), "closing the registry closes what is still held")
	require.NoError(t, releaseOther(), "a release after the registry closed is a no-op")

	var none *workspace.Registry
	alone, release, err := none.Acquire(t.Context(), f.repo, f.source())
	require.NoError(t, err)
	again, releaseAgain, err := none.Acquire(t.Context(), f.repo, f.source())
	require.NoError(t, err)
	require.NotSame(t, alone, again, "a nil registry shares nothing")
	require.NoError(t, release())
	require.NoDirExists(t, alone.Root())
	require.NoError(t, releaseAgain())
}
