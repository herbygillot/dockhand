package engine

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/editprep"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/forge/forgetest"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/assess"
	"github.com/herbygillot/dockhand/internal/macports/distfetch"
	"github.com/herbygillot/dockhand/internal/macports/portedit"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/sourcecompare"
	"github.com/herbygillot/dockhand/internal/store"
	"github.com/herbygillot/dockhand/internal/testsupport"
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
	requests []editprep.Request
	// during runs while the edit is prepared.
	during func()
	// upstream are the old and new versions' archive contents, kept as
	// tarballs when an update asks to compare them.
	upstream [2]map[string]string
	// toolchain is a Go requirement an update's go.mod leaves above the
	// Portfile's minimum.
	toolchain *editprep.GoToolchain
	// dependencies are the ports the updated port depends on.
	dependencies []string
	// options are the evaluated port's options after the edit.
	options map[string]string
	// family are the other ports the Portfile defines, as the edit
	// evaluates them before it.
	family map[string]macports.PortInfo
}

func (p *fakePreparer) ResolveRelease(_ context.Context, r editprep.Request) (model.Release, error) {
	if r.Release != nil {
		return *r.Release, nil
	}
	version := p.version
	if r.Version != "" {
		version = r.Version
	}
	return model.Release{ReleaseSelection: model.ReleaseSelection{Requested: r.Version}, Version: version, Forge: "github", Tag: "jq-" + version}, nil
}

func (p *fakePreparer) Prepare(ctx context.Context, r editprep.Request) (editprep.Result, error) {
	p.requests = append(p.requests, r)
	// The editor holds a version update's request to its release's, as
	// portedit's planArchiveVersion does: update --outdated gave a found
	// release with its version, and every port was refused (field
	// testing, 2026-10-02).
	if r.Action == model.EditUpdate && (r.Release == nil || r.Release.Requested != r.Version) {
		return editprep.Result{}, fmt.Errorf("portedit: a matching resolved release is required")
	}
	name := "textproc/" + r.Selection.Selector + "/Portfile"
	before, data, err := p.repo.File(ctx, string(r.Source.Tree), name)
	if err != nil {
		return editprep.Result{}, err
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
	// A Go requirement is a module-mode Go port's, whose minimum is as the
	// toolchain says the edit found it, and left it.
	options := [2]map[string]string{p.options, p.options}
	if t := p.toolchain; t != nil {
		for i, minimum := range []string{t.Declared, t.Declared} {
			if i == 1 && t.Outcome == editprep.GoToolchainRaised {
				minimum = t.Required
			}
			options[i] = map[string]string{"go.package": "example.org/jq", "go.offline_build": "no"}
			maps.Copy(options[i], p.options)
			if minimum != "" {
				options[i]["go.toolchain_min"] = minimum
			}
		}
	}
	snapshot := func(version string, revision int, options map[string]string) macports.Snapshot {
		return macports.Snapshot{Ports: map[string]macports.PortInfo{r.Selection.Selector: {Name: r.Selection.Selector, Version: version, Revision: revision, Options: options}}}
	}
	result := editprep.Result{Target: model.Target{Name: r.Selection.Selector, Portfile: name}, Release: r.Release, PreparedTree: r.Source.Tree,
		Fidelity: []portedit.Fidelity{{Before: snapshot(string(old), revision, options[0]), After: snapshot(next, nextRevision, options[1])}}}
	for other, info := range p.family {
		if other != r.Selection.Selector {
			result.Fidelity[0].Before.Ports[other] = info
		}
	}
	// The port depended on the same ports before the edit.
	if len(p.dependencies) > 0 {
		before := port(r.Selection.Selector, p.dependencies...)
		before.Version, before.Revision, before.Options = string(old), revision, options[0]
		result.Fidelity[0].Before.Ports[r.Selection.Selector] = before
	}
	if after == string(data) {
		return result, nil
	}
	edit := git.FileEdit{Path: name, Before: before, After: []byte(after), Mode: before.Mode}
	tree, err := p.repo.EditTree(ctx, string(r.Source.Tree), []git.FileEdit{edit})
	if err != nil {
		return editprep.Result{}, err
	}
	result.Files, result.PreparedTree = []git.FileEdit{edit}, model.ObjectID(tree)
	result.GoToolchain = p.toolchain
	if len(p.dependencies) > 0 {
		result.Prepared = snapshot(next, nextRevision, options[1])
		result.Prepared.Ports[r.Selection.Selector] = port(r.Selection.Selector, p.dependencies...)
	}
	result.Commits = []editprep.CommitIntent{{Subject: r.Selection.Selector + ": update to " + next}}
	if r.Action == model.EditRevbump {
		result.Commits[0].Subject = r.Selection.Selector + ": " + r.Subject
	}
	// The new archive replaces the old one where both are given; given
	// alone, it replaces none dockhand found.
	if r.KeepArchives != "" && p.upstream[1] != nil {
		next := distfetch.Download{Path: writeTarball(p.t, r.KeepArchives, "new", p.upstream[1])}
		next.Name = "new.tar.gz"
		result.Downloads = []distfetch.Download{next}
		if p.upstream[0] != nil {
			result.Pairs = []editprep.ArchivePair{{Previous: distfetch.Download{Path: writeTarball(p.t, r.KeepArchives, "old", p.upstream[0])}, Next: next}}
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
	t.Parallel()
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
	require.Equal(t, string(branch.Base), testsupport.Git(t, branch.Worktree, "rev-parse", "HEAD"), "nothing is committed")
	require.Equal(t, "M textproc/jq/Portfile", testsupport.Git(t, branch.Worktree, "status", "--porcelain"))

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
		require.Equal(t, testsupport.Git(t, branch.Worktree, "rev-parse", "HEAD:textproc/jq/Portfile"), string(edits[0].Files[0].Before))
		require.Equal(t, testsupport.Git(t, branch.Worktree, "hash-object", "textproc/jq/Portfile"), string(edits[0].Files[0].After))
		return nil
	}))
}

