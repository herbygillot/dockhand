package outdated_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/outdated"
	"github.com/herbygillot/dockhand/internal/testsupport"
	"github.com/herbygillot/dockhand/internal/upstream"
	"github.com/stretchr/testify/require"
)

func TestObserveKeepsCommittedSourceAndCleansWorkspace(t *testing.T) {
	executable := testsupport.MacPortsTclsh(t)
	root := t.TempDir()
	path := filepath.Join(root, "devel", "fixture", "Portfile")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	require.NoError(t, os.WriteFile(path, []byte("PortSystem 1.0\nname fixture\nversion 1.0\ncategories devel\n"), 0600))
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "fixture"}} {
		command := exec.CommandContext(t.Context(), "git", args...)
		command.Dir = root
		output, err := command.CombinedOutput()
		require.NoError(t, err, "%s", output)
	}
	repo, err := git.Open(t.Context(), root, "")
	require.NoError(t, err)
	before, err := repo.ReadRefs(t.Context(), "refs/heads/")
	require.NoError(t, err)
	head, err := repo.Resolve(t.Context(), "HEAD^{commit}")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte("invalid checkout edits\n"), 0600))
	scratch := t.TempDir()
	t.Setenv("TMPDIR", scratch)
	ports := &eval.Evaluator{Executable: executable, Adapter: testsupport.BaseAdapter()}
	service := outdated.Service{Repo: repo, Ports: ports, Upstream: &upstream.Service{Ports: ports, Versions: ports}}
	result, err := service.Observe(t.Context(), outdated.Selection{Ports: []string{"missing", "fixture", "fixture"}})
	require.NoError(t, err)
	require.Equal(t, head, string(result.Source.Commit))
	require.Len(t, result.Ports, 2)
	require.Equal(t, "missing", result.Ports[0].Selector)
	require.Equal(t, "fixture", result.Ports[1].Selector)
	require.Equal(t, "1.0", result.Ports[1].CurrentVersion)
	for _, port := range result.Ports {
		require.Equal(t, upstream.Unknown, port.Assessment)
		require.NotEmpty(t, port.Detail)
	}
	after, err := repo.ReadRefs(t.Context(), "refs/heads/")
	require.NoError(t, err)
	require.Equal(t, before, after)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "invalid checkout edits\n", string(data))
	// Discovery must release its temporary source on unknown results: the
	// process's run root may stand, holding its lock file and nothing else.
	files, err := os.ReadDir(scratch)
	require.NoError(t, err)
	for _, file := range files {
		require.True(t, file.IsDir() && strings.HasPrefix(file.Name(), "dockhand-run-"), "left behind: %s", file.Name())
		inside, err := os.ReadDir(filepath.Join(scratch, file.Name()))
		require.NoError(t, err)
		for _, item := range inside {
			require.Equal(t, ".lock", item.Name(), "left in the run root: %s", item.Name())
		}
	}
}

// Ports are looked up several at once, and the result keeps the order
// they were asked for in, whichever finished first.
func TestObserveLooksUpPortsTogetherInOrder(t *testing.T) {
	executable := testsupport.MacPortsTclsh(t)
	root := t.TempDir()
	names := []string{"alpha", "beta", "gamma", "delta", "epsilon"}
	for i, name := range names {
		path := filepath.Join(root, "devel", name, "Portfile")
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
		require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf("PortSystem 1.0\nname %s\nversion 1.%d\ncategories devel\n", name, i)), 0600))
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "fixture"}} {
		command := exec.CommandContext(t.Context(), "git", args...)
		command.Dir = root
		output, err := command.CombinedOutput()
		require.NoError(t, err, "%s", output)
	}
	repo, err := git.Open(t.Context(), root, "")
	require.NoError(t, err)
	t.Setenv("TMPDIR", t.TempDir())
	ports := &eval.Evaluator{Executable: executable, Adapter: testsupport.BaseAdapter()}
	var progress [][2]int
	service := outdated.Service{Repo: repo, Ports: ports, Upstream: &upstream.Service{Ports: ports, Versions: ports}, Concurrency: 3,
		Progress: func(done, total int) { progress = append(progress, [2]int{done, total}) }}
	asked := []string{"gamma", "epsilon", "alpha", "delta", "beta"}
	result, err := service.Observe(t.Context(), outdated.Selection{Ports: asked})
	require.NoError(t, err)
	require.Equal(t, [][2]int{{0, 5}, {1, 5}, {2, 5}, {3, 5}, {4, 5}, {5, 5}}, progress, "heard before the first, and after each, in order")
	var selectors, versions []string
	for _, port := range result.Ports {
		selectors = append(selectors, port.Selector)
		versions = append(versions, port.CurrentVersion)
	}
	require.Equal(t, asked, selectors)
	require.Equal(t, []string{"1.2", "1.4", "1.0", "1.3", "1.1"}, versions)
}

