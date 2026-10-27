package engine

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/comparison"
	"github.com/herbygillot/dockhand/internal/comparison/comparisontest"
	"github.com/herbygillot/dockhand/internal/editprep"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/assess"
	"github.com/herbygillot/dockhand/internal/macports/portedit"
	"github.com/herbygillot/dockhand/internal/macports/treequery"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/refusal"
	"github.com/herbygillot/dockhand/internal/sourcecompare"
	"github.com/herbygillot/dockhand/internal/store"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// The fake planner is comparisontest's, which the comparison's own tests
// share.
type (
	fakePlanner = comparisontest.Planner
	plannedPort = comparisontest.PlannedPort
)

func newPlanner(t *testing.T) *fakePlanner { return comparisontest.NewPlanner(t) }

// revisionFixture is a branch on master, and a revision of it whose files
// are master's with those given.
func revisionFixture(t *testing.T, files map[string]string) (*Engine, model.Branch, model.ObjectID, model.ObjectID) {
	t.Helper()
	f := setup(t)
	f.options.Readings = t.TempDir()
	e := f.open(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "assessed"})
	require.NoError(t, err)
	trees, err := e.Repo.CommitTrees(t.Context(), []string{string(branch.Base)})
	require.NoError(t, err)
	base := model.ObjectID(trees[string(branch.Base)])
	return e, branch, base, editTree(t, e, base, files)
}

// editTree is a tree with the files given.
func editTree(t *testing.T, e *Engine, tree model.ObjectID, files map[string]string) model.ObjectID {
	t.Helper()
	var edits []git.FileEdit
	for _, name := range slices.Sorted(maps.Keys(files)) {
		before, _, err := e.Repo.File(t.Context(), string(tree), name)
		require.NoError(t, err)
		mode := before.Mode
		if !before.Exists {
			mode = 0o100644
		}
		edits = append(edits, git.FileEdit{Path: name, Before: before, After: []byte(files[name]), Mode: mode})
	}
	edited, err := e.Repo.EditTree(t.Context(), string(tree), edits)
	require.NoError(t, err)
	return model.ObjectID(edited)
}

// holds are an assessment's findings that hold, by their words.
func holds(a model.Assessment) []string {
	return a.Comparison.Holds()
}

// It's the net change from the base that's assessed, never the edits
// that made it: jq went MIT to GPL and back to MIT through its updates,
// whose recorded comparisons each held, and the revision holds nothing
// (the assessment design's step 3 fixture). What's recorded is read by
// status without collecting, and a later revision whose archives are the
// same fetches nothing again.
func TestARevisionIsAssessedByItsNetChange(t *testing.T) {
	t.Parallel()
	e, branch, base, tree := revisionFixture(t, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\n"})
	p := newPlanner(t)
	e.Ports.Archives = p
	usePorts(e, fakePorts{directories: map[string][]macports.PortInfo{"textproc/jq": {{Name: "jq"}}}})
	p.Add(base, plannedPort{Info: macports.PortInfo{Name: "jq", Version: "1.7.1"}, Archives: map[string]map[string]string{"jq-1.7.1.tar.gz": {"LICENSE": "MIT\n"}}})
	p.Add(tree, plannedPort{Info: macports.PortInfo{Name: "jq", Version: "1.8.1"}, Archives: map[string]map[string]string{"jq-1.8.1.tar.gz": {"LICENSE": "MIT\n", "meson.build": "project('jq')\n"}}})
	gpl := &model.UpstreamComparison{Changes: []model.UpstreamChange{{Kind: "license", Path: "LICENSE", Message: "upstream's LICENSE changed", Hold: true}}}
	require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error {
		for i := range 2 {
			if err := tx.AddEdit(model.Edit{ID: model.EditID(fmt.Sprint("ed_", i)), Branch: branch.ID, Kind: model.EditUpdate, Port: "jq", Directory: "textproc/jq",
				Subject: "jq: update", Files: []model.EditedFile{{Path: "textproc/jq/Portfile", Before: "a", After: "b"}}, At: at, Upstream: gpl}); err != nil {
				return err
			}
		}
		return nil
	}))

	held, state, err := e.assessedHolds(t.Context(), branch, tree, []string{"textproc/jq"})
	require.NoError(t, err)
	require.Empty(t, held)
	require.Equal(t, AssessmentPending, state, "status collects nothing")
	require.Zero(t, p.Fetches.Load())

	assessments, err := e.revisionAssessments(t.Context(), branch.ID, branch.Base, tree, true)
	require.NoError(t, err)
	require.Len(t, assessments, 1)
	require.Equal(t, "jq", assessments[0].Port)
	require.Equal(t, "textproc/jq", assessments[0].Directory)
	require.Equal(t, []string{"upstream's meson.build is new; the build may need the Portfile to follow"}, messagesOf(assessments[0].Comparison),
		"the license is MIT at both ends, and holds nothing; what did change is said")
	require.EqualValues(t, 2, p.Fetches.Load())
	// What's recorded is the collector's comparison, as it collects it
	// (Then's item 15).
	_, portfile, err := e.Repo.File(t.Context(), string(tree), "textproc/jq/Portfile")
	require.NoError(t, err)
	collected := e.collector(p).Revision(t.Context(), [2]model.Source{{Commit: branch.Base, Tree: base, Base: branch.Base}, {Tree: tree, Base: branch.Base}}, "textproc/jq", [2]string{"jq", "jq"}, true, portfile)
	require.Equal(t, collected, assessments[0].Comparison)

	held, state, err = e.assessedHolds(t.Context(), branch, tree, []string{"textproc/jq"})
	require.NoError(t, err)
	require.Equal(t, AssessmentAvailable, state)
	require.Equal(t, []string{"upstream's meson.build is new; the build may need the Portfile to follow"}, HoldLines(held))

	// A later revision that changes an unrelated file reads what was kept.
	later := editTree(t, e, tree, map[string]string{"textproc/jq/files/patch-a.diff": "--- a\n"})
	p.Same(later, tree)
	assessments, err = e.revisionAssessments(t.Context(), branch.ID, branch.Base, later, true)
	require.NoError(t, err)
	require.Len(t, assessments, 1)
	require.EqualValues(t, 2, p.Fetches.Load(), "nothing fetched again")
}

