package portindex

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
)

type indexFixture struct {
	t      *testing.T
	root   string
	repo   *git.Repository
	config Config
}

func newIndexFixture(t *testing.T) *indexFixture {
	t.Helper()
	executable, err := exec.LookPath("portindex")
	if err != nil {
		t.Skip("MacPorts portindex is required")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	f := &indexFixture{t: t, root: t.TempDir(), config: Config{Executable: executable, CacheDirectory: t.TempDir()}}
	f.run("init", "-q", "-b", "main")
	f.repo, err = git.Open(t.Context(), f.root, "")
	require.NoError(t, err)
	return f
}

func (f *indexFixture) run(args ...string) string {
	f.t.Helper()
	command := exec.CommandContext(f.t.Context(), "git", args...)
	command.Dir = f.root
	out, err := command.CombinedOutput()
	require.NoError(f.t, err, "%s", out)
	return strings.TrimSpace(string(out))
}

func (f *indexFixture) put(name, text string) {
	f.t.Helper()
	file := filepath.Join(f.root, filepath.FromSlash(name))
	require.NoError(f.t, os.MkdirAll(filepath.Dir(file), 0700))
	require.NoError(f.t, os.WriteFile(file, []byte(text), 0600))
}

func (f *indexFixture) remove(name string) {
	require.NoError(f.t, os.RemoveAll(filepath.Join(f.root, filepath.FromSlash(name))))
}

// commit records the working files and returns the commit and its tree.
func (f *indexFixture) commit() (commit, tree string) {
	f.t.Helper()
	f.run("add", "-A", ".")
	f.run("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "commit", "-q", "--allow-empty", "-m", "fixture")
	return f.run("rev-parse", "HEAD"), f.run("rev-parse", "HEAD^{tree}")
}

// stage materializes the source tree privately and stages its index there,
// returning the opened index and the progress messages.
func (f *indexFixture) stage(source record.Source) (*Index, []string, error) {
	f.t.Helper()
	snapshot, err := f.repo.Materialize(f.t.Context(), string(source.Tree))
	require.NoError(f.t, err)
	f.t.Cleanup(func() { snapshot.Close() })
	var mu sync.Mutex
	var messages []string
	ctx := progress.WithReporter(f.t.Context(), func(update progress.Update) {
		mu.Lock()
		defer mu.Unlock()
		messages = append(messages, update.Message)
	})
	if err := Stage(ctx, f.repo, source, testPlatform, f.config, snapshot.Root); err != nil {
		return nil, messages, err
	}
	index, err := Open(snapshot.Root)
	require.NoError(f.t, err)
	return index, messages, nil
}

func (f *indexFixture) environment() string {
	f.t.Helper()
	entries, err := os.ReadDir(f.config.CacheDirectory)
	require.NoError(f.t, err)
	require.Len(f.t, entries, 1)
	return filepath.Join(f.config.CacheDirectory, entries[0].Name())
}

func (f *indexFixture) latest() string {
	data, err := os.ReadFile(filepath.Join(f.environment(), latestFileName))
	require.NoError(f.t, err)
	return strings.TrimSpace(string(data))
}

func requireVersion(t *testing.T, index *Index, name, version string) {
	t.Helper()
	entry, err := index.Lookup(name)
	require.NoError(t, err)
	require.Equal(t, version, entry.Fields["version"])
}

const workingPortfile = "PortSystem 1.0\nname working\nversion 1\ncategories devel\n"

func TestStageSharesOneGenerationAcrossConsumers(t *testing.T) {
	f := newIndexFixture(t)
	f.put("devel/working/Portfile", workingPortfile)
	_, tree := f.commit()
	source := record.Source{Tree: record.ObjectID(tree)}
	index, messages, err := f.stage(source)
	require.NoError(t, err)
	requireVersion(t, index, "working", "1")
	require.Contains(t, strings.Join(messages, "\n"), "Generating full PortIndex for source "+tree[:12])
	require.Contains(t, strings.Join(messages, "\n"), "full pass")

	index, messages, err = f.stage(source)
	require.NoError(t, err)
	requireVersion(t, index, "working", "1")
	joined := strings.Join(messages, "\n")
	require.Contains(t, joined, "Using cached PortIndex for source "+tree[:12])
	require.NotContains(t, joined, "Generating")
	require.NotContains(t, joined, "Updating")
	meta, ok := readGeneration(filepath.Join(f.environment(), generationsDirectory, tree))
	require.True(t, ok)
	require.Equal(t, tree, meta.Tree)
	require.True(t, meta.Full)
	require.False(t, meta.Strict)
	require.Equal(t, tree, f.latest())
	data, err := os.ReadFile(filepath.Join(f.environment(), environmentFileName))
	require.NoError(t, err)
	require.Contains(t, string(data), f.config.Executable)
}

func TestCandidateDerivesFromBaseAndLaterMasterAdvancesIncrementally(t *testing.T) {
	f := newIndexFixture(t)
	f.put("devel/working/Portfile", workingPortfile)
	f.put("devel/other/Portfile", "PortSystem 1.0\nname other\nversion 1\ncategories devel\n")
	base, baseTree := f.commit()
	f.put("devel/working/Portfile", strings.Replace(workingPortfile, "version 1", "version 2", 1))
	candidate, candidateTree := f.commit()
	source := record.Source{Commit: record.ObjectID(candidate), Tree: record.ObjectID(candidateTree), Base: record.ObjectID(base)}

	index, messages, err := f.stage(source)
	require.NoError(t, err)
	requireVersion(t, index, "working", "2")
	joined := strings.Join(messages, "\n")
	require.Contains(t, joined, "Generating full PortIndex for source "+baseTree[:12])
	require.Contains(t, joined, "Updating PortIndex for source "+candidateTree[:12]+" from 1 changed paths")
	require.Equal(t, 1, strings.Count(joined, "Generating full"))
	baseMeta, ok := readGeneration(filepath.Join(f.environment(), generationsDirectory, baseTree))
	require.True(t, ok)
	require.False(t, baseMeta.Strict)
	candidateMeta, ok := readGeneration(filepath.Join(f.environment(), generationsDirectory, candidateTree))
	require.True(t, ok)
	require.True(t, candidateMeta.Strict)
	require.Equal(t, baseTree, candidateMeta.Seed)
	require.Equal(t, baseTree, f.latest(), "candidate generations must not displace the upstream seed")

	// A later consumer of the same candidate, such as verification staging
	// after discovery, reuses the completed generation.
	_, messages, err = f.stage(source)
	require.NoError(t, err)
	joined = strings.Join(messages, "\n")
	require.Contains(t, joined, "Using cached PortIndex for source "+candidateTree[:12])
	require.NotContains(t, joined, "Generating")
	require.NotContains(t, joined, "Updating")

	// Upstream moves on; the new master derives from the retained seed.
	f.run("checkout", "-q", base)
	f.put("devel/other/Portfile", "PortSystem 1.0\nname other\nversion 3\ncategories devel\n")
	_, masterTree := f.commit()
	index, messages, err = f.stage(record.Source{Tree: record.ObjectID(masterTree)})
	require.NoError(t, err)
	requireVersion(t, index, "other", "3")
	requireVersion(t, index, "working", "1")
	joined = strings.Join(messages, "\n")
	require.Contains(t, joined, "Updating PortIndex for source "+masterTree[:12]+" from 1 changed paths")
	require.NotContains(t, joined, "Generating full")
	require.Equal(t, masterTree, f.latest())
}

func TestStrictRequestsDoNotReusePartialGenerations(t *testing.T) {
	f := newIndexFixture(t)
	f.put("devel/working/Portfile", workingPortfile)
	f.put("devel/broken/Portfile", "PortSystem 1.0\nerror {existing failure}\n")
	base, _ := f.commit()
	f.put("devel/broken/Portfile", "PortSystem 1.0\nerror {changed failure}\n")
	commit, tree := f.commit()
	source := record.Source{Commit: record.ObjectID(commit), Tree: record.ObjectID(tree)}
	index, _, err := f.stage(source)
	require.NoError(t, err)
	_, err = index.Lookup("working")
	require.NoError(t, err)
	_, err = index.Lookup("broken")
	require.ErrorIs(t, err, ErrNotIndexed)

	source.Base = record.ObjectID(base)
	_, _, err = f.stage(source)
	require.Error(t, err, "a partial index must not hide a changed-port failure from a contribution")
	meta, ok := readGeneration(filepath.Join(f.environment(), generationsDirectory, tree))
	require.True(t, ok, "the partial generation survives the failed strict attempt")
	require.False(t, meta.Strict)
}

func TestStageReusesTreeDiffsAndInvalidatesSharedResources(t *testing.T) {
	f := newIndexFixture(t)
	f.put("devel/working/Portfile", workingPortfile+"subport child {}\n")
	f.put("devel/removed/Portfile", "PortSystem 1.0\nname removed\nversion 1\ncategories devel\n")
	f.put("devel/broken/Portfile", "PortSystem 1.0\nerror broken\n")
	stage := func(wantFull bool, check func(*Index)) {
		t.Helper()
		_, tree := f.commit()
		index, messages, err := f.stage(record.Source{Tree: record.ObjectID(tree)})
		require.NoError(t, err)
		joined := strings.Join(messages, "\n")
		if wantFull {
			require.Contains(t, joined, "Generating full PortIndex")
		} else {
			require.Contains(t, joined, "Updating PortIndex for source")
			require.NotContains(t, joined, "Generating full PortIndex")
		}
		check(index)
	}
	stage(true, func(index *Index) { _, err := index.Lookup("child"); require.NoError(t, err) })
	f.put("devel/working/Portfile", strings.Replace(workingPortfile, "version 1", "version 2", 1))
	f.remove("devel/removed")
	stage(false, func(index *Index) {
		requireVersion(t, index, "working", "2")
		for _, name := range []string{"child", "removed", "broken"} {
			_, err := index.Lookup(name)
			require.ErrorIs(t, err, ErrNotIndexed)
		}
	})
	f.put("_resources/port1.0/group/fixture-1.0.tcl", "set fixture 1\n")
	stage(true, func(index *Index) { requireVersion(t, index, "working", "2") })
}

func TestConcurrentStagersShareOneBuild(t *testing.T) {
	f := newIndexFixture(t)
	f.put("devel/working/Portfile", workingPortfile)
	_, tree := f.commit()
	source := record.Source{Tree: record.ObjectID(tree)}
	var mu sync.Mutex
	var messages []string
	ctx := progress.WithReporter(context.Background(), func(update progress.Update) {
		mu.Lock()
		defer mu.Unlock()
		messages = append(messages, update.Message)
	})
	var group sync.WaitGroup
	errs := make([]error, 3)
	for i := range errs {
		snapshot, err := f.repo.Materialize(t.Context(), tree)
		require.NoError(t, err)
		t.Cleanup(func() { snapshot.Close() })
		group.Add(1)
		go func(i int, root string) {
			defer group.Done()
			errs[i] = Stage(ctx, f.repo, source, testPlatform, f.config, root)
		}(i, snapshot.Root)
	}
	group.Wait()
	for _, err := range errs {
		require.NoError(t, err)
	}
	joined := strings.Join(messages, "\n")
	require.Equal(t, 1, strings.Count(joined, "Generating full PortIndex"), joined)
}
