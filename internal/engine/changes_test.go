package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/fidelity"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// zdemo is a port of three subports, its main port's lines inside
// terraform's stub condition, and its others set apart by subport blocks.
func zdemo(mainRevision, develVersion string) string {
	return "PortSystem 1.0\nname zdemo\nversion 1.0\ncategories devel\nlicense MIT\nmaintainers nomaintainer\n" +
		"homepage https://example.invalid\ndescription demo\nlong_description demo\nmaster_sites https://example.invalid/releases\n" +
		"checksums sha256 " + strings.Repeat("a", 64) + " size 10\n" +
		"if {${subport} eq ${name}} {\n    revision " + mainRevision + "\n}\n" +
		"subport zdemo-devel {\n    version " + develVersion + "\n}\n" +
		"subport zdemo-legacy {\n    version 0.9\n}\n"
}

// A revision's change record says which of a directory's subports it
// changed, as MacPorts evaluates the base and the revision: an edit
// inside one subport's block changes that subport, and an edit inside
// `if {${subport} eq ${name}}`, terraform-1.16's stub, the main port
// alone. The directory's text says every subport changed. The record is
// kept, and what's recorded is read again; only the subports it names are
// assessed.
func TestARevisionsRecordSaysWhichSubportsChanged(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.options.Tclsh = testsupport.MacPortsTclsh(t)
	write(t, f.upstream, map[string]string{"devel/zdemo/Portfile": zdemo("0", "2.0")})
	testsupport.Git(t, f.upstream, "add", ".")
	testsupport.Git(t, f.upstream, "commit", "-q", "-m", "zdemo: new port")
	e := f.open(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "subported"})
	require.NoError(t, err)
	trees, err := e.Repo.CommitTrees(t.Context(), []string{string(branch.Base)})
	require.NoError(t, err)
	base := model.ObjectID(trees[string(branch.Base)])

	devel := editTree(t, e, base, map[string]string{"devel/zdemo/Portfile": zdemo("0", "2.1")})
	records, err := e.revisionChanges(t.Context(), branch.ID, branch.Base, devel, true)
	require.NoError(t, err)
	record := records["devel/zdemo"]
	require.Empty(t, record.Problem)
	require.Equal(t, fidelity.ChangePolicy, record.Policy)
	require.Equal(t, []string{"zdemo-devel"}, record.Changed())
	require.Equal(t, []string{"zdemo", "zdemo-devel", "zdemo-legacy"}, portNames(record.Ports), "the main port first")
	require.Contains(t, record.Ports[1].Fields, model.FieldChange{Field: "version", From: "2.0", To: "2.1"})
	require.NotEmpty(t, record.Platform.OS)

	stub := editTree(t, e, base, map[string]string{"devel/zdemo/Portfile": zdemo("1", "2.0")})
	records, err = e.revisionChanges(t.Context(), branch.ID, branch.Base, stub, true)
	require.NoError(t, err)
	require.Equal(t, []string{"zdemo"}, records["devel/zdemo"].Changed())
	require.Equal(t, []model.FieldChange{{Field: "revision", From: "0", To: "1"}}, records["devel/zdemo"].Ports[0].Fields)

	recorded, err := e.revisionChanges(t.Context(), branch.ID, branch.Base, stub, false)
	require.NoError(t, err)
	require.Equal(t, []string{"zdemo"}, recorded["devel/zdemo"].Changed(), "read as recorded")

	assessments, err := e.revisionAssessments(t.Context(), branch.ID, branch.Base, devel, true)
	require.NoError(t, err)
	var assessed []string
	for _, a := range assessments {
		assessed = append(assessed, a.Port)
	}
	require.Equal(t, []string{"zdemo-devel"}, assessed, "upstream is compared for the subport the revision changed")
}