func TestUpdateStartsFromTheWorkingFilesAsTheyAre(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-update"})
	require.NoError(t, err)
	update, err := e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditUpdate, Port: "jq", Plan: true})
	require.NoError(t, err)
	require.False(t, update.Applied)
	require.Contains(t, update.Diff, "-version 1.7.1\n+version 1.8.1")
	require.NoFileExists(t, filepath.Join(branch.Worktree, "textproc/jq/Portfile"))
	require.Empty(t, testsupport.Git(t, branch.Worktree, "status", "--porcelain"))
}

func TestAnUpdateWritesNothingOverAFileThatChanged(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-update"})
	require.NoError(t, err)
	testsupport.Git(t, branch.Worktree, "switch", "-q", "--detach")
	_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditUpdate, Port: "jq"})
	require.ErrorContains(t, err, "is not checked out in")

	// A branch with no worktree is checked out for the update, which a
	// Git branch that's gone can't be.
	_, err = e.Update(t.Context(), UpdateRequest{Branch: model.Branch{Name: "dockhand/elsewhere"}, Action: model.EditUpdate, Port: "jq"})
	require.ErrorContains(t, err, "local branch no longer exists: dockhand/elsewhere")
	_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditKind("publish"), Port: "jq"})
	require.ErrorContains(t, err, "not an update")
	// A release found already is the version asked for, for a version
	// update, or it is refused rather than taken.
	found := &model.Release{Version: "1.8.1", Forge: "github", Tag: "jq-1.8.1"}
	_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditUpdate, Port: "jq", Version: "1.8.0", Release: found})
	require.ErrorContains(t, err, `the release found is 1.8.1's, for a version update to "1.8.0"`)
	_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditChecksums, Port: "jq", Version: "1.8.1", Release: found})
	require.ErrorContains(t, err, "the release found is 1.8.1's")
	// One found automatically, as outdated finds it, is taken with the
	// version it names, and the editor is asked for no version of its
	// own: update --outdated refused every port (field testing,
	// 2026-10-02).
	fresh, err := e.Start(t.Context(), StartRequest{Name: "jq-found"})
	require.NoError(t, err)
	updated, err := e.Update(t.Context(), UpdateRequest{Branch: fresh, Action: model.EditUpdate, Port: "jq", Version: "1.8.1", Release: found})
	require.NoError(t, err)
	require.Equal(t, "1.8.1", updated.After.Version)
}