// messagesOf are a comparison's findings' words.
func messagesOf(comparison model.UpstreamComparison) []string {
	var all []string
	for _, change := range comparison.Changes {
		all = append(all, change.Message)
	}
	return all
}

// A new port has no base: its candidate is assessed alone, license and all,
// with nothing said of a missing old archive (the design's fixture). Its
// license is said with the Portfile's line, holding nothing, and its build
// files, all new to it, aren't said: "LICENSE was added" and "Package.swift
// is new" held every new port (the sand-runner port).
func TestANewPortIsAssessedAlone(t *testing.T) {
	t.Parallel()
	e, branch, _, tree := revisionFixture(t, map[string]string{"sysutils/rift/Portfile": "name rift\nversion 0.4.2\n"})
	p := newPlanner(t)
	e.Ports.Archives = p
	usePorts(e, fakePorts{directories: map[string][]macports.PortInfo{"sysutils/rift": {{Name: "rift"}}}})
	p.Add(tree, plannedPort{Info: macports.PortInfo{Name: "rift", Version: "0.4.2", Options: map[string]string{"license": "MIT"}},
		Archives: map[string]map[string]string{"rift-0.4.2.tar.gz": {"LICENSE": "MIT\n", "Package.swift": "// swift-tools-version:6.2\n"}}})
	assessments, err := e.revisionAssessments(t.Context(), branch.ID, branch.Base, tree, true)
	require.NoError(t, err)
	require.Len(t, assessments, 1)
	require.Empty(t, assessments[0].Comparison.Problem)
	require.Equal(t, []string{"upstream ships LICENSE, and the Portfile says MIT"}, messagesOf(assessments[0].Comparison))
	require.False(t, assessments[0].Comparison.Held())
}

// Each subport is assessed for itself: a requirement that applies only to
// Pythons before 3.13 asks something of py312-demo's provider, and nothing
// of py313-demo's (the design's fixture).
func TestEachSubportIsAssessedForItself(t *testing.T) {
	t.Parallel()
	e, branch, base, tree := revisionFixture(t, map[string]string{"python/py-demo/Portfile": "name py-demo\nversion 2\n"})
	p := newPlanner(t)
	e.Ports.Archives = p
	usePorts(e, fakePorts{directories: map[string][]macports.PortInfo{
		"python/py-demo":  {{Name: "py312-demo"}, {Name: "py313-demo"}},
		"python/py-tomli": {{Name: "py312-tomli", Version: "1.0"}, {Name: "py313-tomli", Version: "1.0"}},
	}})
	for _, python := range []string{"312", "313"} {
		port := func(version string) macports.PortInfo {
			return macports.PortInfo{Name: "py" + python + "-demo", Version: version, Options: map[string]string{"dockhand.portgroups": "python"},
				Dependencies: []macports.Dependency{{Port: "py" + python + "-tomli"}}}
		}
		p.Add(base, plannedPort{Info: port("1"), Archives: map[string]map[string]string{"demo-1.tar.gz": {"requirements.txt": "click>=8\n"}}})
		p.Add(tree, plannedPort{Info: port("2"), Archives: map[string]map[string]string{"demo-2.tar.gz": {"requirements.txt": "click>=8\ntomli>=2; python_version < '3.13'\n"}}})
	}
	assessments, err := e.revisionAssessments(t.Context(), branch.ID, branch.Base, tree, true)
	require.NoError(t, err)
	byPort := map[string]model.Assessment{}
	for _, a := range assessments {
		byPort[a.Port] = a
	}
	require.Contains(t, holds(byPort["py312-demo"]), "upstream: requirements.txt requires tomli >=2, which MacPorts' py312-tomli 1.0 doesn't meet")
	require.NotContains(t, strings.Join(holds(byPort["py313-demo"]), "\n"), "py313-tomli")
}

// A Python subport new to the directory is compared with the base's
// sibling of its package, so a license change upstream is said of every
// subport alike, and none is taken for a new port (the Vx port's field
// testing, 2026-10-04, py-coremltools).
func TestANewSubportIsComparedWithItsSibling(t *testing.T) {
	t.Parallel()
	e, branch, base, tree := revisionFixture(t, map[string]string{"python/py-sibling/Portfile": "name py-sibling\nversion 2\n"})
	p := newPlanner(t)
	e.Ports.Archives = p
	usePorts(e, fakePorts{directories: map[string][]macports.PortInfo{"python/py-sibling": {{Name: "py310-sibling"}, {Name: "py313-sibling"}}},
		trees: map[model.ObjectID]map[string][]macports.PortInfo{base: {"python/py-sibling": {{Name: "py310-sibling"}}}}})
	port := func(python, version string) macports.PortInfo {
		return macports.PortInfo{Name: "py" + python + "-sibling", Version: version, Options: map[string]string{"dockhand.portgroups": "python", "license": "MIT"}}
	}
	p.Add(base, plannedPort{Info: port("310", "1"), Archives: map[string]map[string]string{"sibling-1.tar.gz": {"LICENSE": "MIT\n"}}})
	for _, python := range []string{"310", "313"} {
		p.Add(tree, plannedPort{Info: port(python, "2"), Archives: map[string]map[string]string{"sibling-2.tar.gz": {"LICENSE": "MIT\n", "NOTICE": "Apache-2.0 parts\n"}}})
	}
	assessments, err := e.revisionAssessments(t.Context(), branch.ID, branch.Base, tree, true)
	require.NoError(t, err)
	byPort := map[string]model.Assessment{}
	for _, a := range assessments {
		byPort[a.Port] = a
	}
	require.NotEmpty(t, messagesOf(byPort["py310-sibling"].Comparison))
	require.Equal(t, messagesOf(byPort["py310-sibling"].Comparison), messagesOf(byPort["py313-sibling"].Comparison), "compared with py310-sibling's base")
}

