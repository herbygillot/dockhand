package engine

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portedit"
	"github.com/herbygillot/dockhand/internal/macports/portedit/archives"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/preparation"
	"github.com/herbygillot/dockhand/internal/sourcecompare"
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
	// toolchain is a Go requirement an update's go.mod leaves above the
	// Portfile's minimum.
	toolchain *preparation.GoToolchain
	// dependencies are the ports the updated port depends on.
	dependencies []string
	// options are the evaluated port's options after the edit.
	options map[string]string
}

func (p *fakePreparer) ResolveRelease(_ context.Context, r preparation.Request) (model.Release, error) {
	if r.Release != nil {
		return *r.Release, nil
	}
	version := p.version
	if r.Version != "" {
		version = r.Version
	}
	return model.Release{Version: version, Forge: "github", Tag: "jq-" + version}, nil
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
	case r.Action == model.EditUpdate:
		next = r.Release.Version
		after = versionLine.ReplaceAllString(after, "version "+next)
	case r.Action == model.EditRevbump:
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
		return macports.Snapshot{Ports: map[string]macports.PortInfo{r.Selection.Selector: {Name: r.Selection.Selector, Version: version, Revision: revision, Options: p.options}}}
	}
	result := preparation.Result{Target: model.Target{Name: r.Selection.Selector, Portfile: name}, Release: r.Release, PreparedTree: r.Source.Tree,
		Fidelity: []portedit.Fidelity{{Before: snapshot(string(old), revision), After: snapshot(next, nextRevision)}}}
	if after == string(data) {
		return result, nil
	}
	edit := git.FileEdit{Path: name, Before: before, After: []byte(after), Mode: before.Mode}
	tree, err := p.repo.EditTree(ctx, string(r.Source.Tree), []git.FileEdit{edit})
	if err != nil {
		return preparation.Result{}, err
	}
	result.Files, result.PreparedTree = []git.FileEdit{edit}, model.ObjectID(tree)
	result.GoToolchain = p.toolchain
	if len(p.dependencies) > 0 {
		result.Prepared = snapshot(next, nextRevision)
		result.Prepared.Ports[r.Selection.Selector] = port(r.Selection.Selector, p.dependencies...)
	}
	result.Commits = []preparation.CommitIntent{{Subject: r.Selection.Selector + ": update to " + next}}
	if r.Action == model.EditRevbump {
		result.Commits[0].Subject = r.Selection.Selector + ": " + r.Subject
	}
	// The new archive replaces the old one where both are given; given
	// alone, it replaces none dockhand found.
	if r.KeepArchives != "" && p.upstream[1] != nil {
		next := archives.Download{Path: writeTarball(p.t, r.KeepArchives, "new", p.upstream[1])}
		next.Name = "new.tar.gz"
		result.Downloads = []archives.Download{next}
		if p.upstream[0] != nil {
			result.Pairs = []preparation.ArchivePair{{Previous: archives.Download{Path: writeTarball(p.t, r.KeepArchives, "old", p.upstream[0])}, Next: next}}
		}
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

	_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditCreate, Port: "jq"})
	require.EqualError(t, err, "engine: create is not an update", "an edit's kinds are an update's, but for create")

	update, err := e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditUpdate, Port: "jq"})
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

	_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditUpdate, Port: "jq", Version: "1.8.0"})
	require.NoError(t, err)
	require.Equal(t, "name jq\nversion 1.8.0\n# my note\n", read(t, portfile), "an uncommitted edit is kept")
	require.Equal(t, model.ObjectID(branch.Base), p.requests[0].Source.Base)
	require.Equal(t, "1.8.0", p.requests[0].Version)

	checksums, err := e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditChecksums, Port: "jq"})
	require.NoError(t, err)
	require.True(t, checksums.Applied)
	require.Equal(t, "name jq\nversion 1.8.0\n# my note\nchecksums sha256 0000\n", read(t, portfile))

	again, err := e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditChecksums, Port: "jq"})
	require.NoError(t, err)
	require.True(t, again.Current, "nothing left to change")
	require.False(t, again.Applied)
}