func TestBranchesChangingAndFreeNames(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-update"})
	require.NoError(t, err)
	changing, err := e.BranchesChanging(t.Context(), "jq")
	require.NoError(t, err)
	require.Empty(t, changing, "uncommitted work changes nothing yet")

	_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditUpdate, Port: "jq"})
	require.NoError(t, err)
	testsupport.Git(t, branch.Worktree, "commit", "-q", "-am", "jq: update to 1.8.1")
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
	t.Parallel()
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
		"upstream: go.mod: 1 added (golang.org/x/net)",
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
	require.True(t, NothingCompared(*quiet.Upstream), "no archives, nothing compared, and said: %+v", quiet.Upstream)
	require.Equal(t, "jq fetches no upstream source, so there's nothing to compare", CoverageWords(*quiet.Upstream))
	require.False(t, quiet.Upstream.Held())
}

// refusing can't make the edit by itself, as for a port whose pre-fetch
// hook runs a command.
type refusing struct{ *fakePreparer }

func (refusing) Prepare(context.Context, editprep.Request) (editprep.Result, error) {
	return editprep.Result{}, fmt.Errorf("%w: its pre-fetch hook runs exec", ErrUnsupported)
}

// A version update asked to start its branch prepares it on master and
// starts the branch from that master only once there is an edit to make,
// or one for the person to make by hand.
func TestAnUpdateStartsItsBranchOnlyForAnEdit(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	update := describe(model.Branch{}, "jq", editprep.Result{Result: portedit.Result{Unchanged: &macports.PortInfo{Name: "jq", Version: "1.8.2", Revision: 1}}})
	require.Equal(t, PortVersion{Version: "1.8.2", Revision: 1}, update.Before)
	require.Equal(t, PortVersion{Version: "1.8.2", Revision: 1}, update.After)
	require.Equal(t, "1.8.2_1", update.After.String())
}

// Each archive an update replaced is compared with its own replacement,
// and a change they share, such as the license both carry, is said once;
// what only one carries names it (the architecture review's finding 2).
func TestAChangeTheArchivesShareIsSaidOnce(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pair := func(name string, before, after map[string]string) editprep.ArchivePair {
		next := distfetch.Download{Path: writeTarball(t, dir, name+"-2", after)}
		next.Name = name + "-2.tar.gz"
		return editprep.ArchivePair{Previous: distfetch.Download{Path: writeTarball(t, dir, name+"-1", before)}, Next: next}
	}
	source := pair("source", map[string]string{"LICENSE": "MIT\n"}, map[string]string{"LICENSE": "Apache-2.0\n", "meson.build": "project('x')\n"})
	binary := pair("binary", map[string]string{"LICENSE": "MIT\n"}, map[string]string{"LICENSE": "Apache-2.0\n"})
	result := editprep.Result{}
	result.Downloads = []distfetch.Download{source.Next, binary.Next}
	result.Pairs = []editprep.ArchivePair{source, binary}
	comparison := (&Engine{}).assessUpstream(t.Context(), result, sourcecompare.Versions{}, [2]model.Source{}, true)
	require.Empty(t, comparison.Problem)
	require.Equal(t, []model.UpstreamChange{
		{Kind: "license", Path: "LICENSE", Message: "upstream's LICENSE changed; the Portfile's license line may need to follow", Hold: true, Rule: assess.LicenseChanged, Class: model.Introduced},
		{Kind: "build", Path: "meson.build", Message: "upstream: source-2.tar.gz: meson.build is new; the build may need the Portfile to follow", Hold: true, Rule: assess.BuildFileChanged, Class: model.Introduced, Source: "source-*.tar.gz"},
	}, comparison.Changes)
}