// A Git-fetched port whose source can't be read isn't compared, which
// holds as what couldn't be checked does (D4); a port that fetches no
// source has nothing to compare, and says so without holding.
func TestWhatCantBeComparedSaysSo(t *testing.T) {
	t.Parallel()
	e, branch, base, tree := revisionFixture(t, map[string]string{"devel/gitty/Portfile": "name gitty\n", "devel/meta/Portfile": "name meta\n", "python/py-stub/Portfile": "name py-stub\n"})
	p := newPlanner(t)
	e.Ports.Archives = p
	usePorts(e, fakePorts{directories: map[string][]macports.PortInfo{"devel/gitty": {{Name: "gitty"}}, "devel/meta": {{Name: "meta"}}, "python/py-stub": {{Name: "py-stub"}}}})
	for _, tree := range []model.ObjectID{base, tree} {
		p.Add(tree, plannedPort{Info: macports.PortInfo{Name: "gitty", Options: map[string]string{"fetch.type": "git"}}})
		p.Add(tree, plannedPort{Info: macports.PortInfo{Name: "meta"}, NoSource: true})
		p.Add(tree, plannedPort{Info: macports.PortInfo{Name: "py-stub", Options: map[string]string{"dockhand.metadata_only": "1"}}, NoSource: true})
	}
	assessments, err := e.revisionAssessments(t.Context(), branch.ID, branch.Base, tree, true)
	require.NoError(t, err)
	byPort := map[string]model.Assessment{}
	for _, a := range assessments {
		byPort[a.Port] = a
	}
	require.Equal(t, []string{"the upstream archives couldn't be compared: the revision's Git source couldn't be read: macports: fetched with Git, and git.url names no repository"}, holds(byPort["gitty"]))
	_, state, err := e.assessedHolds(t.Context(), branch, tree, []string{"devel/gitty", "devel/meta"})
	require.NoError(t, err)
	require.Equal(t, AssessmentIncomplete, state)
	require.Empty(t, holds(byPort["meta"]))
	require.Equal(t, []model.Coverage{{Path: "devel/meta", Relevance: "unknown", Treatment: "inspected", Policy: comparison.PolicyNotCompared, Reason: "meta fetches no upstream source, so there's nothing to compare"}}, byPort["meta"].Comparison.Coverage)
	require.True(t, NothingCompared(byPort["meta"].Comparison))
	// A python stub's subports are compared, which it says rather than
	// read as a port with no source (the Vx port's field testing).
	require.Equal(t, "py-stub builds nothing itself, so its subports' source is what's compared", byPort["py-stub"].Comparison.Coverage[0].Reason)
}

// What's recorded for a revision stands: it's read, not made again. One
// recorded under other rules, for another tree, or for another base
// doesn't stand for it, and is made again; a port the revision removed
// isn't assessed.
func TestWhatsRecordedForARevisionStands(t *testing.T) {
	t.Parallel()
	e, branch, base, tree := revisionFixture(t, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\n"})
	p := newPlanner(t)
	e.Ports.Archives = p
	usePorts(e, fakePorts{directories: map[string][]macports.PortInfo{"textproc/jq": {{Name: "jq"}}}})
	p.Add(base, plannedPort{Info: macports.PortInfo{Name: "jq", Version: "1.7.1"}, Archives: map[string]map[string]string{"jq-1.7.1.tar.gz": {"LICENSE": "MIT\n"}}})
	p.Add(tree, plannedPort{Info: macports.PortInfo{Name: "jq", Version: "1.8.1"}, Archives: map[string]map[string]string{"jq-1.8.1.tar.gz": {"LICENSE": "MIT\n"}}})
	marker := model.UpstreamComparison{Changes: []model.UpstreamChange{{Kind: "license", Path: "LICENSE", Message: "recorded", Hold: true}}}
	record := func(a model.Assessment) {
		require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error { return tx.RecordAssessment(a) }))
	}
	record(model.Assessment{Branch: branch.ID, Tree: tree, Base: branch.Base, Port: "jq", Directory: "textproc/jq", Comparison: marker, Policy: assess.Policy, At: at})
	assessments, err := e.revisionAssessments(t.Context(), branch.ID, branch.Base, tree, true)
	require.NoError(t, err)
	require.Len(t, assessments, 1)
	require.Equal(t, marker, assessments[0].Comparison, "what's recorded is read")
	require.Zero(t, p.Fetches.Load())

	for _, stale := range []model.Assessment{
		{Branch: branch.ID, Tree: tree, Base: branch.Base, Port: "jq", Directory: "textproc/jq", Comparison: marker, Policy: assess.Policy + 1, At: at},
	} {
		other := editTree(t, e, tree, map[string]string{"textproc/jq/files/a.diff": fmt.Sprint(stale.Policy)})
		p.Same(other, tree)
		stale.Tree = other
		record(stale)
		assessments, err := e.revisionAssessments(t.Context(), branch.ID, branch.Base, other, true)
		require.NoError(t, err)
		require.Len(t, assessments, 1)
		require.NotEqual(t, marker, assessments[0].Comparison, "one made under other rules is made again")
		require.Equal(t, other, assessments[0].Tree)
	}

	// A later tree gets its own, though an earlier one's is recorded.
	later := editTree(t, e, tree, map[string]string{"textproc/jq/files/b.diff": "b"})
	p.Same(later, tree)
	assessments, err = e.revisionAssessments(t.Context(), branch.ID, branch.Base, later, true)
	require.NoError(t, err)
	require.Equal(t, later, assessments[0].Tree)
	require.NotEqual(t, marker, assessments[0].Comparison)

	// A port the revision removes isn't assessed.
	removed := editTree(t, e, base, nil)
	file, _, err := e.Repo.File(t.Context(), string(base), "devel/libharbor/Portfile")
	require.NoError(t, err)
	gone, err := e.Repo.EditTree(t.Context(), string(removed), []git.FileEdit{{Path: "devel/libharbor/Portfile", Before: file, Delete: true}})
	require.NoError(t, err)
	assessments, err = e.revisionAssessments(t.Context(), branch.ID, branch.Base, model.ObjectID(gone), true)
	require.NoError(t, err)
	require.Empty(t, assessments)
}