func TestAPlannedUpdateChangesNothing(t *testing.T) {
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-update"})
	require.NoError(t, err)
	update, err := e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditUpdate, Port: "jq", Plan: true})
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

	_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditUpdate, Port: "jq"})
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
	_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditUpdate, Port: "jq"})
	require.ErrorContains(t, err, "is not checked out in")

	_, err = e.Update(t.Context(), UpdateRequest{Branch: model.Branch{Name: "dockhand/elsewhere"}, Action: model.EditUpdate, Port: "jq"})
	require.ErrorContains(t, err, "not checked out anywhere")
	_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditKind("publish"), Port: "jq"})
	require.ErrorContains(t, err, "not an update")
	// A release found already is the version asked for, for a version
	// update, or it is refused rather than taken.
	found := &model.Release{Version: "1.8.1", Forge: "github", Tag: "jq-1.8.1"}
	_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditUpdate, Port: "jq", Version: "1.8.0", Release: found})
	require.ErrorContains(t, err, `the release found is 1.8.1's, for a version update to "1.8.0"`)
	_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditChecksums, Port: "jq", Version: "1.8.1", Release: found})
	require.ErrorContains(t, err, "the release found is 1.8.1's")
}

func TestBranchesChangingAndFreeNames(t *testing.T) {
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-update"})
	require.NoError(t, err)
	changing, err := e.BranchesChanging(t.Context(), "jq")
	require.NoError(t, err)
	require.Empty(t, changing, "uncommitted work changes nothing yet")

	_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditUpdate, Port: "jq"})
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
	update, err := e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditUpdate, Port: "jq", CompareUpstream: true})
	require.NoError(t, err)
	require.True(t, update.Upstream.Held())
	require.Equal(t, []string{
		"upstream's COPYING changed; the Portfile's license line may need to follow",
		"upstream: go.mod: 1 added",
	}, []string{update.Upstream.Changes[0].Message, update.Upstream.Changes[1].Message})
	require.False(t, update.Upstream.Changes[1].Hold, "a Go module holds nothing (D9)")
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
	quiet, err := e.Update(t.Context(), UpdateRequest{Branch: other, Action: model.EditUpdate, Port: "jq", CompareUpstream: true})
	require.NoError(t, err)
	require.Nil(t, quiet.Upstream, "no archives, nothing compared")
	require.False(t, quiet.Upstream.Held())
}

// refusing can't make the edit by itself, as for a port whose pre-fetch
// hook runs a command.
type refusing struct{ *fakePreparer }

func (refusing) Prepare(context.Context, preparation.Request) (preparation.Result, error) {
	return preparation.Result{}, fmt.Errorf("%w: its pre-fetch hook runs exec", ErrUnsupported)
}

// A version update asked to start its branch prepares it on master and
// starts the branch from that master only once there is an edit to make,
// or one for the person to make by hand.
func TestAnUpdateStartsItsBranchOnlyForAnEdit(t *testing.T) {
	f := setup(t)
	e, p := f.withPreparer(t)
	master := f.upstreamMaster(t)
	start := func(name string) UpdateRequest {
		return UpdateRequest{Start: &StartRequest{Name: name}, Action: model.EditUpdate, Port: "jq"}
	}

	p.version = "1.7.1"
	update, err := e.Update(t.Context(), start("jq-current"))
	require.NoError(t, err)
	require.True(t, update.Current)
	require.False(t, update.Started)
	require.Equal(t, PortVersion{Version: "1.7.1"}, update.After)
	_, err = e.Resolve(t.Context(), "jq-current")
	require.Error(t, err, "a port already current starts nothing")
	require.NoDirExists(t, filepath.Join(e.Worktrees(), "jq-current"))

	p.version = "1.8.1"
	update, err = e.Update(t.Context(), start("jq-new"))
	require.NoError(t, err)
	require.True(t, update.Started)
	require.True(t, update.Applied)
	require.Equal(t, master, update.Branch.Base, "the master it was prepared on")
	require.Contains(t, read(t, filepath.Join(update.Branch.Worktree, "textproc/jq/Portfile")), "version 1.8.1")

	changing, err := e.BranchesChanging(t.Context(), "jq")
	require.NoError(t, err)
	require.Len(t, changing, 1, "an edit not yet committed changes the port")
	require.Equal(t, update.Branch.ID, changing[0].ID)
	e.OutdatedReader = &newReleases{}
	report, err := e.Outdated(t.Context(), OutdatedRequest{Maintainers: []string{"@ada"}})
	require.NoError(t, err)
	plan, err := e.PlanOutdated(t.Context(), report)
	require.NoError(t, err)
	require.Empty(t, plan.Updates, "serve starts no second branch for it")
	require.Contains(t, plan.Skipped, SkippedUpdate{Port: "jq", Reason: "already in jq-new"})

	e.Preparer = refusing{p}
	update, err = e.Update(t.Context(), start("jq-by-hand"))
	require.ErrorIs(t, err, ErrUnsupported)
	require.True(t, update.Started, "the branch to make the edit in by hand")
	require.DirExists(t, update.Branch.Worktree)
}