// Each version is read where its port builds, as its worksrcdir names it:
// a project nested in python/ is compared there, with the license at the
// archive's top, and a manifest outside it isn't read (the update-workflow
// review's finding 1).
func TestTheComparisonReadsWhereThePortBuilds(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	next := distfetch.Download{Path: writeTarball(t, dir, "demo-2", map[string]string{
		"LICENSE": "MIT\n", "python/pyproject.toml": "[project]\ndependencies = [\"requests>=2\", \"rich>=13\"]\n", "package.json": `{"dependencies":{"x":"1"}}`,
	})}
	next.Name = "demo-2.tar.gz"
	previous := distfetch.Download{Path: writeTarball(t, dir, "demo-1", map[string]string{
		"LICENSE": "MIT\n", "python/pyproject.toml": "[project]\ndependencies = [\"requests>=2\"]\n",
	})}
	result := editprep.Result{}
	result.Target = model.Target{Name: "py-demo"}
	result.Unchanged = &macports.PortInfo{Name: "py-demo", Options: map[string]string{"worksrcdir": "demo-1/python"}}
	result.Prepared = macports.Snapshot{Ports: map[string]macports.PortInfo{"py-demo": {Name: "py-demo", Options: map[string]string{"worksrcdir": "demo-2/python"}}}}
	result.Downloads = []distfetch.Download{next}
	result.Pairs = []editprep.ArchivePair{{Previous: previous, Next: next}}
	comparison := (&Engine{}).assessUpstream(t.Context(), result, sourcecompare.Versions{}, [2]model.Source{}, true)
	require.Empty(t, comparison.Problem)
	require.Equal(t, []model.UpstreamChange{
		{Kind: "dependency", Path: "python/pyproject.toml", Message: "upstream: python/pyproject.toml adds rich >=13", Hold: true, Rule: assess.DependencyAdded, Subject: "rich", Class: model.Introduced},
		{Kind: "dependency", Path: "python/pyproject.toml", Message: "upstream: python/pyproject.toml requires rich >=13, and no port the Portfile depends on is named for it",
			Rule: assess.ProviderUnresolved, Subject: "rich", Class: model.Introduced},
	}, comparison.Changes)
}

// A Python requirement the new version moves holds where the port that
// provides it doesn't meet it at the version the branch has: sqlit-tui
// 1.6.4 pins textual-fastdatatable==0.19.0, and MacPorts had 0.17.1, while
// a noarch build passes regardless (the sshuttle run). A branch that
// updates the dependency first meets it (the libuv run). One whose
// provider's version can't be told holds too, and one no dependency's name
// matches is said, holding nothing (batch 19).
func TestAPythonPinMacPortsCantMeetHolds(t *testing.T) {
	t.Parallel()
	unresolved := model.UpstreamChange{Kind: "dependency", Path: "pyproject.toml", Rule: assess.ProviderUnresolved, Subject: "pyyaml", Class: model.Introduced,
		Message: "upstream: pyproject.toml requires pyyaml >=6.0.2, and no port the Portfile depends on is named for it"}
	for _, test := range []struct {
		name, has string
		want      []model.UpstreamChange
	}{
		{"unmet", "0.17.1", []model.UpstreamChange{unresolved, {Kind: "dependency", Path: "pyproject.toml", Hold: true, Rule: assess.RequirementUnmet, Subject: "textual-fastdatatable", Class: model.Introduced,
			Message: "upstream: pyproject.toml requires textual-fastdatatable ==0.19.0, which MacPorts' py313-textual-fastdatatable 0.17.1 doesn't meet"}}},
		{"met by the branch", "0.19.0", []model.UpstreamChange{unresolved}},
		{"unreadable", "not-a-version", []model.UpstreamChange{unresolved, {Kind: "dependency", Path: "pyproject.toml", Hold: true, Rule: assess.RequirementUnknown, Subject: "textual-fastdatatable", Class: model.Introduced,
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
			require.Equal(t, test.want, pins)
		})
	}
}

// An update on a fresh branch records its own comparison as the
// assessment of the files it leaves, since it compared them against the
// base in every context; a Git-fetched port's compared no archives, and
// records none, so its assessment is made when it's collected.
func TestAFreshUpdateRecordsItsAssessment(t *testing.T) {
	t.Parallel()
	for _, git := range []bool{false, true} {
		f := setup(t)
		e, p := f.withPreparer(t)
		p.upstream = [2]map[string]string{{"LICENSE": "MIT\n"}, {"LICENSE": "GPL\n"}}
		if git {
			p.options = map[string]string{"fetch.type": "git"}
		}
		update, err := e.Update(t.Context(), UpdateRequest{Start: &StartRequest{Name: "jq-update"}, Action: model.EditUpdate, Port: "jq", CompareUpstream: true})
		require.NoError(t, err)
		var recorded []model.Assessment
		require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
			var err error
			recorded, err = r.Assessments(store.AssessmentFilter{Branch: update.Branch.ID})
			return err
		}))
		if git {
			require.Empty(t, recorded)
			continue
		}
		require.Len(t, recorded, 1)
		require.Equal(t, *update.Upstream, recorded[0].Comparison)
		require.Equal(t, update.Branch.Base, recorded[0].Base)
	}
}