// An archive both versions name alike is itself on each side, however
// the plans order the rest: jq's manual keeps its name across versions,
// named first in the base's plan and last in the revision's, and the
// source archives pair with each other.
func TestAlikeArchivesPairByName(t *testing.T) {
	t.Parallel()
	e, branch, base, tree := revisionFixture(t, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\n"})
	p := newPlanner(t)
	e.Ports.Archives = p
	usePorts(e, fakePorts{directories: map[string][]macports.PortInfo{"textproc/jq": {{Name: "jq"}}}})
	manual := map[string]string{"LICENSE": "the manual's license\n"}
	p.Add(base, plannedPort{Info: macports.PortInfo{Name: "jq", Version: "1.7.1"}, Archives: map[string]map[string]string{
		"jq-1.7.1.tar.gz": {"LICENSE": "MIT\n"}, "jq-manual.tar.gz": manual}})
	p.Add(tree, plannedPort{Info: macports.PortInfo{Name: "jq", Version: "1.8.1"}, Archives: map[string]map[string]string{
		"jqz-1.8.1.tar.gz": {"LICENSE": "MIT\n"}, "jq-manual.tar.gz": manual}})
	assessments, err := e.revisionAssessments(t.Context(), branch.ID, branch.Base, tree, true)
	require.NoError(t, err)
	require.Len(t, assessments, 1)
	require.Empty(t, messagesOf(assessments[0].Comparison), "the manual beside itself, the source beside the source: nothing changed")
}

// commitArchives stand in for a forge's archives of commits, each commit's
// files as given, counting what's asked of them.
type commitArchives struct {
	t     *testing.T
	files map[string]map[string]string
	asked *atomic.Int64
}

func (c commitArchives) SourceArchive(_ context.Context, _ macports.PortInfo, source model.GitSource, directory string) (string, error) {
	c.asked.Add(1)
	commit := string(source.Commit)
	files, ok := c.files[commit]
	if !ok {
		return "", fmt.Errorf("no archive of %s", commit)
	}
	return writeTarball(c.t, directory, "owner-tool-"+commit[:7], files), nil
}

// A Git-fetched port is compared through its forge's archive of the commit
// each version's git.branch names, resolved as a clone would, and said to
// be read so; a reading of a commit is kept by the commit, so the forge is
// asked nothing again. A git.branch no ref is can't be compared, and holds.
func TestAGitFetchedPortIsComparedByItsCommits(t *testing.T) {
	t.Parallel()
	project := t.TempDir()
	testsupport.Git(t, project, "init", "-q")
	write(t, project, map[string]string{"README": "1\n"})
	testsupport.Git(t, project, "add", "-A")
	testsupport.Git(t, project, "commit", "-q", "-m", "one")
	testsupport.Git(t, project, "tag", "v1")
	write(t, project, map[string]string{"README": "2\n"})
	testsupport.Git(t, project, "commit", "-q", "-am", "two")
	testsupport.Git(t, project, "tag", "v2")
	v1, v2 := testsupport.Git(t, project, "rev-parse", "v1^{commit}"), testsupport.Git(t, project, "rev-parse", "v2^{commit}")

	e, branch, base, tree := revisionFixture(t, map[string]string{"devel/libharbor/Portfile": "name libharbor\nversion 3\n"})
	p := newPlanner(t)
	e.Ports.Archives = p
	usePorts(e, fakePorts{directories: map[string][]macports.PortInfo{"devel/libharbor": {{Name: "libharbor"}}}})
	archives := commitArchives{t: t, files: map[string]map[string]string{v1: {"LICENSE": "MIT\n"}, v2: {"LICENSE": "GPL\n"}}, asked: &atomic.Int64{}}
	e.SourceArchiver = archives
	tool := func(ref string) plannedPort {
		return plannedPort{Info: macports.PortInfo{Name: "libharbor", Options: map[string]string{"fetch.type": "git", "git.url": project, "git.branch": ref}}}
	}
	p.Add(base, tool("v1"))
	p.Add(tree, tool("v2"))
	assessments, err := e.revisionAssessments(t.Context(), branch.ID, branch.Base, tree, true)
	require.NoError(t, err)
	require.Len(t, assessments, 1)
	require.Empty(t, assessments[0].Comparison.Problem)
	require.Equal(t, []string{"upstream's LICENSE changed; the Portfile's license line may need to follow"}, holds(assessments[0]))
	require.Equal(t, []model.Coverage{
		{Path: "LICENSE", Relevance: "used", Treatment: "inspected", Policy: "read", Reason: "compared with the base's"},
		{Path: v2, Relevance: "unknown", Treatment: "inspected",
			Reason: "read from the forge's archive of each commit, submodules left out; the base's git.branch as it names a commit now"}}, assessments[0].Comparison.Coverage)
	require.Equal(t, v2, assessments[0].Comparison.Commit, "the commit it read is kept, for submit to compare with what a check built")
	require.EqualValues(t, 2, archives.asked.Load())

	later := editTree(t, e, tree, map[string]string{"devel/libharbor/files/a.diff": "a"})
	p.Same(later, tree)
	_, err = e.revisionAssessments(t.Context(), branch.ID, branch.Base, later, true)
	require.NoError(t, err)
	require.EqualValues(t, 2, archives.asked.Load(), "the forge asked nothing again")

	moved := editTree(t, e, tree, map[string]string{"devel/libharbor/files/b.diff": "b"})
	p.Add(moved, tool("v9"))
	assessments, err = e.revisionAssessments(t.Context(), branch.ID, branch.Base, moved, true)
	require.NoError(t, err)
	require.Contains(t, assessments[0].Comparison.Problem, "the revision's git.branch couldn't be resolved")
	require.NotEmpty(t, holds(assessments[0]))
	require.EqualValues(t, refusal.UpstreamMissing, assessments[0].Comparison.ProblemKind, "a ref not there yet, looked at again a day later")

	// A repository that couldn't be read, as a clone that failed, is
	// looked at again with no tree change, and compared once it reads
	// (C8-5: it was Failed, which lookAgain never made again).
	unreadable := filepath.Join(t.TempDir(), "harbor")
	elsewhere := editTree(t, e, tree, map[string]string{"devel/libharbor/files/d.diff": "d"})
	p.Add(elsewhere, plannedPort{Info: macports.PortInfo{Name: "libharbor", Options: map[string]string{"fetch.type": "git", "git.url": unreadable, "git.branch": "v2"}}})
	assessments, err = e.revisionAssessments(t.Context(), branch.ID, branch.Base, elsewhere, true)
	require.NoError(t, err)
	require.Contains(t, assessments[0].Comparison.Problem, "the revision's git.branch couldn't be resolved")
	require.True(t, refusal.Kind(assessments[0].Comparison.ProblemKind).Transient())
	testsupport.Git(t, filepath.Dir(unreadable), "clone", "-q", project, unreadable)
	assessments, err = e.revisionAssessments(t.Context(), branch.ID, branch.Base, elsewhere, true)
	require.NoError(t, err)
	require.Empty(t, assessments[0].Comparison.Problem, "made again, and read")

	abbreviated := editTree(t, e, tree, map[string]string{"devel/libharbor/files/c.diff": "c"})
	p.Add(abbreviated, tool(v2[:9]))
	assessments, err = e.revisionAssessments(t.Context(), branch.ID, branch.Base, abbreviated, true)
	require.NoError(t, err)
	require.Equal(t, "the revision's git.branch is an abbreviated commit, "+v2[:9]+", which only a clone expands", assessments[0].Comparison.Problem)
}

// A Git-fetched port whose tag moved after its check is a concern: the
// check built one source, and the submission would ship another. One
// whose tag still names what was built, or can't be read now, isn't.
func TestASourceMovedSinceItsCheckIsAConcern(t *testing.T) {
	t.Parallel()
	project := t.TempDir()
	testsupport.Git(t, project, "init", "-q")
	write(t, project, map[string]string{"README": "1\n"})
	testsupport.Git(t, project, "add", "-A")
	testsupport.Git(t, project, "commit", "-q", "-m", "one")
	testsupport.Git(t, project, "tag", "v2")
	built := testsupport.Git(t, project, "rev-parse", "HEAD")
	f := setup(t)
	e := f.open(t)
	target := model.PlanTarget{ID: "tool", Target: model.Target{Name: "tool"}}
	evidence := &Evidence{Run: model.Run{Number: 7}, Plan: model.Plan{Targets: []model.PlanTarget{target}, Builds: []model.EnvironmentPlan{{
		Order: []model.TargetID{"tool"}, Git: map[model.TargetID]model.GitSource{"tool": {URL: project, Ref: "v2", Commit: model.ObjectID(built)}}}}}}
	require.Empty(t, e.movedSources(t.Context(), evidence))

	write(t, project, map[string]string{"README": "2\n"})
	testsupport.Git(t, project, "commit", "-q", "-am", "two")
	testsupport.Git(t, project, "tag", "-f", "v2")
	now := testsupport.Git(t, project, "rev-parse", "HEAD")
	moved := e.movedSources(t.Context(), evidence)
	require.Len(t, moved, 1)
	require.Equal(t, model.Concern{Origin: model.FromUpstream, Port: "tool", Rule: "source-moved", Subject: now, Class: model.Introduced,
		Detail: "tool: git.branch v2 pointed to " + built[:7] + " when check-7 was planned, but now points to " + now[:7] + ". The checked source differs from the source this submission would use"}, moved[0])
	require.Contains(t, HoldLines(SubmitPlan{Moved: moved}.Held()), moved[0].Detail, "it holds a submission nobody looks over")
	// A target --only left out of the check is read the same way.
	omitted := &Evidence{Run: model.Run{Number: 8}, Plan: model.Plan{Omitted: []model.PlanTarget{target},
		Builds: []model.EnvironmentPlan{{Git: map[model.TargetID]model.GitSource{"tool": {URL: project, Ref: "v2", Commit: model.ObjectID(built)}}}}}}
	moved = e.movedSources(t.Context(), omitted)
	require.Len(t, moved, 1)
	require.Equal(t, "tool: git.branch v2 pointed to "+built[:7]+" when check-8 was planned, but now points to "+now[:7]+". The checked source differs from the source this submission would use", moved[0].Detail)

	evidence.Plan.Builds[0].Git["tool"] = model.GitSource{URL: t.TempDir() + "/gone", Ref: "v2", Commit: model.ObjectID(built)}
	// A ref that can't be read now isn't said to have moved, but its
	// freshness is unknown, which holds serve (C8-5's gap, the person's
	// word, 2026-10-23), and a person's submission shows it.
	unknown := e.movedSources(t.Context(), evidence)
	require.Len(t, unknown, 1)
	require.Equal(t, "source-unknown", unknown[0].Rule)
	require.Equal(t, model.UnknownBaseline, unknown[0].Class)
	require.Contains(t, unknown[0].Detail, "tool: whether git.branch v2 still points to "+built[:7]+", as when check-7 was planned, isn't known: ")
	require.Contains(t, HoldLines(SubmitPlan{Moved: unknown}.Held()), unknown[0].Detail, "it holds a submission nobody looks over")
	require.NotContains(t, HoldRules(), "source-unknown", "what couldn't be checked holds whatever holds.allow says (D4)")
	require.Empty(t, e.movedSources(t.Context(), nil))
}

// An assessment that stopped short for what another try may not meet, a
// server failing, upstream's or the mirror's where upstream refused, is
// tried again when it's next collected, and finishes;
// one refused, as a 404 refuses, stands for its files, and isn't fetched
// again until a day has passed.
func TestAnAssessmentStoppedByTheNetworkIsTriedAgain(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		status, mirror int
		again          bool
	}{{http.StatusServiceUnavailable, 0, true}, {http.StatusNotFound, 0, false}, {http.StatusNotFound, http.StatusBadGateway, true}} {
		e, branch, base, tree := revisionFixture(t, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\n"})
		p := newPlanner(t)
		e.Ports.Archives = p
		e.ArchiveMirror = p.Server.URL + "/mirror/"
		usePorts(e, fakePorts{directories: map[string][]macports.PortInfo{"textproc/jq": {{Name: "jq"}}}})
		p.Add(base, plannedPort{Info: macports.PortInfo{Name: "jq", Version: "1.7.1"}, Archives: map[string]map[string]string{"jq-1.7.1.tar.gz": {"LICENSE": "MIT\n"}}})
		p.Add(tree, plannedPort{Info: macports.PortInfo{Name: "jq", Version: "1.8.1"}, Archives: map[string]map[string]string{"jq-1.8.1.tar.gz": {"LICENSE": "MIT\n"}}})
		p.Refusing.Store(int64(test.status))
		p.Mirror.Store(int64(test.mirror))
		assessments, err := e.revisionAssessments(t.Context(), branch.ID, branch.Base, tree, true)
		require.NoError(t, err)
		require.Contains(t, assessments[0].Comparison.Problem, "archives couldn't be fetched", test.status)
		require.Equal(t, test.again, refusal.Kind(assessments[0].Comparison.ProblemKind).Transient(), test.status)
		require.NotEmpty(t, holds(assessments[0]), "what couldn't be checked holds (D4)")

		p.Refusing.Store(0)
		p.Mirror.Store(0)
		assessments, err = e.revisionAssessments(t.Context(), branch.ID, branch.Base, tree, true)
		require.NoError(t, err)
		if test.again {
			require.Empty(t, assessments[0].Comparison.Problem, "tried again, and finished")
			require.False(t, refusal.Kind(assessments[0].Comparison.ProblemKind).Transient())
		} else {
			require.Contains(t, assessments[0].Comparison.Problem, "archives couldn't be fetched", "a refusal stands for its files")
			require.Zero(t, p.Fetches.Load())
			// A day later it's fetched again, and finishes, the archive
			// served again (v0.4.0's known issues).
			later := time.Now().Add(refusedAgainAfter + time.Minute)
			e.options.Now = func() time.Time { return later }
			assessments, err = e.revisionAssessments(t.Context(), branch.ID, branch.Base, tree, true)
			require.NoError(t, err)
			require.Empty(t, assessments[0].Comparison.Problem, "fetched again a day later, and finished")
		}
	}
}

// A port that declares its crates is compared through its own archives,
// planned from its Portfile with the crates set aside, as a checksum
// refresh sets them aside: they're its Cargo.lock's, which the comparison
// reads, and MacPorts' fetch of them isn't one dockhand checks. One
// fetched with Git plans none, and is compared by its commits.
func TestAVendoredPortIsComparedByItsOwnArchives(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.options.Tclsh = testsupport.MacPortsTclsh(t)
	e := f.open(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "vendored"})
	require.NoError(t, err)
	trees, err := e.Repo.CommitTrees(t.Context(), []string{string(branch.Base)})
	require.NoError(t, err)
	sum := strings.Repeat("a", 64)
	port := func(name, fetch string) string {
		return "PortSystem 1.0\nPortGroup cargo_fetch 1.0\nname " + name + "\nversion 1.0\ncategories devel\nlicense MIT\nmaintainers nomaintainer\n" +
			"homepage https://example.invalid\ndescription demo\nlong_description demo\n" + fetch +
			"cargo.crates \\\n    anyhow 1.0.0 " + sum + " \\\n    serde 1.0.1 " + sum + "\n"
	}
	tree := editTree(t, e, model.ObjectID(trees[string(branch.Base)]), map[string]string{
		// A stand-in for MacPorts' cargo_fetch PortGroup, appending each
		// crate to the port's own archives as it does.
		"_resources/port1.0/group/cargo_fetch-1.0.tcl": `options cargo.crates cargo.crates_github
default cargo.crates {}
default cargo.crates_github {}
proc demo_crates {} {
    foreach {cname cversion chksum} [option cargo.crates] {
        distfiles-append ${cname}-${cversion}.crate:crate-${cname}
        master_sites-append https://static.crates.io/crates/${cname}:crate-${cname}
        checksums-append ${cname}-${cversion}.crate sha256 ${chksum}
    }
}
port::register_callback demo_crates
`,
		"devel/zdemo/Portfile": port("zdemo", "master_sites https://example.invalid/releases\nchecksums sha256 "+sum+" size 10\n"),
		// One with a patch of its own, read in its files/ (the rust and
		// cargo run, whose nine patches read "local patch directory is
		// unavailable").
		"devel/zpatched/Portfile":               port("zpatched", "master_sites https://example.invalid/releases\nchecksums sha256 "+sum+" size 10\npatchfiles patch-build.diff\n"),
		"devel/zpatched/files/patch-build.diff": "--- a/build\n+++ b/build\n",
		"devel/zgit/Portfile":                   port("zgit", "fetch.type git\ngit.url https://example.invalid/zgit.git\ngit.branch "+strings.Repeat("b", 40)+"\n"),
	})
	planner, err := e.archivePlanner()
	require.NoError(t, err)

	info, plan, err := planner.ArchivePlan(t.Context(), model.Source{Tree: tree}, "devel/zdemo", "")
	require.NoError(t, err)
	require.Len(t, plan, 1)
	require.Equal(t, "zdemo-1.0.tar.gz", plan[0].Name)
	require.Contains(t, info.Options["cargo.crates"], "anyhow", "the port is as it is, crates and all")
	require.Contains(t, info.Options["checksums"], "anyhow-1.0.0.crate")

	_, plan, err = planner.ArchivePlan(t.Context(), model.Source{Tree: tree}, "devel/zpatched", "")
	require.NoError(t, err)
	require.Equal(t, "zpatched-1.0.tar.gz", plan[0].Name)

	_, _, err = planner.ArchivePlan(t.Context(), model.Source{Tree: tree}, "devel/zgit", "")
	require.ErrorIs(t, err, treequery.ErrNoArchives)
}

