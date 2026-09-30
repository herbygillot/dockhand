package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/assess"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// plannedPort is a port as a fake planner has it in one tree: as
// evaluated, and its archives by name, each file's contents by path below
// the archive's top; or a port fetched with Git, or with no source.
type plannedPort struct {
	info     macports.PortInfo
	archives map[string]map[string]string
	noSource bool
}

// fakePlanner stands in for MacPorts' fetch plans, serving each archive
// its ports name over HTTP, and counting what's fetched.
type fakePlanner struct {
	t       *testing.T
	ports   map[model.ObjectID]map[string]plannedPort
	server  *httptest.Server
	served  map[string][]byte
	fetches *atomic.Int64
}

func newPlanner(t *testing.T) *fakePlanner {
	p := &fakePlanner{t: t, ports: map[model.ObjectID]map[string]plannedPort{}, served: map[string][]byte{}, fetches: &atomic.Int64{}}
	p.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, ok := p.served[strings.TrimPrefix(r.URL.Path, "/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		p.fetches.Add(1)
		_, _ = w.Write(data)
	}))
	t.Cleanup(p.server.Close)
	return p
}

// add plans a port in a tree.
func (p *fakePlanner) add(tree model.ObjectID, port plannedPort) {
	if p.ports[tree] == nil {
		p.ports[tree] = map[string]plannedPort{}
	}
	var checksums []string
	for _, name := range slices.Sorted(maps.Keys(port.archives)) {
		path := writeTarball(p.t, p.t.TempDir(), strings.TrimSuffix(name, ".tar.gz"), port.archives[name])
		data, err := os.ReadFile(path)
		require.NoError(p.t, err)
		p.served[name] = data
		sum := sha256.Sum256(data)
		checksums = append(checksums, fmt.Sprintf("%s sha256 %s size %d", name, hex.EncodeToString(sum[:]), len(data)))
	}
	options := map[string]string{"fetch.type": "standard", "fetch.archive_compatible": "1", "fetch.ignore_sslcert": "0", "fetch.has_credentials": "0"}
	maps.Copy(options, port.info.Options)
	options["checksums"] = strings.Join(checksums, " ")
	port.info.Options = options
	p.ports[tree][port.info.Name] = port
}

// same plans a tree's ports as another tree's, the same archives served.
func (p *fakePlanner) same(tree, as model.ObjectID) {
	p.ports[tree] = p.ports[as]
}