// The base's provider is read in the base's tree: a requirement the
// candidate's provider doesn't meet, which the base's didn't either, is
// said and holds nothing, while one the base met holds.
func TestAPinIsJudgedAgainstTheBasesTree(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		base string
		hold bool
	}{{"12", false}, {"13.1", true}} {
		dir := t.TempDir()
		old := distfetch.Download{Name: "old.tar.gz", Path: writeTarball(t, dir, "pkg-1", map[string]string{"requirements.txt": "rich>=13\n"})}
		next := distfetch.Download{Name: "new.tar.gz", Path: writeTarball(t, dir, "pkg-2", map[string]string{"requirements.txt": "rich>=14\n"})}
		result := editprep.Result{}
		result.Target = model.Target{Name: "demo"}
		result.Downloads = []distfetch.Download{next}
		result.Pairs = []editprep.ArchivePair{{Previous: old, Next: next}}
		demo := macports.PortInfo{Name: "demo", Options: map[string]string{"dockhand.portgroups": "python"}, Dependencies: []macports.Dependency{{Port: "py313-rich"}}}
		result.Unchanged = &demo
		result.Prepared = macports.Snapshot{Ports: map[string]macports.PortInfo{"demo": demo}}
		e := Engine{PortReader: fakePorts{directories: map[string][]macports.PortInfo{"python/py-rich": {{Name: "py313-rich", Version: "13.2"}}},
			trees: map[model.ObjectID]map[string][]macports.PortInfo{"base": {"python/py-rich": {{Name: "py313-rich", Version: test.base}}}}}}
		comparison := e.assessUpstream(t.Context(), result, sourcecompare.Versions{}, [2]model.Source{{Tree: "base"}, {Tree: "new"}}, true)
		var unmet []model.UpstreamChange
		for _, change := range comparison.Changes {
			if change.Rule == assess.RequirementUnmet {
				unmet = append(unmet, change)
			}
		}
		require.Len(t, unmet, 1, test.base)
		require.Equal(t, test.hold, unmet[0].Hold, test.base)
	}
}

// A file of a build system the port doesn't use holds nothing, and says
// why: flatbuffers, built with CMake, held on package.json and
// Package.swift (the flatbuffers run's finding 2). A build file of the one
// it uses still holds, and one that changed only the version it names
// doesn't (nuspell's CMakeLists.txt).
func TestAChangeTheBuildDoesntReadHoldsNothing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	next := distfetch.Download{Path: writeTarball(t, dir, "flatbuffers-25.12.19", map[string]string{
		"CMakeLists.txt": "project(FlatBuffers VERSION 25.12.19)\nadd_library(flatbuffers src/a.cpp src/b.cpp)\n",
		"package.json":   `{"devDependencies": {"eslint": "9.0.0", "typescript": "5.8.3"}}`,
		"Package.swift":  "// swift-tools-version:5.9\n",
	})}
	next.Name = "flatbuffers-25.12.19.tar.gz"
	previous := distfetch.Download{Path: writeTarball(t, dir, "flatbuffers-25.9.23", map[string]string{
		"CMakeLists.txt": "project(FlatBuffers VERSION 25.9.23)\nadd_library(flatbuffers src/a.cpp)\n",
		"package.json":   `{"devDependencies": {"eslint": "8.0.0"}}`,
	})}
	result := editprep.Result{}
	result.Target = model.Target{Name: "flatbuffers"}
	result.Prepared = macports.Snapshot{Ports: map[string]macports.PortInfo{"flatbuffers": {Name: "flatbuffers", Options: map[string]string{"dockhand.portgroups": "github cmake", "use_configure": "yes", "configure.cmd": "/opt/local/bin/cmake"}}}}
	// A second archive with the same package.json counts nothing twice.
	other := next
	other.Name = "flatbuffers-25.12.19.zip"
	result.Downloads = []distfetch.Download{next, other}
	result.Pairs = []editprep.ArchivePair{{Previous: previous, Next: next}, {Previous: previous, Next: other}}
	comparison := (&Engine{}).assessUpstream(t.Context(), result, sourcecompare.Versions{Old: "25.9.23", New: "25.12.19"}, [2]model.Source{}, true)
	require.Equal(t, []model.UpstreamChange{
		{Kind: "build", Path: "CMakeLists.txt", Message: "upstream's CMakeLists.txt changed, though no option or find_package did; lines change outside any if(); the build may need the Portfile to follow", Hold: true, Rule: assess.BuildFileChanged, Class: model.Introduced},
	}, comparison.Changes)
	// The files of build systems flatbuffers doesn't use are said in
	// coverage alone, set apart (rust 1.99.0's package.json, batch 23).
	require.Equal(t, "Compared CMakeLists.txt; not compared, as the port doesn't build with them: Package.swift (swift), package.json (node)", CoverageWords(*comparison))
}

