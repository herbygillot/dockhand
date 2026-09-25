package engine

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portedit"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/preparation"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/store"
)

var versionLine = regexp.MustCompile(`(?m)^version (\S+)$`)

// fakePreparer edits a Portfile's version line, and adds a checksums line
// on a refresh, in the tree it is given, the way the real one returns its
// edits: file edits with preconditions, and the edited tree.
type fakePreparer struct {
	repo     *git.Repository
	version  string
	requests []preparation.Request
	// during runs while the edit is prepared.
	during func()
}

func (p *fakePreparer) ResolveRelease(_ context.Context, r preparation.Request) (record.Release, error) {
	version := p.version
	if r.Version != "" {
		version = r.Version
	}
	return record.Release{Version: version, Forge: "github", Tag: "jq-" + version}, nil
}

func (p *fakePreparer) Prepare(ctx context.Context, r preparation.Request) (preparation.Result, error) {
	p.requests = append(p.requests, r)
	name := "textproc/" + r.Selection.Selector + "/Portfile"
	before, data, err := p.repo.File(ctx, string(r.Source.Tree), name)
	if err != nil {
		return preparation.Result{}, err
	}
	old := versionLine.FindSubmatch(data)[1]
	next, after := string(old), string(data)
	if r.Action == record.Bump {
		next = r.Release.Version
		after = versionLine.ReplaceAllString(after, "version "+next)
	} else if !regexp.MustCompile(`(?m)^checksums `).MatchString(after) {
		after += "checksums sha256 0000\n"
	}
	if p.during != nil {
		p.during()
	}
	snapshot := func(version string) macports.Snapshot {
		return macports.Snapshot{Ports: map[string]macports.PortInfo{r.Selection.Selector: {Name: r.Selection.Selector, Version: version}}}
	}
	result := preparation.Result{Target: record.Target{Name: r.Selection.Selector, Portfile: name}, Release: r.Release, PreparedTree: r.Source.Tree,
		Fidelity: []portedit.Fidelity{{Before: snapshot(string(old)), After: snapshot(next)}}}
	if after == string(data) {
		return result, nil
	}
	edit := git.FileEdit{Path: name, Before: before, After: []byte(after), Mode: before.Mode}
	tree, err := p.repo.EditTree(ctx, string(r.Source.Tree), []git.FileEdit{edit})
	if err != nil {
		return preparation.Result{}, err
	}
	result.Files, result.PreparedTree = []git.FileEdit{edit}, record.ObjectID(tree)
	result.Commits = []preparation.CommitIntent{{Subject: r.Selection.Selector + ": update to " + next}}
	return result, nil
}

func (f fixture) withPreparer(t *testing.T) (*Engine, *fakePreparer) {
	e := f.open(t)
	p := &fakePreparer{repo: e.Repo, version: "1.8.1"}
	e.Preparer = p
	return e, p
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

func TestUpdateEditsWorkingFilesAndCommitsNothing(t *testing.T) {
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-update"})
	require.NoError(t, err)
	portfile := filepath.Join(branch.Worktree, "textproc/jq/Portfile")
	require.NoFileExists(t, portfile, "the sparse worktree starts with _resources only")

	update, err := e.Update(t.Context(), UpdateRequest{Branch: branch, Action: record.Bump, Port: "jq"})
	require.NoError(t, err)
	require.True(t, update.Applied)
	require.Equal(t, "jq", update.Port)
	require.Equal(t, "1.7.1", update.Before.String())
	require.Equal(t, "1.8.1", update.After.String())
	require.Equal(t, "jq-1.8.1", update.Release.Tag)
	require.Equal(t, []string{"textproc/jq/Portfile"}, update.Files)
	require.Equal(t, "jq: update to 1.8.1", update.Subject)
	require.Contains(t, update.Diff, "+version 1.8.1")

	require.Equal(t, "name jq\nversion 1.8.1\n", read(t, portfile), "the cone grew to hold the edited port")
	require.Equal(t, string(branch.Base), run(t, branch.Worktree, "rev-parse", "HEAD"), "nothing is committed")
	require.Equal(t, "M textproc/jq/Portfile", run(t, branch.Worktree, "status", "--porcelain"))

	require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
		events, err := r.Events(0, 100)
		require.NoError(t, err)
		last := events[len(events)-1]
		require.Equal(t, "branch.edit", last.Kind)
		require.Equal(t, branch.ID, last.Branch)
		require.Equal(t, "jq: 1.7.1 → 1.8.1 (textproc/jq/Portfile)", last.Message)
		return nil
	}))
}