// A record never narrows further than it covered. A revision that changes
// zdemo-devel natively and zdemo-legacy only under Darwin 19, which this
// Mac's evaluation doesn't take, changes both as far as the record can
// say; one whose platform condition is outside any subport's block keeps
// the directory's text scope (Codex's review of 386ac2cc, finding 1).
func TestARecordKeepsTheSubportsItCantSee(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.options.Tclsh = testsupport.MacPortsTclsh(t)
	write(t, f.upstream, map[string]string{"devel/zdemo/Portfile": zdemo("0", "2.0")})
	testsupport.Git(t, f.upstream, "add", ".")
	testsupport.Git(t, f.upstream, "commit", "-q", "-m", "zdemo: new port")
	e := f.open(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "mixed"})
	require.NoError(t, err)
	trees, err := e.Repo.CommitTrees(t.Context(), []string{string(branch.Base)})
	require.NoError(t, err)
	base := model.ObjectID(trees[string(branch.Base)])

	legacy := "subport zdemo-legacy {\n    version 0.9\n}\n"
	mixed := strings.Replace(zdemo("0", "2.1"), legacy,
		"subport zdemo-legacy {\n    version 0.9\n    platform darwin 19 {\n        configure.args-append --old\n    }\n}\n", 1)
	tree := editTree(t, e, base, map[string]string{"devel/zdemo/Portfile": mixed})
	records, err := e.revisionChanges(t.Context(), branch.ID, branch.Base, tree, true)
	require.NoError(t, err)
	record := records["devel/zdemo"]
	require.Equal(t, []string{"zdemo-devel"}, record.Changed(), "what this Mac's evaluation sees")
	require.Equal(t, []string{"zdemo-legacy"}, record.Unseen)
	scope, ok := changedSubports(records, "devel/zdemo")
	require.True(t, ok)
	require.Equal(t, []string{"zdemo-devel", "zdemo-legacy"}, scope)
	ports, notes := recordedPorts([]string{"devel/zdemo"}, records)
	require.Equal(t, []string{"zdemo-devel", "zdemo-legacy"}, ports)
	require.Equal(t, "changed where this Mac's evaluation doesn't look", notes["zdemo-legacy"])

	everywhere := zdemo("0", "2.1") + "if {${os.major} < 20} {\n    depends_build-append port:cctools\n}\n"
	tree = editTree(t, e, base, map[string]string{"devel/zdemo/Portfile": everywhere})
	records, err = e.revisionChanges(t.Context(), branch.ID, branch.Base, tree, true)
	require.NoError(t, err)
	require.True(t, records["devel/zdemo"].AllUnseen)
	_, ok = changedSubports(records, "devel/zdemo")
	require.False(t, ok, "the text scope: every subport")
}

// flakyFamily is MacPorts' evaluator, failing its first evaluations of a
// family, as a busy MacPorts or a timeout would, and counting them all.
type flakyFamily struct {
	PortReader
	family FamilyReader
	fails  int
	calls  int
}

func (f *flakyFamily) Family(ctx context.Context, source model.Source, directory string) (macports.Snapshot, error) {
	f.calls++
	if f.fails > 0 {
		f.fails--
		return macports.Snapshot{}, errors.New("MacPorts was busy")
	}
	return f.family.Family(ctx, source, directory)
}

// A record that couldn't be made is made again by the next collection,
// and a read that collects nothing, as status's, says why it couldn't
// rather than that it isn't evaluated yet, and evaluates nothing (Codex's
// review of 386ac2cc, finding 5).
func TestAFailedRecordIsTriedAgain(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.options.Tclsh = testsupport.MacPortsTclsh(t)
	write(t, f.upstream, map[string]string{"devel/zdemo/Portfile": zdemo("0", "2.0")})
	testsupport.Git(t, f.upstream, "add", ".")
	testsupport.Git(t, f.upstream, "commit", "-q", "-m", "zdemo: new port")
	e := f.open(t)
	reader, err := e.portReader()
	require.NoError(t, err)
	flaky := &flakyFamily{PortReader: reader, family: reader.(FamilyReader), fails: 1}
	e.PortReader = flaky
	branch, err := e.Start(t.Context(), StartRequest{Name: "flaky"})
	require.NoError(t, err)
	trees, err := e.Repo.CommitTrees(t.Context(), []string{string(branch.Base)})
	require.NoError(t, err)
	tree := editTree(t, e, model.ObjectID(trees[string(branch.Base)]), map[string]string{"devel/zdemo/Portfile": zdemo("0", "2.1")})

	records, err := e.revisionChanges(t.Context(), branch.ID, branch.Base, tree, true)
	require.NoError(t, err)
	require.Equal(t, "the base couldn't be evaluated: MacPorts was busy", records["devel/zdemo"].Problem)

	calls := flaky.calls
	records, err = e.revisionChanges(t.Context(), branch.ID, branch.Base, tree, false)
	require.NoError(t, err)
	require.Equal(t, calls, flaky.calls, "a read evaluates nothing")
	_, notes := recordedPorts([]string{"devel/zdemo"}, records)
	require.Equal(t, "the base couldn't be evaluated: MacPorts was busy", notes["zdemo"])

	records, err = e.revisionChanges(t.Context(), branch.ID, branch.Base, tree, true)
	require.NoError(t, err)
	require.Empty(t, records["devel/zdemo"].Problem, "tried again, and made")
	require.Equal(t, []string{"zdemo-devel"}, records["devel/zdemo"].Changed())
	recorded, err := e.revisionChanges(t.Context(), branch.ID, branch.Base, tree, false)
	require.NoError(t, err)
	require.Empty(t, recorded["devel/zdemo"].Problem, "the made record replaced the failed one")
}