// What an assessment wants of the ports is observed in the tree: a port
// that's there by its version and directory, one the tree hasn't got as
// absent, which a native library with no port is set apart for, and not
// as a problem.
func TestAPortTheTreeHasntGotIsObservedAbsent(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	usePorts(e, fakePorts{directories: map[string][]macports.PortInfo{"archivers/zstd": {{Name: "zstd", Version: "1.5.7"}}}})
	observer := e.collector(nil).Observer
	tree := model.Source{Tree: "candidate"}
	require.Equal(t, assess.Observation{Version: "1.5.7", Directory: "archivers/zstd"}, observer.Observe(t.Context(), tree, "zstd"))
	require.Equal(t, assess.Observation{Absent: true}, observer.Observe(t.Context(), tree, "aws-lc"))
}

// A revision's patches are checked against its source, as an update's
// are: one that no longer applies is said, and one that does isn't
// (the libuv run's finding 2).
func TestARevisionsPatchesAreCheckedAgainstItsSource(t *testing.T) {
	t.Parallel()
	kept := "--- src/main.c\n+++ src/main.c\n@@ -1 +1 @@\n-int main;\n+int main(void);\n"
	stale := "--- src/gone.c\n+++ src/gone.c\n@@ -1 +1 @@\n-old\n+new\n"
	e, branch, base, tree := revisionFixture(t, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\n", "textproc/jq/files/patch-kept.diff": kept, "textproc/jq/files/patch-stale.diff": stale})
	p := newPlanner(t)
	e.Ports.Archives = p
	usePorts(e, fakePorts{directories: map[string][]macports.PortInfo{"textproc/jq": {{Name: "jq"}}}})
	p.Add(base, plannedPort{Info: macports.PortInfo{Name: "jq", Version: "1.7.1"}, Archives: map[string]map[string]string{"jq-1.7.1.tar.gz": {"src/main.c": "int main;\n"}}})
	options := map[string]string{"patchfiles": "patch-kept.diff patch-stale.diff", "worksrcdir": "jq-1.8.1", "patch.pre_args": "-p0", "extract.rename": "0"}
	p.Add(tree, plannedPort{Info: macports.PortInfo{Name: "jq", Version: "1.8.1", Options: options}, Archives: map[string]map[string]string{"jq-1.8.1.tar.gz": {"src/main.c": "int main;\n"}}})
	assessments, err := e.revisionAssessments(t.Context(), branch.ID, branch.Base, tree, true)
	require.NoError(t, err)
	require.Len(t, assessments, 1)
	var patches []model.UpstreamChange
	for _, change := range assessments[0].Comparison.Changes {
		if change.Kind == "patch" {
			patches = append(patches, change)
		}
	}
	require.Len(t, patches, 1, "%+v", assessments[0].Comparison)
	require.Equal(t, assess.PatchRejected, patches[0].Rule)
	require.Equal(t, "patch-stale.diff", patches[0].Subject)
	require.Contains(t, patches[0].Message, "patch-stale.diff doesn't apply to 1.8.1's source, as dockhand read it, and a check shows whether the build's patch phase fails")
	require.True(t, patches[0].Hold, "it holds nobody's run (the docs dig's I9)")
	require.Equal(t, int64(2), p.Fetches.Load(), "each version's archive fetched once, the patch check reading the revision's as fetched")
}

// A subport's fetch plan is its own, asked for by its name: libuv's
// libuv-devel couldn't be assessed, since the directory resolved to libuv
// alone (the batch 11 run on #34620). A name the directory doesn't define
// is ErrNoPort, as a subport new to it is on the base's side.
func TestASubportsArchivesArePlannedForIt(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.options.Tclsh = testsupport.MacPortsTclsh(t)
	e := f.open(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "subported"})
	require.NoError(t, err)
	trees, err := e.Repo.CommitTrees(t.Context(), []string{string(branch.Base)})
	require.NoError(t, err)
	sum := strings.Repeat("a", 64)
	tree := editTree(t, e, model.ObjectID(trees[string(branch.Base)]), map[string]string{
		"devel/zdemo/Portfile": "PortSystem 1.0\nname zdemo\nversion 1.0\ncategories devel\nlicense MIT\nmaintainers nomaintainer\n" +
			"homepage https://example.invalid\ndescription demo\nlong_description demo\nmaster_sites https://example.invalid/releases\n" +
			"checksums sha256 " + sum + " size 10\nsubport zdemo-devel {\n    version 2.0\n}\n",
	})
	planner, err := e.archivePlanner()
	require.NoError(t, err)
	info, plan, err := planner.ArchivePlan(t.Context(), model.Source{Tree: tree}, "devel/zdemo", "zdemo-devel")
	require.NoError(t, err)
	require.Equal(t, "zdemo-devel", info.Name)
	require.Equal(t, "2.0", info.Version)
	require.Len(t, plan, 1)
	require.Equal(t, "zdemo-2.0.tar.gz", plan[0].Name, "distname is ${name}-${version}, the main port's name")
	info, _, err = planner.ArchivePlan(t.Context(), model.Source{Tree: tree}, "devel/zdemo", "zdemo")
	require.NoError(t, err)
	require.Equal(t, "1.0", info.Version)
	_, _, err = planner.ArchivePlan(t.Context(), model.Source{Tree: tree}, "devel/zdemo", "zdemo-nightly")
	require.ErrorIs(t, err, treequery.ErrNoPort)
}