// An update that edited nothing, the port already at the release, says what
// the port is at from the port as it stands, since no fidelity report says
// it then; "jq is already at ; nothing to change" read nothing.
func TestAnUpdateThatEditedNothingSaysWhatThePortIsAt(t *testing.T) {
	update := describe(model.Branch{}, "jq", preparation.Result{Result: portedit.Result{Unchanged: &macports.PortInfo{Name: "jq", Version: "1.8.2", Revision: 1}}})
	require.Equal(t, PortVersion{Version: "1.8.2", Revision: 1}, update.Before)
	require.Equal(t, PortVersion{Version: "1.8.2", Revision: 1}, update.After)
	require.Equal(t, "1.8.2_1", update.After.String())
}

// Each archive an update replaced is compared with its own replacement,
// and a change they share, such as the license both carry, is said once.
func TestAChangeTheArchivesShareIsSaidOnce(t *testing.T) {
	dir := t.TempDir()
	pair := func(name string, before, after map[string]string) preparation.ArchivePair {
		next := archives.Download{Path: writeTarball(t, dir, name+"-2", after)}
		next.Name = name + "-2.tar.gz"
		return preparation.ArchivePair{Previous: archives.Download{Path: writeTarball(t, dir, name+"-1", before)}, Next: next}
	}
	source := pair("source", map[string]string{"LICENSE": "MIT\n"}, map[string]string{"LICENSE": "Apache-2.0\n", "meson.build": "project('x')\n"})
	binary := pair("binary", map[string]string{"LICENSE": "MIT\n"}, map[string]string{"LICENSE": "Apache-2.0\n"})
	result := preparation.Result{}
	result.Downloads = []archives.Download{source.Next, binary.Next}
	result.Pairs = []preparation.ArchivePair{source, binary}
	comparison, _ := compareUpstream(t.Context(), result, sourcecompare.Versions{})
	require.Empty(t, comparison.Problem)
	require.Equal(t, []model.UpstreamChange{
		{Kind: "license", Path: "LICENSE", Message: "upstream's LICENSE changed; the Portfile's license line may need to follow", Hold: true},
		{Kind: "build", Path: "meson.build", Message: "upstream's meson.build is new; the build may need the Portfile to follow", Hold: true},
	}, comparison.Changes)
}