// A branch changes the subports its record says: one editing
// zdemo-devel's block changes zdemo-devel, and neither zdemo nor
// zdemo-legacy, though the directory is zdemo's; before it has a record,
// it changes the port its directory is named for, as its text says.
func TestABranchChangesTheSubportsItsRecordSays(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.options.Tclsh = testsupport.MacPortsTclsh(t)
	write(t, f.upstream, map[string]string{"devel/zdemo/Portfile": zdemo("0", "2.0")})
	testsupport.Git(t, f.upstream, "add", ".")
	testsupport.Git(t, f.upstream, "commit", "-q", "-m", "zdemo: new port")
	e := f.open(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "zdemo-devel"})
	require.NoError(t, err)
	_, err = e.Edit(t.Context(), branch, "zdemo")
	require.NoError(t, err)
	write(t, branch.Worktree, map[string]string{"devel/zdemo/Portfile": zdemo("0", "2.1")})
	changing := func(port string) int {
		t.Helper()
		branches, err := e.BranchesChanging(t.Context(), port)
		require.NoError(t, err)
		return len(branches)
	}
	require.Equal(t, 1, changing("zdemo"), "no record yet: the directory's name")
	require.Zero(t, changing("zdemo-devel"))

	head, _, err := e.Repo.Branch(t.Context(), branch.Name)
	require.NoError(t, err)
	tree, err := e.branchTree(t.Context(), branch, head)
	require.NoError(t, err)
	_, err = e.revisionChanges(t.Context(), branch.ID, branch.Base, tree, true)
	require.NoError(t, err)
	require.Equal(t, 1, changing("zdemo-devel"))
	require.Zero(t, changing("zdemo"), "the record says the main port is as it was")
	require.Zero(t, changing("zdemo-legacy"))

	write(t, branch.Worktree, map[string]string{"devel/zdemo/Portfile": zdemo("1", "2.1")})
	require.Equal(t, 1, changing("zdemo"), "files edited since their record are read by their text again")
}

// tidy names the subport a hand edit changes, as its record says: a
// version moved in zdemo-devel's block is "zdemo-devel: update to 2.1",
// where the directory's name read the main port's version, which didn't
// move, and found no subject.
func TestTidyNamesTheSubportAHandEditChanges(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.options.Tclsh = testsupport.MacPortsTclsh(t)
	write(t, f.upstream, map[string]string{"devel/zdemo/Portfile": zdemo("0", "2.0")})
	testsupport.Git(t, f.upstream, "add", ".")
	testsupport.Git(t, f.upstream, "commit", "-q", "-m", "zdemo: new port")
	e := f.open(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "zdemo-devel"})
	require.NoError(t, err)
	_, err = e.Edit(t.Context(), branch, "zdemo")
	require.NoError(t, err)
	write(t, branch.Worktree, map[string]string{"devel/zdemo/Portfile": zdemo("0", "2.1")})
	plan, err := e.PlanTidy(t.Context(), TidyRequest{Branch: branch})
	require.NoError(t, err)
	require.Len(t, plan.Groups, 1)
	require.Equal(t, []string{"zdemo-devel"}, plan.Groups[0].Ports)
	require.Equal(t, "zdemo-devel: update to 2.1", plan.Groups[0].Subject())
}

// An adopted pull request's Portfile isn't the person's, so nothing
// evaluates it for a record (the trust rule), and its directories keep
// their text scope.
func TestAnAdoptedPullRequestHasNoRecord(t *testing.T) {
	t.Parallel()
	e, branch, _, tree := revisionFixture(t, map[string]string{"devel/libharbor/Portfile": "name libharbor\nversion 3\n"})
	branch.PullRequest = &model.PullRequest{Repository: UpstreamRepository, Number: 4711, Head: "someone:feature", Adopted: true}
	require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error { return tx.UpdateBranch(branch) }))
	records, err := e.revisionChanges(t.Context(), branch.ID, branch.Base, tree, true)
	require.NoError(t, err)
	require.Empty(t, records)
	_, ok := changedSubports(records, "devel/libharbor")
	require.False(t, ok)
	require.True(t, recordedChange(records, "devel/libharbor", "libharbor"), "its text scope")
}

func portNames(ports []model.SubportChange) []string {
	var names []string
	for _, port := range ports {
		names = append(names, port.Port)
	}
	return names
}