// An archive only the base fetched, with the candidate's each matched, is
// said and set apart, and holds nothing.
func TestAnArchiveNoLongerFetchedIsSaid(t *testing.T) {
	t.Parallel()
	p := newPlanner(t)
	p.Add("base", plannedPort{Info: macports.PortInfo{Name: "demo"}, Archives: map[string]map[string]string{
		"main-1.0.tar.gz": {"LICENSE": "MIT\n"}, "extra-1.0.tar.gz": {"LICENSE": "GPL\n"},
	}})
	p.Add("candidate", plannedPort{Info: macports.PortInfo{Name: "demo"}, Archives: map[string]map[string]string{"main-1.1.tar.gz": {"LICENSE": "MIT\n"}}})
	e := &Engine{}
	e.options.Readings = t.TempDir()
	e.SourceArchiver = stubArchiver{}
	result := e.collector(p).Revision(t.Context(), [2]model.Source{{Tree: "base"}, {Tree: "candidate"}}, "devel/demo", [2]string{"demo", "demo"}, true, nil)
	require.Empty(t, result.Problem)
	require.True(t, result.Held(), "an archive no longer fetched holds for a look (L2a)")
	require.Contains(t, result.Changes, model.UpstreamChange{Kind: "source", Path: "extra-1.0.tar.gz", Message: "upstream: extra-1.0.tar.gz is no longer fetched; look at what the port used of it, and at its checksum lines in the Portfile",
		Rule: assess.SourceRemoved, Hold: true, Class: model.Introduced, Source: "extra-*.tar.gz"})
	require.Contains(t, CoverageWords(result), "not compared: extra-1.0.tar.gz")
	require.Contains(t, CoverageWords(result), "Compared LICENSE", "the one archive compared reads as a port of one archive's")
}