func TestUpdateStartsFromTheWorkingFilesAsTheyAre(t *testing.T) {
	f := setup(t)
	e, p := f.withPreparer(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-update", Here: true})
	require.NoError(t, err)
	portfile := filepath.Join(branch.Worktree, "textproc/jq/Portfile")
	write(t, branch.Worktree, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.7.1\n# my note\n"})

	_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: record.Bump, Port: "jq", Version: "1.8.0"})
	require.NoError(t, err)
	require.Equal(t, "name jq\nversion 1.8.0\n# my note\n", read(t, portfile), "an uncommitted edit is kept")
	require.Equal(t, record.ObjectID(branch.Base), p.requests[0].Source.Base)
	require.Equal(t, "1.8.0", p.requests[0].Version)

	checksums, err := e.Update(t.Context(), UpdateRequest{Branch: branch, Action: record.RefreshChecksums, Port: "jq"})
	require.NoError(t, err)
	require.True(t, checksums.Applied)
	require.Equal(t, "name jq\nversion 1.8.0\n# my note\nchecksums sha256 0000\n", read(t, portfile))

	again, err := e.Update(t.Context(), UpdateRequest{Branch: branch, Action: record.RefreshChecksums, Port: "jq"})
	require.NoError(t, err)
	require.True(t, again.Current, "nothing left to change")
	require.False(t, again.Applied)
}

func TestAPlannedUpdateChangesNothing(t *testing.T) {
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-update"})
	require.NoError(t, err)
	update, err := e.Update(t.Context(), UpdateRequest{Branch: branch, Action: record.Bump, Port: "jq", Plan: true})
	require.NoError(t, err)
	require.False(t, update.Applied)
	require.Contains(t, update.Diff, "-version 1.7.1\n+version 1.8.1")
	require.NoFileExists(t, filepath.Join(branch.Worktree, "textproc/jq/Portfile"))
	require.Empty(t, run(t, branch.Worktree, "status", "--porcelain"))
}

func TestAnUpdateWritesNothingOverAFileThatChanged(t *testing.T) {
	f := setup(t)
	e, p := f.withPreparer(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-update", Here: true})
	require.NoError(t, err)
	portfile := filepath.Join(branch.Worktree, "textproc/jq/Portfile")
	p.during = func() {
		write(t, branch.Worktree, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.7.2\n"})
	}

	_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: record.Bump, Port: "jq"})
	require.ErrorIs(t, err, git.ErrWorkingFile)
	require.ErrorContains(t, err, "nothing was written")
	require.Equal(t, "name jq\nversion 1.7.2\n", read(t, portfile), "the person's edit stands")
}

func TestUpdateNeedsTheBranchCheckedOut(t *testing.T) {
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-update"})
	require.NoError(t, err)
	run(t, branch.Worktree, "switch", "-q", "--detach")
	_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: record.Bump, Port: "jq"})
	require.ErrorContains(t, err, "is not checked out in")

	_, err = e.Update(t.Context(), UpdateRequest{Branch: model.Branch{Name: "dockhand/elsewhere"}, Action: record.Bump, Port: "jq"})
	require.ErrorContains(t, err, "not checked out anywhere")
	_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: record.BumpRevision, Port: "jq"})
	require.ErrorContains(t, err, "not an update")
}

func TestBranchesChangingAndFreeNames(t *testing.T) {
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-update"})
	require.NoError(t, err)
	changing, err := e.BranchesChanging(t.Context(), "jq")
	require.NoError(t, err)
	require.Empty(t, changing, "uncommitted work changes nothing yet")

	_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: record.Bump, Port: "jq"})
	require.NoError(t, err)
	run(t, branch.Worktree, "commit", "-q", "-am", "jq: update to 1.8.1")
	changing, err = e.BranchesChanging(t.Context(), "jq")
	require.NoError(t, err)
	require.Len(t, changing, 1)
	require.Equal(t, branch.ID, changing[0].ID)
	none, err := e.BranchesChanging(t.Context(), "libharbor")
	require.NoError(t, err)
	require.Empty(t, none)

	name, err := e.FreeName(t.Context(), "jq")
	require.NoError(t, err)
	require.Regexp(t, `^jq-[a-z0-9]{4}$`, name)
}