// A Python requirement the new version moves holds where the port that
// provides it doesn't meet it at the version the branch has: sqlit-tui
// 1.6.4 pins textual-fastdatatable==0.19.0, and MacPorts had 0.17.1, while
// a noarch build passes regardless (the sshuttle run). A branch that
// updates the dependency first meets it (the libuv run).
func TestAPythonPinMacPortsCantMeetHolds(t *testing.T) {
	for _, test := range []struct {
		name, has string
		want      []model.UpstreamChange
	}{
		{"unmet", "0.17.1", []model.UpstreamChange{{Kind: "dependency", Path: "pyproject.toml", Hold: true,
			Message: "upstream: pyproject.toml requires textual-fastdatatable ==0.19.0, which MacPorts' py313-textual-fastdatatable 0.17.1 doesn't meet"}}},
		{"met by the branch", "0.19.0", nil},
		{"unreadable", "not-a-version", []model.UpstreamChange{{Kind: "dependency", Path: "pyproject.toml",
			Message: "upstream: couldn't tell whether MacPorts' py313-textual-fastdatatable meets pyproject.toml's textual-fastdatatable ==0.19.0: \"not-a-version\" isn't a PEP 440 version"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := setup(t)
			e, p := f.withPreparer(t)
			branch, err := e.Start(t.Context(), StartRequest{Name: "sqlit-tui"})
			require.NoError(t, err)
			p.dependencies = []string{"py313-textual-fastdatatable", "py313-textual"}
			e.PortReader = fakePorts{directories: map[string][]macports.PortInfo{"python/py-textual-fastdatatable": {{Name: "py313-textual-fastdatatable", Version: test.has}}}}
			p.upstream = [2]map[string]string{
				{"pyproject.toml": "[project]\ndependencies = [\"textual-fastdatatable==0.17.1\", \"textual>=0.80\", \"PyYAML>=6\"]\n"},
				{"pyproject.toml": "[project]\ndependencies = [\"Textual_FastDataTable==0.19.0\", \"textual>=0.80\", \"PyYAML>=6.0.2\"]\n"},
			}
			update, err := e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditUpdate, Port: "jq", CompareUpstream: true})
			require.NoError(t, err)
			var pins []model.UpstreamChange
			for _, change := range update.Upstream.Changes {
				if !strings.Contains(change.Message, " moves ") {
					pins = append(pins, change)
				}
			}
			require.Equal(t, test.want, pins, "PyYAML, which no dependency's name matches, is left alone")
		})
	}
}

// A file of a build system the port doesn't use holds nothing, and says
// why: flatbuffers, built with CMake, held on package.json and
// Package.swift (the flatbuffers run's finding 2). A build file of the one
// it uses still holds, and one that changed only the version it names
// doesn't (nuspell's CMakeLists.txt).
func TestAChangeTheBuildDoesntReadHoldsNothing(t *testing.T) {
	dir := t.TempDir()
	next := archives.Download{Path: writeTarball(t, dir, "flatbuffers-25.12.19", map[string]string{
		"CMakeLists.txt": "project(FlatBuffers VERSION 25.12.19)\nadd_library(flatbuffers src/a.cpp src/b.cpp)\n",
		"package.json":   `{"devDependencies": {"eslint": "9.0.0", "typescript": "5.8.3"}}`,
		"Package.swift":  "// swift-tools-version:5.9\n",
	})}
	next.Name = "flatbuffers-25.12.19.tar.gz"
	previous := archives.Download{Path: writeTarball(t, dir, "flatbuffers-25.9.23", map[string]string{
		"CMakeLists.txt": "project(FlatBuffers VERSION 25.9.23)\nadd_library(flatbuffers src/a.cpp)\n",
		"package.json":   `{"devDependencies": {"eslint": "8.0.0"}}`,
	})}
	result := preparation.Result{}
	result.Target = model.Target{Name: "flatbuffers"}
	result.Prepared = macports.Snapshot{Ports: map[string]macports.PortInfo{"flatbuffers": {Name: "flatbuffers", Options: map[string]string{"dockhand.portgroups": "github cmake", "use_configure": "yes", "configure.cmd": "/opt/local/bin/cmake"}}}}
	// A second archive with the same package.json counts nothing twice.
	other := next
	other.Name = "flatbuffers-25.12.19.zip"
	result.Downloads = []archives.Download{next, other}
	result.Pairs = []preparation.ArchivePair{{Previous: previous, Next: next}, {Previous: previous, Next: other}}
	comparison, _ := compareUpstream(t.Context(), result, sourcecompare.Versions{Old: "25.9.23", New: "25.12.19"})
	require.Equal(t, []model.UpstreamChange{
		{Kind: "build", Path: "CMakeLists.txt", Message: "upstream's CMakeLists.txt changed; the build may need the Portfile to follow", Hold: true},
		{Kind: "build", Path: "Package.swift", Message: "upstream's Package.swift is new; the build may need the Portfile to follow; flatbuffers builds with cmake, not swift, so it holds nothing"},
		{Kind: "dependency", Path: "package.json", Message: "upstream: package.json: 2 dependencies changed; flatbuffers builds with cmake, not node, so it holds nothing"},
	}, comparison.Changes)
}