// An update of a Git-fetched port compares its commits, as a revision's
// assessment does, and says it read them so: semgrep's update compared
// nothing and said nothing (field testing, 2026-10-02).
func TestAnUpdateOfAGitFetchedPortComparesItsCommits(t *testing.T) {
	t.Parallel()
	project := t.TempDir()
	testsupport.Git(t, project, "init", "-q")
	write(t, project, map[string]string{"README": "1\n"})
	testsupport.Git(t, project, "add", "-A")
	testsupport.Git(t, project, "commit", "-q", "-m", "one")
	testsupport.Git(t, project, "tag", "v1")
	write(t, project, map[string]string{"README": "2\n"})
	testsupport.Git(t, project, "commit", "-q", "-am", "two")
	testsupport.Git(t, project, "tag", "v2")
	v1, v2 := testsupport.Git(t, project, "rev-parse", "v1^{commit}"), testsupport.Git(t, project, "rev-parse", "v2^{commit}")

	e, _, base, tree := revisionFixture(t, map[string]string{"devel/libharbor/Portfile": "name libharbor\nversion 3\n"})
	e.SourceArchiver = commitArchives{t: t, files: map[string]map[string]string{v1: {"LICENSE": "MIT\n"}, v2: {"LICENSE": "GPL\n"}}, asked: &atomic.Int64{}}
	port := func(version, ref string) macports.Snapshot {
		return macports.Snapshot{Ports: map[string]macports.PortInfo{"libharbor": {Name: "libharbor", Version: version, Options: map[string]string{"fetch.type": "git", "git.url": project, "git.branch": ref}}}}
	}
	result := editprep.Result{Target: model.Target{Name: "libharbor", Portfile: "devel/libharbor/Portfile"}, Fidelity: []portedit.Fidelity{{Before: port("1", "v1"), After: port("2", "v2")}}}
	trees := [2]model.Source{{Tree: base, Base: base}, {Tree: tree, Base: base}}
	comparison := e.assessUpstream(t.Context(), result, sourcecompare.Versions{Old: "1", New: "2"}, trees, true)
	require.NotNil(t, comparison)
	require.Empty(t, comparison.Problem)
	require.Equal(t, []string{"upstream's LICENSE changed; the Portfile's license line may need to follow"}, comparison.Holds())
	require.Equal(t, v2, comparison.Commit)
	require.Equal(t, "Compared LICENSE; read from the forge's archive of each commit, submodules left out; the base's git.branch as it names a commit now", CoverageWords(*comparison))
}

