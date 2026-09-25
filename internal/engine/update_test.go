package engine

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portedit"
	"github.com/herbygillot/dockhand/internal/macports/portedit/archives"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/preparation"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/store"
)

var (
	versionLine  = regexp.MustCompile(`(?m)^version (\S+)$`)
	revisionLine = regexp.MustCompile(`(?m)^revision (\S+)$`)
)

// fakePreparer edits a Portfile's version line, and adds a checksums line
// on a refresh, in the tree it is given, the way the real one returns its
// edits: file edits with preconditions, and the edited tree.
type fakePreparer struct {
	t        *testing.T
	repo     *git.Repository
	version  string
	requests []preparation.Request
	// during runs while the edit is prepared.
	during func()
	// upstream are the old and new versions' archive contents, kept as
	// tarballs when an update asks to compare them.
	upstream [2]map[string]string
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
	revision, nextRevision := 0, 0
	if m := revisionLine.FindSubmatch(data); m != nil {
		revision, _ = strconv.Atoi(string(m[1]))
	}
	nextRevision = revision
	switch {
	case r.Action == record.Bump:
		next = r.Release.Version
		after = versionLine.ReplaceAllString(after, "version "+next)
	case r.Action == record.BumpRevision:
		nextRevision = revision + 1
		if revisionLine.MatchString(after) {
			after = revisionLine.ReplaceAllString(after, "revision "+strconv.Itoa(nextRevision))
		} else {
			after += "revision 1\n"
		}
	case !regexp.MustCompile(`(?m)^checksums `).MatchString(after):
		after += "checksums sha256 0000\n"
	}
	if p.during != nil {
		p.during()
	}
	snapshot := func(version string, revision int) macports.Snapshot {
		return macports.Snapshot{Ports: map[string]macports.PortInfo{r.Selection.Selector: {Name: r.Selection.Selector, Version: version, Revision: revision}}}
	}
	result := preparation.Result{Target: record.Target{Name: r.Selection.Selector, Portfile: name}, Release: r.Release, PreparedTree: r.Source.Tree,
		Fidelity: []portedit.Fidelity{{Before: snapshot(string(old), revision), After: snapshot(next, nextRevision)}}}
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
	if r.Action == record.BumpRevision {
		result.Commits[0].Subject = r.Selection.Selector + ": " + r.Subject
	}
	if r.KeepArchives != "" && p.upstream[0] != nil {
		result.Previous = []archives.Download{{Path: writeTarball(p.t, r.KeepArchives, "old", p.upstream[0])}}
		result.Downloads = []archives.Download{{Path: writeTarball(p.t, r.KeepArchives, "new", p.upstream[1])}}
	}
	return result, nil
}

// writeTarball writes files under one top directory, as a release archive
// has them.
func writeTarball(t *testing.T, directory, top string, files map[string]string) string {
	name := filepath.Join(directory, top+".tar.gz")
	out, err := os.Create(name)
	require.NoError(t, err)
	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)
	for path, content := range files {
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: top + "/" + path, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg}))
		_, err := tw.Write([]byte(content))
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	require.NoError(t, out.Close())
	return name
}

func (f fixture) withPreparer(t *testing.T) (*Engine, *fakePreparer) {
	e := f.open(t)
	p := &fakePreparer{t: t, repo: e.Repo, version: "1.8.1"}
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
		edits, err := r.Edits(branch.ID)
		require.NoError(t, err)
		require.Len(t, edits, 1)
		require.Equal(t, model.EditUpdate, edits[0].Kind)
		require.Equal(t, "textproc/jq", edits[0].Directory)
		require.Equal(t, "jq: update to 1.8.1", edits[0].Subject)
		require.Equal(t, run(t, branch.Worktree, "rev-parse", "HEAD:textproc/jq/Portfile"), string(edits[0].Files[0].Before))
		require.Equal(t, run(t, branch.Worktree, "hash-object", "textproc/jq/Portfile"), string(edits[0].Files[0].After))
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
	_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: record.Action("publish"), Port: "jq"})
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

func TestAnUpdateComparesTheUpstreamArchives(t *testing.T) {
	f := setup(t)
	e, p := f.withPreparer(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-update"})
	require.NoError(t, err)
	p.upstream = [2]map[string]string{
		{"COPYING": "MIT\n", "go.mod": "module jq\n"},
		{"COPYING": "GPL\n", "go.mod": "module jq\n\nrequire golang.org/x/net v0.44.0\n"},
	}
	update, err := e.Update(t.Context(), UpdateRequest{Branch: branch, Action: record.Bump, Port: "jq", CompareUpstream: true})
	require.NoError(t, err)
	require.True(t, update.Upstream.Held())
	require.Equal(t, []string{
		"upstream's COPYING changed; the Portfile's license line may need to follow",
		"upstream: go.mod adds golang.org/x/net v0.44.0",
	}, []string{update.Upstream.Changes[0].Message, update.Upstream.Changes[1].Message})
	require.NoDirExists(t, p.requests[0].KeepArchives, "the archives go when the update is done")

	var edits []model.Edit
	require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
		edits, err = r.Edits(branch.ID)
		return err
	}))
	require.Equal(t, update.Upstream, edits[0].Upstream, "the edit keeps what was found")

	p.upstream = [2]map[string]string{}
	other, err := e.Start(t.Context(), StartRequest{Name: "jq-two"})
	require.NoError(t, err)
	quiet, err := e.Update(t.Context(), UpdateRequest{Branch: other, Action: record.Bump, Port: "jq", CompareUpstream: true})
	require.NoError(t, err)
	require.Nil(t, quiet.Upstream, "no archives, nothing compared")
	require.False(t, quiet.Upstream.Held())
}