// A version update or a checksum refresh says the port's URLs over plain
// HTTP, its homepage and its master_sites, with whether each answers over
// HTTPS, which MacPorts prefers; a mirror group is MacPorts' own, and a
// revision bump doesn't look.
func TestAnUpdateSaysThePortsPlainHTTPURLs(t *testing.T) {
	f := setup(t)
	e, p := f.withPreparer(t)
	p.options = map[string]string{"homepage": "http://jqlang.example/", "master_sites": "http://dl.example/jq/:src gnu https://github.com/jqlang/jq/releases/"}
	e.HTTPS = httpsAnswers{"https://jqlang.example/": true}
	want := []PlainURL{
		{PlainURL: macports.PlainURL{Option: "homepage", URL: "http://jqlang.example/"}, HTTPS: "https://jqlang.example/", Answers: true},
		{PlainURL: macports.PlainURL{Option: "master_sites", URL: "http://dl.example/jq/"}, HTTPS: "https://dl.example/jq/"},
	}
	update, err := e.Update(t.Context(), UpdateRequest{Start: &StartRequest{Name: "jq-update"}, Action: model.EditUpdate, Port: "jq"})
	require.NoError(t, err)
	require.Equal(t, want, update.PlainHTTP)
	checksums, err := e.Update(t.Context(), UpdateRequest{Branch: update.Branch, Action: model.EditChecksums, Port: "jq"})
	require.NoError(t, err)
	require.Equal(t, want, checksums.PlainHTTP)
	revbump, err := e.Update(t.Context(), UpdateRequest{Branch: update.Branch, Action: model.EditRevbump, Port: "jq", Subject: "rebuild for oniguruma 6.9.10"})
	require.NoError(t, err)
	require.Empty(t, revbump.PlainHTTP)
}

// A Python pin that applies only elsewhere asks nothing of MacPorts' port:
// a Windows-only requests==999 doesn't hold an update MacPorts'
// py313-requests 1 builds, while the same pin for macOS does (the
// helper-ownership review's finding 1, its probe as a regression test).
func TestAPinForAnotherPlatformHoldsNothing(t *testing.T) {
	for marker, holds := range map[string]bool{"sys_platform == 'win32'": false, "sys_platform == 'darwin'": true, "python_version >= '3.12'": true, "python_version < '3.10'": false} {
		dir := t.TempDir()
		old := archives.Download{Name: "old.tar.gz", Path: writeTarball(t, dir, "pkg-1", map[string]string{"requirements.txt": "requests==1; " + marker + "\n"})}
		next := archives.Download{Name: "new.tar.gz", Path: writeTarball(t, dir, "pkg-2", map[string]string{"requirements.txt": "requests==999; " + marker + "\n"})}
		result := preparation.Result{}
		result.Target = model.Target{Name: "demo"}
		result.Downloads = []archives.Download{next}
		result.Pairs = []preparation.ArchivePair{{Previous: old, Next: next}}
		result.Prepared = macports.Snapshot{Ports: map[string]macports.PortInfo{"demo": {Name: "demo", Options: map[string]string{"dockhand.portgroups": "python"},
			Dependencies: []macports.Dependency{{Port: "py313-requests"}}}}}
		e := Engine{PortReader: fakePorts{directories: map[string][]macports.PortInfo{"python/py-requests": {{Name: "py313-requests", Version: "1"}}}}}
		_, requirements := compareUpstream(t.Context(), result, sourcecompare.Versions{})
		require.Len(t, requirements, 1, marker)
		pins := e.pythonPins(t.Context(), model.Source{}, result, requirements)
		if holds {
			require.Len(t, pins, 1, marker)
			require.True(t, pins[0].Hold, marker)
			require.Contains(t, pins[0].Message, "requires requests ==999, which MacPorts' py313-requests 1 doesn't meet")
		} else {
			require.Empty(t, pins, marker)
		}
	}
}