// What an option off by default gates holds nothing only where the
// Portfile, as the update left it, doesn't name the option, in any
// variant: one that does may set it (D12, revisited 2026-10-01).
func TestAnOptionThePortfileNamesHolds(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	dir := t.TempDir()
	next := distfetch.Download{Path: writeTarball(t, dir, "fluent-bit-5.1.3", map[string]string{
		"CMakeLists.txt": "project(fluent-bit VERSION 5.1.3)\noption(FLB_PROTOBUF_ENCODER \"Protobuf\" No)\nif(FLB_PROTOBUF_ENCODER)\n  find_package(Protobuf REQUIRED)\nendif()\nadd_library(flb a.c)\n",
	})}
	next.Name = "fluent-bit-5.1.3.tar.gz"
	previous := distfetch.Download{Path: writeTarball(t, dir, "fluent-bit-5.1.2", map[string]string{
		"CMakeLists.txt": "project(fluent-bit VERSION 5.1.2)\nadd_library(flb a.c)\n",
	})}
	held := func(portfile string) bool {
		t.Helper()
		write(t, f.clone, map[string]string{"sysutils/fluent-bit/Portfile": portfile})
		testsupport.Git(t, f.clone, "add", "sysutils/fluent-bit/Portfile")
		testsupport.Git(t, f.clone, "commit", "-q", "-m", "fluent-bit")
		tree := strings.TrimSpace(testsupport.Git(t, f.clone, "rev-parse", "HEAD^{tree}"))
		result := editprep.Result{}
		result.Target = model.Target{Name: "fluent-bit", Portfile: "sysutils/fluent-bit/Portfile"}
		result.Prepared = macports.Snapshot{Ports: map[string]macports.PortInfo{"fluent-bit": {Name: "fluent-bit", Options: map[string]string{"dockhand.portgroups": "github cmake", "use_configure": "yes", "configure.cmd": "/opt/local/bin/cmake"}}}}
		result.Downloads = []distfetch.Download{next}
		result.Pairs = []editprep.ArchivePair{{Previous: previous, Next: next}}
		comparison := e.assessUpstream(t.Context(), result, sourcecompare.Versions{Old: "5.1.2", New: "5.1.3"}, [2]model.Source{{}, {Tree: model.ObjectID(tree)}}, true)
		return comparison.Held()
	}
	require.False(t, held("name fluent-bit\nversion 5.1.3\nconfigure.args-append -DFLB_WASM=OFF\n"))
	require.True(t, held("name fluent-bit\nversion 5.1.3\nvariant protobuf {\n    configure.args-append -DFLB_PROTOBUF_ENCODER=ON\n}\n"))
}

// A version update or a checksum refresh says the port's URLs over plain
// HTTP, its homepage and its master_sites, with whether each answers over
// HTTPS, which MacPorts prefers; a mirror group is MacPorts' own, and a
// revision bump doesn't look. Nor does an update with nothing to change,
// which says the port is current without waiting on its hosts.
func TestAnUpdateSaysThePortsPlainHTTPURLs(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, p := f.withPreparer(t)
	p.options = map[string]string{"homepage": "http://jqlang.example/", "master_sites": "http://dl.example/jq/:src gnu https://github.com/jqlang/jq/releases/"}
	probe := newGatedProbe(testsupport.HTTPSAnswers{"https://jqlang.example/": true}, false)
	e.HTTPS = probe
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

	asked := probe.asked["https://jqlang.example/"]
	for _, action := range []model.EditKind{model.EditUpdate, model.EditChecksums} {
		current, err := e.Update(t.Context(), UpdateRequest{Branch: update.Branch, Action: action, Port: "jq"})
		require.NoError(t, err)
		require.True(t, current.Current, action)
		require.Empty(t, current.PlainHTTP, action)
	}
	require.Equal(t, asked, probe.asked["https://jqlang.example/"], "a current port's URLs aren't asked")
}