// A subport whose source the change left as the base had it says nothing
// of upstream, and the comparisons submit and review show leave it out:
// terraform-1.16's update listed nine unchanged subports' comparisons
// (field testing, 2026-10-02).
func TestASubportWhoseSourceDidntChangeIsLeftOut(t *testing.T) {
	t.Parallel()
	e, branch, base, tree := revisionFixture(t, map[string]string{"devel/libharbor/Portfile": "name libharbor\nversion 3\nsubport tool-2 {}\n"})
	p := newPlanner(t)
	e.Ports.Archives = p
	usePorts(e, fakePorts{directories: map[string][]macports.PortInfo{"devel/libharbor": {{Name: "tool-1"}, {Name: "tool-2"}}}})
	one := plannedPort{Info: macports.PortInfo{Name: "tool-1", Version: "1.0"}, Archives: map[string]map[string]string{"tool-1.0.tar.gz": {"LICENSE": "MIT\n"}}}
	p.Add(base, one)
	p.Add(tree, one)
	p.Add(base, plannedPort{Info: macports.PortInfo{Name: "tool-2", Version: "2.0"}, Archives: map[string]map[string]string{"tool-2.0.tar.gz": {"LICENSE": "MIT\n"}}})
	p.Add(tree, plannedPort{Info: macports.PortInfo{Name: "tool-2", Version: "2.1"}, Archives: map[string]map[string]string{"tool-2.1.tar.gz": {"LICENSE": "MIT\n"}}})
	assessments, err := e.revisionAssessments(t.Context(), branch.ID, branch.Base, tree, true)
	require.NoError(t, err)
	require.Len(t, assessments, 2)
	byPort := map[string]model.Assessment{}
	for _, a := range assessments {
		byPort[a.Port] = a
	}
	require.True(t, SourceUnchanged(byPort["tool-1"].Comparison), "%+v", byPort["tool-1"].Comparison)
	require.False(t, SourceUnchanged(byPort["tool-2"].Comparison))
	comparisons, err := e.revisionComparisons(t.Context(), branch, tree, true)
	require.NoError(t, err)
	require.Len(t, comparisons, 1)
	require.Equal(t, "tool-2", comparisons[0].Port)
}

// A crate linking a library MacPorts has no port for is set apart by its
// lock, which was compared: it's said by the crate, where "compared
// Cargo.lock" and "not compared: Cargo.lock" said both (field testing,
// hyperfine 2.0.0).
func TestALibraryWithNoPortIsSaidByItsCrate(t *testing.T) {
	t.Parallel()
	comparison := model.UpstreamComparison{Coverage: []model.Coverage{
		{Path: "Cargo.lock", Relevance: "used", Treatment: "inspected", Policy: "read"},
		{Path: "Cargo.toml", Relevance: "used", Treatment: "inspected", Policy: "read"},
		{Path: "Cargo.lock", Relevance: "unknown", Treatment: "set-apart", Policy: "native-library-ports", Reason: "mimalloc-sys links mimalloc, which MacPorts has no port for"},
	}}
	require.Equal(t, "Compared Cargo.lock, Cargo.toml; set apart, with no port to declare: mimalloc-sys links mimalloc, which MacPorts has no port for", CoverageWords(comparison))
}

// An incomplete assessment another build made is made again, where a
// refusal a newer dockhand reads stood until the tree moved; one this
// build made stands, and so does a complete one another build made (the
// docs dig's I26). A record from before schema 42, with no build, reads
// as another build's.
func TestAnIncompleteAssessmentIsMadeAgainByANewerBuild(t *testing.T) {
	t.Parallel()
	e := &Engine{}
	incomplete := model.Assessment{At: time.Now(), Comparison: model.UpstreamComparison{Problem: "couldn't compare: an archive kind dockhand doesn't read", ProblemKind: string(refusal.Unsupported)}}
	incomplete.Build = assessmentBuild()
	require.False(t, e.lookAgain(incomplete), "this build's stands")
	incomplete.Build = "v0.6.0-rc5"
	require.True(t, e.lookAgain(incomplete), "another build's is made again")
	incomplete.Build = ""
	require.True(t, e.lookAgain(incomplete), "one recorded before builds were")
	complete := model.Assessment{At: time.Now(), Build: "v0.6.0-rc5"}
	require.False(t, e.lookAgain(complete), "a complete one stands across builds")
}

// stubArchiver archives nothing, for a collector whose ports fetch no Git
// source.
type stubArchiver struct{}

func (stubArchiver) SourceArchive(context.Context, macports.PortInfo, model.GitSource, string) (string, error) {
	return "", errors.New("no Git source here")
}