func (p *fakePlanner) ArchivePlan(_ context.Context, source model.Source, directory, name string) (macports.PortInfo, []macports.Distfile, error) {
	port, ok := p.ports[source.Tree][name]
	if !ok {
		return macports.PortInfo{}, nil, fmt.Errorf("%w: %s defines no port %s", ErrNoPort, directory, name)
	}
	if port.noSource || port.info.GitFetched() {
		return port.info, nil, ErrNoArchives
	}
	var plan []macports.Distfile
	for _, name := range slices.Sorted(maps.Keys(port.archives)) {
		plan = append(plan, macports.Distfile{Name: name, URLs: []string{p.server.URL + "/" + name}})
	}
	return port.info, plan, nil
}

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
	e, branch, base, tree := revisionFixture(t, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\n"})
	p := newPlanner(t)
	e.ArchivePlanner = p
	e.PortReader = fakePorts{directories: map[string][]macports.PortInfo{"textproc/jq": {{Name: "jq"}}}}
	p.add(base, plannedPort{info: macports.PortInfo{Name: "jq", Version: "1.7.1"}, archives: map[string]map[string]string{"jq-1.7.1.tar.gz": {"LICENSE": "MIT\n"}}})
	p.add(tree, plannedPort{info: macports.PortInfo{Name: "jq", Version: "1.8.1"}, archives: map[string]map[string]string{"jq-1.8.1.tar.gz": {"LICENSE": "MIT\n", "meson.build": "project('jq')\n"}}})
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
	require.Zero(t, p.fetches.Load())

	assessments, err := e.revisionAssessments(t.Context(), branch.ID, branch.Base, tree, true)
	require.NoError(t, err)
	require.Len(t, assessments, 1)
	require.Equal(t, "jq", assessments[0].Port)
	require.Equal(t, "textproc/jq", assessments[0].Directory)
	require.Equal(t, []string{"upstream's meson.build is new; the build may need the Portfile to follow"}, messagesOf(assessments[0].Comparison),
		"the license is MIT at both ends, and holds nothing; what did change is said")
	require.EqualValues(t, 2, p.fetches.Load())

	held, state, err = e.assessedHolds(t.Context(), branch, tree, []string{"textproc/jq"})
	require.NoError(t, err)
	require.Equal(t, AssessmentAvailable, state)
	require.Equal(t, []string{"upstream's meson.build is new; the build may need the Portfile to follow"}, held)

	// A later revision that changes an unrelated file reads what was kept.
	later := editTree(t, e, tree, map[string]string{"textproc/jq/files/patch-a.diff": "--- a\n"})
	p.same(later, tree)
	assessments, err = e.revisionAssessments(t.Context(), branch.ID, branch.Base, later, true)
	require.NoError(t, err)
	require.Len(t, assessments, 1)
	require.EqualValues(t, 2, p.fetches.Load(), "nothing fetched again")
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
// with nothing said of a missing old archive (the design's fixture).
func TestANewPortIsAssessedAlone(t *testing.T) {
	e, branch, _, tree := revisionFixture(t, map[string]string{"sysutils/rift/Portfile": "name rift\nversion 0.4.2\n"})
	p := newPlanner(t)
	e.ArchivePlanner = p
	e.PortReader = fakePorts{directories: map[string][]macports.PortInfo{"sysutils/rift": {{Name: "rift"}}}}
	p.add(tree, plannedPort{info: macports.PortInfo{Name: "rift", Version: "0.4.2"}, archives: map[string]map[string]string{"rift-0.4.2.tar.gz": {"LICENSE": "MIT\n"}}})
	assessments, err := e.revisionAssessments(t.Context(), branch.ID, branch.Base, tree, true)
	require.NoError(t, err)
	require.Len(t, assessments, 1)
	require.Empty(t, assessments[0].Comparison.Problem)
	require.Equal(t, []string{"upstream's LICENSE was added; the Portfile's license line may need to follow"}, messagesOf(assessments[0].Comparison))
}

// Each subport is assessed for itself: a requirement that applies only to
// Pythons before 3.13 asks something of py312-demo's provider, and nothing
// of py313-demo's (the design's fixture).
func TestEachSubportIsAssessedForItself(t *testing.T) {
	e, branch, base, tree := revisionFixture(t, map[string]string{"python/py-demo/Portfile": "name py-demo\nversion 2\n"})
	p := newPlanner(t)
	e.ArchivePlanner = p
	e.PortReader = fakePorts{directories: map[string][]macports.PortInfo{
		"python/py-demo":  {{Name: "py312-demo"}, {Name: "py313-demo"}},
		"python/py-tomli": {{Name: "py312-tomli", Version: "1.0"}, {Name: "py313-tomli", Version: "1.0"}},
	}}
	for _, python := range []string{"312", "313"} {
		port := func(version string) macports.PortInfo {
			return macports.PortInfo{Name: "py" + python + "-demo", Version: version, Options: map[string]string{"dockhand.portgroups": "python"},
				Dependencies: []macports.Dependency{{Port: "py" + python + "-tomli"}}}
		}
		p.add(base, plannedPort{info: port("1"), archives: map[string]map[string]string{"demo-1.tar.gz": {"requirements.txt": "click>=8\n"}}})
		p.add(tree, plannedPort{info: port("2"), archives: map[string]map[string]string{"demo-2.tar.gz": {"requirements.txt": "click>=8\ntomli>=2; python_version < '3.13'\n"}}})
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

// A Git-fetched port whose source can't be read isn't compared, which
// holds as what couldn't be checked does (D4); a port that fetches no
// source has nothing to compare, and says so without holding.
func TestWhatCantBeComparedSaysSo(t *testing.T) {
	e, branch, base, tree := revisionFixture(t, map[string]string{"devel/gitty/Portfile": "name gitty\n", "devel/meta/Portfile": "name meta\n"})
	p := newPlanner(t)
	e.ArchivePlanner = p
	e.PortReader = fakePorts{directories: map[string][]macports.PortInfo{"devel/gitty": {{Name: "gitty"}}, "devel/meta": {{Name: "meta"}}}}
	for _, tree := range []model.ObjectID{base, tree} {
		p.add(tree, plannedPort{info: macports.PortInfo{Name: "gitty", Options: map[string]string{"fetch.type": "git"}}})
		p.add(tree, plannedPort{info: macports.PortInfo{Name: "meta"}, noSource: true})
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
	require.Equal(t, []model.Coverage{{Path: "devel/meta", Relevance: "unknown", Treatment: "inspected", Reason: "meta fetches no upstream source, so there's nothing to compare"}}, byPort["meta"].Comparison.Coverage)
}

// What's recorded for a revision stands: it's read, not made again. One
// recorded under other rules, for another tree, or for another base
// doesn't stand for it, and is made again; a port the revision removed
// isn't assessed.
func TestWhatsRecordedForARevisionStands(t *testing.T) {
	e, branch, base, tree := revisionFixture(t, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\n"})
	p := newPlanner(t)
	e.ArchivePlanner = p
	e.PortReader = fakePorts{directories: map[string][]macports.PortInfo{"textproc/jq": {{Name: "jq"}}}}
	p.add(base, plannedPort{info: macports.PortInfo{Name: "jq", Version: "1.7.1"}, archives: map[string]map[string]string{"jq-1.7.1.tar.gz": {"LICENSE": "MIT\n"}}})
	p.add(tree, plannedPort{info: macports.PortInfo{Name: "jq", Version: "1.8.1"}, archives: map[string]map[string]string{"jq-1.8.1.tar.gz": {"LICENSE": "MIT\n"}}})
	marker := model.UpstreamComparison{Changes: []model.UpstreamChange{{Kind: "license", Path: "LICENSE", Message: "recorded", Hold: true}}}
	record := func(a model.Assessment) {
		require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error { return tx.RecordAssessment(a) }))
	}
	record(model.Assessment{Branch: branch.ID, Tree: tree, Base: branch.Base, Port: "jq", Directory: "textproc/jq", Comparison: marker, Policy: assess.Policy, At: at})
	assessments, err := e.revisionAssessments(t.Context(), branch.ID, branch.Base, tree, true)
	require.NoError(t, err)
	require.Len(t, assessments, 1)
	require.Equal(t, marker, assessments[0].Comparison, "what's recorded is read")
	require.Zero(t, p.fetches.Load())

	for _, stale := range []model.Assessment{
		{Branch: branch.ID, Tree: tree, Base: branch.Base, Port: "jq", Directory: "textproc/jq", Comparison: marker, Policy: assess.Policy + 1, At: at},
	} {
		other := editTree(t, e, tree, map[string]string{"textproc/jq/files/a.diff": fmt.Sprint(stale.Policy)})
		p.same(other, tree)
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
	p.same(later, tree)
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
	e, branch, base, tree := revisionFixture(t, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\n"})
	p := newPlanner(t)
	e.ArchivePlanner = p
	e.PortReader = fakePorts{directories: map[string][]macports.PortInfo{"textproc/jq": {{Name: "jq"}}}}
	manual := map[string]string{"LICENSE": "the manual's license\n"}
	p.add(base, plannedPort{info: macports.PortInfo{Name: "jq", Version: "1.7.1"}, archives: map[string]map[string]string{
		"jq-1.7.1.tar.gz": {"LICENSE": "MIT\n"}, "jq-manual.tar.gz": manual}})
	p.add(tree, plannedPort{info: macports.PortInfo{Name: "jq", Version: "1.8.1"}, archives: map[string]map[string]string{
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

func (c commitArchives) SourceArchive(_ context.Context, _ macports.PortInfo, commit, directory string) (string, error) {
	c.asked.Add(1)
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
	project := t.TempDir()
	run(t, project, "init", "-q")
	write(t, project, map[string]string{"README": "1\n"})
	run(t, project, "add", "-A")
	run(t, project, "commit", "-q", "-m", "one")
	run(t, project, "tag", "v1")
	write(t, project, map[string]string{"README": "2\n"})
	run(t, project, "commit", "-q", "-am", "two")
	run(t, project, "tag", "v2")
	v1, v2 := run(t, project, "rev-parse", "v1^{commit}"), run(t, project, "rev-parse", "v2^{commit}")

	e, branch, base, tree := revisionFixture(t, map[string]string{"devel/libharbor/Portfile": "name libharbor\nversion 3\n"})
	p := newPlanner(t)
	e.ArchivePlanner = p
	e.PortReader = fakePorts{directories: map[string][]macports.PortInfo{"devel/libharbor": {{Name: "libharbor"}}}}
	archives := commitArchives{t: t, files: map[string]map[string]string{v1: {"LICENSE": "MIT\n"}, v2: {"LICENSE": "GPL\n"}}, asked: &atomic.Int64{}}
	e.SourceArchiver = archives
	tool := func(ref string) plannedPort {
		return plannedPort{info: macports.PortInfo{Name: "libharbor", Options: map[string]string{"fetch.type": "git", "git.url": project, "git.branch": ref}}}
	}
	p.add(base, tool("v1"))
	p.add(tree, tool("v2"))
	assessments, err := e.revisionAssessments(t.Context(), branch.ID, branch.Base, tree, true)
	require.NoError(t, err)
	require.Len(t, assessments, 1)
	require.Empty(t, assessments[0].Comparison.Problem)
	require.Equal(t, []string{"upstream's LICENSE changed; the Portfile's license line may need to follow"}, holds(assessments[0]))
	require.Equal(t, []model.Coverage{{Path: v2, Relevance: "unknown", Treatment: "inspected",
		Reason: "read from the forge's archive of each commit, submodules left out; the base's git.branch as it names a commit now"}}, assessments[0].Comparison.Coverage)
	require.EqualValues(t, 2, archives.asked.Load())

	later := editTree(t, e, tree, map[string]string{"devel/libharbor/files/a.diff": "a"})
	p.same(later, tree)
	_, err = e.revisionAssessments(t.Context(), branch.ID, branch.Base, later, true)
	require.NoError(t, err)
	require.EqualValues(t, 2, archives.asked.Load(), "the forge asked nothing again")

	moved := editTree(t, e, tree, map[string]string{"devel/libharbor/files/b.diff": "b"})
	p.add(moved, tool("v9"))
	assessments, err = e.revisionAssessments(t.Context(), branch.ID, branch.Base, moved, true)
	require.NoError(t, err)
	require.Contains(t, assessments[0].Comparison.Problem, "the revision's git.branch couldn't be resolved")
	require.NotEmpty(t, holds(assessments[0]))

	abbreviated := editTree(t, e, tree, map[string]string{"devel/libharbor/files/c.diff": "c"})
	p.add(abbreviated, tool(v2[:9]))
	assessments, err = e.revisionAssessments(t.Context(), branch.ID, branch.Base, abbreviated, true)
	require.NoError(t, err)
	require.Equal(t, "the revision's git.branch is an abbreviated commit, "+v2[:9]+", which only a clone expands", assessments[0].Comparison.Problem)
}

// A Git-fetched port whose tag moved after its check is a concern: the
// check built one source, and the submission would ship another. One
// whose tag still names what was built, or can't be read now, isn't.
func TestASourceMovedSinceItsCheckIsAConcern(t *testing.T) {
	project := t.TempDir()
	run(t, project, "init", "-q")
	write(t, project, map[string]string{"README": "1\n"})
	run(t, project, "add", "-A")
	run(t, project, "commit", "-q", "-m", "one")
	run(t, project, "tag", "v2")
	built := run(t, project, "rev-parse", "HEAD")
	f := setup(t)
	e := f.open(t)
	target := model.PlanTarget{ID: "tool", Target: model.Target{Name: "tool"}}
	evidence := &Evidence{Run: model.Run{Number: 7}, Plan: model.Plan{Targets: []model.PlanTarget{target}, Builds: []model.EnvironmentPlan{{
		Order: []model.TargetID{"tool"}, Git: map[model.TargetID]model.GitSource{"tool": {URL: project, Ref: "v2", Commit: model.ObjectID(built)}}}}}}
	require.Empty(t, e.movedSources(t.Context(), evidence))

	write(t, project, map[string]string{"README": "2\n"})
	run(t, project, "commit", "-q", "-am", "two")
	run(t, project, "tag", "-f", "v2")
	now := run(t, project, "rev-parse", "HEAD")
	moved := e.movedSources(t.Context(), evidence)
	require.Len(t, moved, 1)
	require.Equal(t, model.Concern{Origin: model.FromUpstream, Port: "tool", Rule: "source-moved", Subject: now, Class: model.Introduced,
		Detail: "tool's git.branch v2 named " + built[:7] + " when check-7 planned it, and names " + now[:7] + " now: the check built another source than this would submit"}, moved[0])
	require.Contains(t, SubmitPlan{Moved: moved}.held(), moved[0].Detail, "it holds a submission nobody looks over")

	evidence.Plan.Builds[0].Git["tool"] = model.GitSource{URL: t.TempDir() + "/gone", Ref: "v2", Commit: model.ObjectID(built)}
	require.Empty(t, e.movedSources(t.Context(), evidence), "a ref that can't be read now isn't said to have moved")
	require.Empty(t, e.movedSources(t.Context(), nil))
}