// A Python pin that applies only elsewhere asks nothing of MacPorts' port:
// a Windows-only requests==999 doesn't hold an update MacPorts'
// py313-requests 1 builds, while the same pin for macOS does (the
// helper-ownership review's finding 1, its probe as a regression test).
func TestAPinForAnotherPlatformHoldsNothing(t *testing.T) {
	t.Parallel()
	for marker, holds := range map[string]bool{"sys_platform == 'win32'": false, "sys_platform == 'darwin'": true, "python_version >= '3.12'": true, "python_version < '3.10'": false} {
		dir := t.TempDir()
		old := distfetch.Download{Name: "old.tar.gz", Path: writeTarball(t, dir, "pkg-1", map[string]string{"requirements.txt": "requests==1; " + marker + "\n"})}
		next := distfetch.Download{Name: "new.tar.gz", Path: writeTarball(t, dir, "pkg-2", map[string]string{"requirements.txt": "requests==999; " + marker + "\n"})}
		result := editprep.Result{}
		result.Target = model.Target{Name: "demo"}
		result.Downloads = []distfetch.Download{next}
		result.Pairs = []editprep.ArchivePair{{Previous: old, Next: next}}
		result.Prepared = macports.Snapshot{Ports: map[string]macports.PortInfo{"demo": {Name: "demo", Options: map[string]string{"dockhand.portgroups": "python"},
			Dependencies: []macports.Dependency{{Port: "py313-requests"}}}}}
		e := Engine{PortReader: fakePorts{directories: map[string][]macports.PortInfo{"python/py-requests": {{Name: "py313-requests", Version: "1"}}}}}
		var pins []model.UpstreamChange
		for _, change := range e.assessUpstream(t.Context(), result, sourcecompare.Versions{}, [2]model.Source{}, true).Changes {
			if change.Rule == assess.RequirementUnmet {
				pins = append(pins, change)
			}
		}
		if holds {
			require.Len(t, pins, 1, marker)
			require.True(t, pins[0].Hold, marker)
			require.Contains(t, pins[0].Message, "requires requests ==999, which MacPorts' py313-requests 1 doesn't meet")
		} else {
			require.Empty(t, pins, marker)
		}
	}
}

// A checksum refresh's stealth decision gets the port as the base
// evaluates it, where the branch changed its Portfile since, and nothing
// otherwise, or where the base has no such port.
func TestAStealthRefreshGetsTheBasesPort(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "stealth"})
	require.NoError(t, err)
	trees, err := e.Repo.CommitTrees(t.Context(), []string{string(branch.Base)})
	require.NoError(t, err)
	base := model.ObjectID(trees[string(branch.Base)])
	e.PortReader = fakePorts{directories: map[string][]macports.PortInfo{"textproc/jq": {{Name: "jq", Version: "1.8.1"}}},
		trees: map[model.ObjectID]map[string][]macports.PortInfo{base: {"textproc/jq": {{Name: "jq", Version: "1.7.1"}}}}}
	source := model.Source{Tree: "branch-tree", Base: branch.Base}
	port := e.basePort(t.Context(), source, branch.Base, "jq", []string{"textproc/jq/Portfile"})
	require.NotNil(t, port)
	require.Equal(t, "1.7.1", port.Version, "the base's, not the branch's")
	require.Nil(t, e.basePort(t.Context(), source, branch.Base, "jq", []string{"textproc/jq/files/patch-a.diff"}), "the Portfile didn't change")
	require.Nil(t, e.basePort(t.Context(), source, branch.Base, "gone", []string{"textproc/gone/Portfile"}), "no such port")
}