// Selected by maintainer, through the port index, a stub is looked up
// through the subport carrying its release, as when it is named. The index
// names the stub, fixture, and the probe evaluates fixture-314; that is the
// redirection, not a port the index didn't name. outdated --mine once
// reported every such port, the person's Python ports, helm, and kubectl,
// as one it couldn't check.
func TestObserveByMaintainerFollowsAStub(t *testing.T) {
	executable := testsupport.MacPortsTclsh(t)
	root := t.TempDir()
	path := filepath.Join(root, "devel", "fixture", "Portfile")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	require.NoError(t, os.WriteFile(path, []byte(`PortSystem 1.0
name fixture
version 1.2.3
revision 0
categories devel
maintainers {example.org:ada @ada}
distname shared-${version}
master_sites https://example.invalid/${version}
checksums sha256 aaaa size 2
livecheck.type none
subport fixture-313 {}
subport fixture-314 {}
if {${subport} eq ${name}} {
 distfiles
 fetch {}
 use_configure no
 build {}
}
`), 0600))
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "fixture"}} {
		command := exec.CommandContext(t.Context(), "git", args...)
		command.Dir = root
		output, err := command.CombinedOutput()
		require.NoError(t, err, "%s", output)
	}
	repo, err := git.Open(t.Context(), root, "")
	require.NoError(t, err)
	t.Setenv("TMPDIR", t.TempDir())
	ports := &eval.Evaluator{Executable: executable, Adapter: testsupport.BaseAdapter()}
	index := &portindex.Stager{Repo: repo, Config: portindex.Config{Executable: testsupport.MacPortsTool(t, "portindex"), CacheDirectory: t.TempDir()}, NativePlatform: ports.NativePlatform}
	service := outdated.Service{Repo: repo, Ports: ports, Upstream: &upstream.Service{Ports: ports, Versions: ports}, Index: index}
	result, err := service.Observe(t.Context(), outdated.Selection{Maintainers: []string{"ada@example.org"}})
	require.NoError(t, err)
	found := map[string]outdated.Port{}
	for _, port := range result.Ports {
		found[port.Selector] = port
	}
	require.Contains(t, found, "fixture")
	stub := found["fixture"]
	require.NotContains(t, stub.Detail, "indexed subport", "the stub's redirection is not a disagreement")
	require.Equal(t, "1.2.3", stub.CurrentVersion, "it was evaluated, through fixture-314")
}

func TestObserveRejectsInvalidSelectionAndCanceledWorkBeforeDependencies(t *testing.T) {
	var service *outdated.Service
	_, err := service.Observe(t.Context(), outdated.Selection{})
	require.ErrorContains(t, err, "select ports")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = service.Observe(ctx, outdated.Selection{Ports: []string{"fixture"}})
	require.ErrorIs(t, err, context.Canceled)
	_, err = service.Observe(t.Context(), outdated.Selection{Ports: []string{"fixture"}})
	require.ErrorContains(t, err, "required")
}

func TestOutOfDateKeepsOnlyUpdatesAndCountsTheRest(t *testing.T) {
	t.Parallel()
	result := outdated.Result{Ports: []outdated.Port{
		{Selector: "a", Result: upstream.Result{Assessment: upstream.UpdateAvailable}},
		{Selector: "b", Result: upstream.Result{Assessment: upstream.Current}},
		{Selector: "c", Result: upstream.Result{Assessment: upstream.Unknown}},
		{Selector: "d", Result: upstream.Result{Assessment: upstream.Current}},
	}}
	report, hidden := result.OutOfDate()
	require.Len(t, report.Ports, 1)
	require.Equal(t, "a", report.Ports[0].Selector)
	require.Equal(t, outdated.Hidden{Current: 2, Unknown: 1}, hidden)
	require.Equal(t, "Not listed: 2 current, 1 could not be checked; --all lists them.", hidden.Note())
	require.Empty(t, outdated.Hidden{}.Note())
	require.Len(t, result.Ports, 4, "the full result is left as it was")
}