// An update to a new major version says so: semgrep's 0.14.0 to 1.179.0
// read as a plain bump (field testing, 2026-10-02).
func TestAnUpdateSaysANewMajorVersion(t *testing.T) {
	t.Parallel()
	result := func(from, to string) editprep.Result {
		port := func(version string) macports.Snapshot {
			return macports.Snapshot{Ports: map[string]macports.PortInfo{"semgrep": {Name: "semgrep", Version: version}}}
		}
		return editprep.Result{Target: model.Target{Name: "semgrep"}, Fidelity: []portedit.Fidelity{{Before: port(from), After: port(to)}}}
	}
	require.True(t, describe(model.Branch{}, "semgrep", result("0.14.0", "1.179.0")).CrossesMajor)
	require.False(t, describe(model.Branch{}, "semgrep", result("1.178.0", "1.179.0")).CrossesMajor)
}

// renamedRepository is a GitHub repository answering by another name, as
// a renamed one does through GitHub's redirect.
type renamedRepository struct {
	forge.Repository
	name, now string
}

func (r renamedRepository) Name() string { return r.name }
func (r renamedRepository) Describe(context.Context) (forge.Description, error) {
	return forge.Description{Name: r.now}, nil
}

// renamingForge answers each repository by the name it has now.
type renamingForge struct {
	*forgetest.GitHub
	now map[string]string
}

func (f renamingForge) Repository(_, name string) (forge.Repository, error) {
	now := f.now[name]
	if now == "" {
		now = name
	}
	return renamedRepository{name: name, now: now}, nil
}

// An update's release from a repository GitHub now names otherwise says
// so: returntocorp/semgrep answered as semgrep/semgrep by a redirect
// discovery followed unsaid (field testing, 2026-10-02).
func TestAnUpdateSaysItsRepositoryWasRenamed(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	e.Forge = renamingForge{GitHub: forgetest.New("", ""), now: map[string]string{"returntocorp/semgrep": "semgrep/semgrep"}}
	require.Equal(t, "semgrep/semgrep", e.renamed(t.Context(), model.Release{Forge: forge.GitHub, Repository: "returntocorp/semgrep"}))
	require.Empty(t, e.renamed(t.Context(), model.Release{Forge: forge.GitHub, Repository: "jqlang/jq"}))
	require.Empty(t, e.renamed(t.Context(), model.Release{Forge: forge.GitLab, Repository: "returntocorp/semgrep"}))
}

// The obsolete stub a Portfile keeps for a port, replaced_by it and
// fetching nothing, is said to stay where it is, and moved with the port,
// in the same commit, with --with-obsolete: terraform's stayed at 1.16.0
// while terraform-1.16 moved to 1.16.5 (field testing, 2026-10-02).
func TestAnObsoleteStubMovesWithItsReplacementWhenAsked(t *testing.T) {
	t.Parallel()
	f := setup(t)
	write(t, f.upstream, map[string]string{"textproc/jq-old/Portfile": "name jq-old\nversion 1.7.1\n"})
	testsupport.Git(t, f.upstream, "add", "-A")
	testsupport.Git(t, f.upstream, "commit", "-q", "-m", "jq-old: obsolete")
	e, p := f.withPreparer(t)
	p.family = map[string]macports.PortInfo{"jq-old": {Name: "jq-old", Version: "1.7.1", Options: map[string]string{"replaced_by": "jq", "distfiles": ""}}}

	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-update"})
	require.NoError(t, err)
	stays, err := e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditUpdate, Port: "jq", Plan: true})
	require.NoError(t, err)
	require.Equal(t, &ObsoleteStub{Port: "jq-old", Version: "1.7.1"}, stays.Obsolete)

	moved, err := e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditUpdate, Port: "jq", WithObsolete: true})
	require.NoError(t, err)
	require.Equal(t, &ObsoleteStub{Port: "jq-old", Version: "1.7.1", Moved: true}, moved.Obsolete)
	require.Equal(t, "name jq-old\nversion 1.8.1\n", read(t, filepath.Join(branch.Worktree, "textproc/jq-old/Portfile")))
	var edits []model.Edit
	require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
		edits, err = r.Edits(branch.ID)
		return err
	}))
	require.Len(t, edits, 2)
	for _, edit := range edits {
		require.Equal(t, "jq: update to 1.8.1", edit.Subject, "the stub's edit is the replacement's update")
	}
}
