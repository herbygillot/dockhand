package change

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/edit"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/git/gittest"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/statestore"
	"github.com/herbygillot/dockhand/internal/tool"
)

// tools is the finder every fixture opens with: the real PATH search,
// because the git under test is the real one. This package drives real
// git for the reason internal/git, internal/ledger and internal/statestore
// do — what is being proven is what git does with a graft, an assert
// line and a delete line, and a fake would prove something about the
// fake.
var tools = tool.NewFinder(nil)

// portdir is the headline portdir the fixtures use: sysutils/jq at 1.7,
// the port every minted branch moves.
const portdir = TreePath("sysutils/jq")

// newRepo is the ports-tree-shaped repository every fixture starts
// from, and a store over it. It is gittest.PortsTree widened rather than
// PortsTree itself: git.GraftTree adds a file beside its siblings and
// refuses to invent a DIRECTORY that is not there ("a missing directory
// means the path does not name what the caller thought"), so a test that
// needs a second portdir, or the tree's own resources, must start from a
// tree that has them.
func newRepo(t *testing.T) (*git.Repo, *statestore.Store) {
	t.Helper()
	repo := gittest.Init(t, tools, "", map[string]string{
		"sysutils/jq/Portfile":                    "version 1.7\n",
		"sysutils/mise/Portfile":                  "version 2024.1.0\n",
		"textproc/oniguruma6/Portfile":            "version 6.9.8\n",
		"_resources/port1.0/group/golang-1.0.tcl": "# the golang port group\n",
	})
	return repo, statestore.Open(repo)
}

// primary is the tree's base commit, which is what every fixture mints
// over.
func primary(t *testing.T, repo *git.Repo) string {
	t.Helper()
	sha, err := repo.RevParse(context.Background(), "HEAD")
	require.NoError(t, err)
	return sha
}

// refValueOf is what a ref holds according to git itself, absence
// included, read outside every verb this package uses so that a bug here
// cannot make its own assertion pass.
func refValueOf(t *testing.T, repo *git.Repo, ref string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", repo.Root, "rev-parse", "--verify", "--quiet", ref).Output()
	if err != nil {
		return ""
	}
	return string(out[:len(out)-1])
}

// aPlan is the bump every fixture prepares: jq 1.7 -> 1.8, one version
// edit over the ports tree's own Portfile bytes.
func aPlan() *plan.Plan {
	src := "version 1.7\n"
	return &plan.Plan{
		Format:         plan.Format,
		Intent:         "bump",
		Port:           "jq",
		Slug:           "jq-1.8",
		Summary:        "jq: update to 1.8",
		Portdir:        "/host/ports/sysutils/jq",
		PortfileSHA256: edit.FileSHA256([]byte(src)),
		Edits: []edit.Edit{{
			Kind: edit.Version, Start: 8, End: 11, Old: "1.7", New: "1.8", Reason: "version",
		}},
		Predicted: []plan.ContextDelta{{
			Subport: "jq",
			Changes: []plan.Change{{Field: "version", Old: []string{"1.7"}, New: []string{"1.8"}}},
		}},
	}
}

// stubEval answers Values from a table, so a preparation is testable
// with no MacPorts installation — which is the whole reason Evaluator is
// declared by the consumer.
type stubEval struct {
	vals info.Values
	err  error
}

func (s stubEval) Values(context.Context, string) (info.Values, error) { return s.vals, s.err }

func prepared(t *testing.T, repo *git.Repo) Prepared {
	t.Helper()
	base := primary(t, repo)
	at, err := repo.CommittedAt(context.Background(), base)
	require.NoError(t, err)
	p, err := Prepare(context.Background(), aPlan(),
		Source{Base: record.Base{Sha: base, CommittedAt: at}, Portdir: portdir, Portfile: []byte("version 1.7\n")},
		stubEval{vals: info.Values{Name: "jq", Version: "1.7"}})
	require.NoError(t, err)
	return p
}

func TestPrepareIsTheOneFileSet(t *testing.T) {
	repo, _ := newRepo(t)
	p := prepared(t, repo)

	// The Portfile first, computed from the BASE blob and not from any
	// working file, with the plan's whole files after it in the plan's own
	// order. One list, and every realization of this change is built from
	// it.
	require.Len(t, p.Files, 1)
	assert.Equal(t, "sysutils/jq/Portfile", p.Files[0].Path,
		"File.Path is tree-relative: the portdir prefix is joined at this boundary, not at materialize")
	assert.Equal(t, "version 1.8\n", string(p.Files[0].Content))
	assert.Equal(t, portdir, p.Portdir, "the portdir is tree-relative, from Source and not from the plan's host path")
	require.Len(t, p.Subjects, 1)
	assert.Equal(t, "jq", p.Subjects[0].Port)
	assert.Equal(t, []string{"jq"}, p.Subjects[0].Names, "written as [Port]: the empty slice already means nobody asked")
	assert.Equal(t, "sysutils/jq", p.Subjects[0].Portdir)
	assert.Equal(t, "1.8", p.Subjects[0].Target, "the target is the slug minus the port, not a parse of a branch name")
	assert.Equal(t, []record.Region{{Kind: edit.Version, Edits: 1}}, p.Regions)
	assert.Equal(t, "1.7", p.Before.Version)
	assert.Equal(t, "1.8", p.After.Version, "After is Before with the predicted version movement, evaluated on both sides")
}

func TestPrepareCarriesThePlansAuxiliaryFiles(t *testing.T) {
	repo, _ := newRepo(t)
	pl := aPlan()
	was := []byte("--- a\n+++ b\n")
	pl.Files = []plan.FileEdit{{Path: "files/patch-a.diff", Content: "--- a\n",
		Reason: "2 hunks moved", Was: edit.FileSHA256(was)}}
	base := primary(t, repo)
	p, err := Prepare(context.Background(), pl,
		Source{Base: record.Base{Sha: base}, Portdir: portdir, Portfile: []byte("version 1.7\n"),
			Files: map[string][]byte{"files/patch-a.diff": was}}, nil)
	require.NoError(t, err)
	require.Len(t, p.Files, 2)
	assert.Equal(t, "sysutils/jq/files/patch-a.diff", p.Files[1].Path,
		"a plan's whole files ride in the same set as the Portfile, tree-relative like it")
}

func TestPrepareRefusesTheDriftedPortfile(t *testing.T) {
	repo, _ := newRepo(t)
	_, err := Prepare(context.Background(), aPlan(),
		Source{Base: record.Base{Sha: primary(t, repo)}, Portdir: portdir, Portfile: []byte("version 1.6\n")}, nil)
	assert.ErrorIs(t, err, ErrDrift)
}

func TestPrepareRefusesAPredictionAboutAnotherState(t *testing.T) {
	repo, _ := newRepo(t)
	_, err := Prepare(context.Background(), aPlan(),
		Source{Base: record.Base{Sha: primary(t, repo)}, Portdir: portdir, Portfile: []byte("version 1.7\n")},
		stubEval{vals: info.Values{Name: "jq", Version: "1.6"}})
	assert.ErrorIs(t, err, ErrPredicted,
		"the bytes are the ones planned against and the evaluated meaning is not")
}

func TestPrepareWithNoEvaluatorLeavesTheCrossingUnknown(t *testing.T) {
	repo, _ := newRepo(t)
	base := primary(t, repo)
	p, err := Prepare(context.Background(), aPlan(),
		Source{Base: record.Base{Sha: base}, Portdir: portdir, Portfile: []byte("version 1.7\n")}, nil)
	require.NoError(t, err)
	assert.Equal(t, record.CrossingUnknown, Cross(p))
	assert.True(t, Cross(p).WithholdsUnattended(), "rule 7: what could not be classified is not published unattended")
}

func TestPrepareReturnsAnEvaluatorsFailureAsItself(t *testing.T) {
	repo, _ := newRepo(t)
	boom := errors.New("the shell would not start")
	_, err := Prepare(context.Background(), aPlan(),
		Source{Base: record.Base{Sha: primary(t, repo)}, Portdir: portdir, Portfile: []byte("version 1.7\n")},
		stubEval{err: boom})
	assert.ErrorIs(t, err, boom, "a failure is a fault; rule 7's slot is for the caller who never asked")
}

func TestPrepareNeedsABaseAndATreeRelativePortdir(t *testing.T) {
	_, err := Prepare(context.Background(), aPlan(), Source{Portdir: portdir, Portfile: []byte("version 1.7\n")}, nil)
	require.ErrorIs(t, err, ErrIncomplete)
	_, err = Prepare(context.Background(), aPlan(), Source{Base: record.Base{Sha: "abc"}, Portfile: []byte("version 1.7\n")}, nil)
	assert.ErrorIs(t, err, ErrIncomplete)
}

func TestCrossReadsBothSides(t *testing.T) {
	for _, c := range []struct {
		name     string
		from, to string
		want     record.Crossing
	}{
		{"stable to stable", "1.7", "1.8", record.StableToStable},
		{"leaving stable", "1.7", "1.8-rc1", record.StableToPrerelease},
		{"returning to stable", "1.8-rc1", "1.8", record.PrereleaseToStable},
		{"prerelease lateral", "1.8-rc1", "1.8-rc2", record.PrereleaseLateral},
		{"one side missing", "1.7", "", record.CrossingUnknown},
		{"neither side", "", "", record.CrossingUnknown},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := Prepared{Before: info.Values{Version: c.from}, After: info.Values{Version: c.to}}
			assert.Equal(t, c.want, Cross(p))
		})
	}
}

func TestIdentifyJoinsThePortdirOnceAndIsTheTreeCommitWrites(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()
	base := primary(t, repo)
	p := prepared(t, repo)

	content, err := p.Identify(ctx, repo, base)
	require.NoError(t, err)
	// The file landed at the portdir-joined path and nowhere else.
	blob, err := repo.BlobAt(ctx, string(content), "sysutils/jq/Portfile")
	require.NoError(t, err)
	assert.Equal(t, "version 1.8\n", string(blob))

	sha, committed, err := Commit(ctx, repo, p, base)
	require.NoError(t, err)
	assert.Equal(t, content, committed,
		"Commit is Identify followed by CommitTree, so a record's Content and its Tip's tree cannot disagree")
	tree, err := repo.RevParse(ctx, sha+"^{tree}")
	require.NoError(t, err)
	assert.Equal(t, string(content), tree)
}

func TestCommitNamesNoRef(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()
	before, err := repo.Branches(ctx, "")
	require.NoError(t, err)

	sha, _, err := Commit(ctx, repo, prepared(t, repo), primary(t, repo))
	require.NoError(t, err)
	require.NotEmpty(t, sha)

	after, err := repo.Branches(ctx, "")
	require.NoError(t, err)
	assert.Equal(t, before, after, "a pure object writer: the commit is unreferenced until a batch names it")
	pins, err := repo.RefsUnder(ctx, PinRef(""))
	require.NoError(t, err)
	assert.Empty(t, pins)
}

func TestMaterializeRefusesAModeItCannotWrite(t *testing.T) {
	repo, _ := newRepo(t)
	p := Prepared{Portdir: portdir, Files: []File{{Path: "files/run.sh", Mode: fs.FileMode(0o755), Content: []byte("#!/bin/sh\n")}}}
	_, err := p.Identify(context.Background(), repo, primary(t, repo))
	assert.ErrorIs(t, err, ErrMode, "a change that says 0755 and lands 0644 is a change that lied about what it wrote")
}

func TestMessageIsTheSummaryAndTheTrailerAndNothingElse(t *testing.T) {
	assert.Equal(t, "jq: update to 1.8", Message(Prepared{Summary: "jq: update to 1.8"}))

	p := Prepared{Summary: "jq: update to 1.8", Closes: "12345"}
	assert.Equal(t, "jq: update to 1.8\n\nCloses: https://trac.macports.org/ticket/12345", Message(p),
		"a blank line before it, because git reads a trailer only in the last paragraph")
	assert.NotContains(t, Message(p)[len(Message(p))-1:], "\n",
		"no trailing newline: git supplies one, and two messages differing by one are two commits")
}

func TestMessageIsTheCohortsMessageToo(t *testing.T) {
	p := Prepared{
		Summary: "jq: revbump 2 dependents of jq 1.8",
		Closes:  "999",
		Subjects: []record.Subject{
			{Port: "jq", Portdir: "sysutils/jq"},
			{Port: "oniguruma6", Portdir: "textproc/oniguruma6", Reason: "depends_lib on jq"},
			{Port: "mise", Portdir: "sysutils/mise", Reason: "depends_lib on jq"},
		},
	}
	msg := Message(p)
	assert.Contains(t, msg, "  oniguruma6 (textproc/oniguruma6): depends_lib on jq")
	assert.Contains(t, msg, "  mise (sysutils/mise): depends_lib on jq")
	assert.NotContains(t, msg, "  jq (sysutils/jq)", "the headline has the subject line; the members have the body")
	// The trailer is still last, after the members, because a trailer is
	// only read in the final paragraph.
	assert.Greater(t, len(msg), len("Closes: "))
	assert.Equal(t, "jq: revbump 2 dependents of jq 1.8", msg[:len("jq: revbump 2 dependents of jq 1.8")])
	assert.Contains(t, msg[len(msg)-60:], "Closes: https://trac.macports.org/ticket/999")
}

func TestSnapshotCapturesTheWorkingTreeIncludingARemoval(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()
	dir := filepath.Join(repo.Root, "sysutils", "jq")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "files"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "files", "patch-old.diff"), []byte("old\n"), 0o644))
	// Commit the patch so HEAD carries it, then remove it and edit the
	// Portfile the way a person would before asking `verify <portdir>`.
	plant(t, repo, "add", "-A")
	plant(t, repo, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "add a patch")
	require.NoError(t, os.Remove(filepath.Join(dir, "files", "patch-old.diff")))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Portfile"), []byte("version 1.9\n"), 0o644))

	sha, content, err := Snapshot(ctx, repo, "chg-01", dir)
	require.NoError(t, err)
	require.NotEmpty(t, sha)

	blob, err := repo.BlobAt(ctx, string(content), "sysutils/jq/Portfile")
	require.NoError(t, err)
	assert.Equal(t, "version 1.9\n", string(blob))
	_, err = repo.BlobAt(ctx, string(content), "sysutils/jq/files/patch-old.diff")
	require.Error(t, err, "a person who deleted a stale patch meant the deletion; a snapshot that committed HEAD's copy back would build a portdir they do not have")

	pins, err := repo.RefsUnder(ctx, PinRef(""))
	require.NoError(t, err)
	assert.Empty(t, pins, "the pin is AdoptIn's line in the batch that records it, never Snapshot's")
}

func TestSnapshotRefusesAPortdirOutsideTheRepository(t *testing.T) {
	repo, _ := newRepo(t)
	_, _, err := Snapshot(context.Background(), repo, "chg-01", t.TempDir())
	assert.Error(t, err, "nothing here can record what it starts")
}

func TestPermitsIsTheRulingsConfinedSet(t *testing.T) {
	for _, k := range []edit.Kind{edit.Version, edit.RevisionReset, edit.Checksum, edit.VendoredBlock} {
		assert.True(t, Permits(k))
	}
	for _, k := range []edit.Kind{
		edit.Unclassified, edit.RevisionBump, edit.EpochBump, edit.ChecksumSet,
		edit.VendoredNew, edit.DistfileName, edit.ToolchainMin, edit.Rider,
	} {
		assert.False(t, Permits(k), "the permitted set is four kinds and the zero value never permits")
	}
}

func TestJudgeAnswersConfinementAndMovementAndNothingElse(t *testing.T) {
	tree := record.ContentID("t1")
	for _, c := range []struct {
		name string
		in   Reconstruction
		want Simplicity
	}{
		{"never compared", Reconstruction{Regions: []record.Region{{Kind: edit.Version, Edits: 1}}}, Unjudged},
		{"the bytes are not the tip's", Reconstruction{Compared: true, Content: "t2", Tip: tree,
			Regions: []record.Region{{Kind: edit.Version, Edits: 1}}}, Unjudged},
		{"a confined bump", Reconstruction{Compared: true, Content: tree, Tip: tree, Regions: []record.Region{
			{Kind: edit.Version, Edits: 1}, {Kind: edit.RevisionReset, Edits: 1}, {Kind: edit.Checksum, Edits: 3},
		}}, Simple},
		{"a rider rode along", Reconstruction{Compared: true, Content: tree, Tip: tree, Regions: []record.Region{
			{Kind: edit.Version, Edits: 1}, {Kind: edit.Rider, Edits: 1},
		}}, NotSimple},
		{"a same-version refresh", Reconstruction{Compared: true, Content: tree, Tip: tree, Regions: []record.Region{
			{Kind: edit.Checksum, Edits: 2},
		}}, NotSimple},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, why := Judge(c.in)
			assert.Equal(t, c.want, got)
			if c.want == NotSimple {
				assert.NotEmpty(t, why, "a refusal a person cannot read is a refusal nobody can answer")
			}
		})
	}
}

func TestReconstructComparesTheRePlanAgainstTheTip(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()
	base := primary(t, repo)
	at, err := repo.CommittedAt(ctx, base)
	require.NoError(t, err)
	p := prepared(t, repo)
	tip, content, err := Commit(ctx, repo, p, base)
	require.NoError(t, err)

	c := record.Change{
		ID: "chg-01", Tip: tip, Content: content, Base: record.Base{Sha: base, CommittedAt: at},
		Subjects: []record.Subject{{Port: "jq", Portdir: "sysutils/jq"}},
	}
	ev := stubEval{vals: info.Values{Name: "jq", Version: "1.7"}}

	got, err := Reconstruct(ctx, repo, c, aPlan(), ev)
	require.NoError(t, err)
	assert.True(t, got.Compared)
	assert.Equal(t, got.Tip, got.Content, "the same bytes are the same object, by construction")
	assert.Empty(t, got.Diverged)
	simplicity, _ := Judge(got)
	assert.Equal(t, Simple, simplicity)

	// An upstream re-roll: the same plan over the same base writes other
	// bytes, the oid diverges, and the machine refuses — which is the
	// mechanism that retires the stealth re-witness.
	rolled := aPlan()
	rolled.Edits[0].New = "1.8.1"
	got, err = Reconstruct(ctx, repo, c, rolled, ev)
	require.NoError(t, err)
	assert.True(t, got.Compared)
	assert.NotEqual(t, got.Tip, got.Content)
	assert.Equal(t, []string{"sysutils/jq/Portfile"}, got.Diverged,
		"paths, because a person has to answer this — tree-relative, since a change may span portdirs and \"Portfile\" would name several")
	simplicity, _ = Judge(got)
	assert.Equal(t, Unjudged, simplicity, "different bytes is not a finding about simplicity")
}

func TestReconstructReportsWhatItCouldNotDoAsAnAnswer(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()
	for _, c := range []struct {
		name string
		rec  record.Change
		plan *plan.Plan
		ev   Evaluator
	}{
		{"no base", record.Change{Tip: "abc"}, aPlan(), stubEval{}},
		{"no plan", record.Change{Tip: "abc", Base: record.Base{Sha: "def"}}, nil, stubEval{}},
		{"no portdir", record.Change{Tip: "abc", Base: record.Base{Sha: "def"}}, aPlan(), stubEval{}},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := Reconstruct(ctx, repo, c.rec, c.plan, c.ev)
			require.NoError(t, err, "an absence is an answer the machine road acts on, not an incident")
			assert.False(t, got.Compared)
			require.Error(t, got.Err)
			simplicity, _ := Judge(got)
			assert.Equal(t, Unjudged, simplicity)
		})
	}
}

func TestHeldAnswersByOriginActAndInvoker(t *testing.T) {
	person := record.Change{Hold: &record.Hold{Origin: record.HoldPerson, At: time.Now()}}
	crossing := record.Change{Hold: &record.Hold{Origin: record.HoldCrossing, At: time.Now()}}
	unstamped := record.Change{Hold: &record.Hold{At: time.Now()}}

	assert.NoError(t, Held(record.Change{}, ActPublish, record.Machine), "no hold withholds nothing")

	for _, act := range []Act{ActVerify, ActPublish, ActDemolish, ActUnknown} {
		for _, by := range []record.Driver{record.Human, record.Machine} {
			require.ErrorIs(t, Held(person, act, by), ErrHeld, "a person's hold withholds every act for every invoker")
			require.ErrorIs(t, Held(unstamped, act, by), ErrHeld, "rule 7: an origin nobody stamped is a wiring gap")
		}
	}
	assert.NoError(t, Held(crossing, ActVerify, record.Machine), "a crossing never withholds a build")
	assert.NoError(t, Held(crossing, ActVerify, record.Human))
	assert.NoError(t, Held(crossing, ActPublish, record.Human), "the person is warned, never refused")
	assert.NoError(t, Held(crossing, ActDemolish, record.Human))
	require.ErrorIs(t, Held(crossing, ActPublish, record.Machine), ErrHeld)
	require.ErrorIs(t, Held(crossing, ActDemolish, record.Machine), ErrHeld)
	assert.ErrorIs(t, Held(crossing, ActUnknown, record.Human), ErrHeld, "an unnamed act is refused, never permitted")
}

// plant runs git straight against the repository, outside every verb
// this package uses, so a test can be the hand that never took the lock.
func plant(t *testing.T, repo *git.Repo, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo.Root}, args...)...)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
	return string(out)
}

// THE PORTFILE WAS THE PLAN'S ONLY PRECONDITION, and everything beside
// it was written on trust. A change whose Portfile is untouched at the
// base but whose patch file somebody rewrote in between committed the
// planner's stale relocation over the newer file and passed every drift
// check on the way.
func TestPrepareRefusesAWholeFileWhoseBaseBytesMoved(t *testing.T) {
	repo, _ := newRepo(t)
	pl := aPlan()
	pl.Files = []plan.FileEdit{{Path: "files/patch-a.diff", Content: "--- relocated\n",
		Was: edit.FileSHA256([]byte("--- what the planner read\n"))}}

	_, err := Prepare(context.Background(), pl,
		Source{Base: record.Base{Sha: primary(t, repo)}, Portdir: portdir,
			Portfile: []byte("version 1.7\n"),
			Files:    map[string][]byte{"files/patch-a.diff": []byte("--- somebody refreshed it\n")}}, nil)
	require.ErrorIs(t, err, ErrDrift)
	assert.Contains(t, err.Error(), "files/patch-a.diff", "a person meeting drift needs the path")
}

// AND ONE THAT IS GONE. A plan that rewrites a file the base no longer
// holds is about some other state of this portdir, and writing it back
// would resurrect a deleted patch.
func TestPrepareRefusesAWholeFileTheBaseNoLongerHolds(t *testing.T) {
	repo, _ := newRepo(t)
	pl := aPlan()
	pl.Files = []plan.FileEdit{{Path: "files/patch-a.diff", Content: "--- relocated\n",
		Was: edit.FileSHA256([]byte("--- what the planner read\n"))}}

	_, err := Prepare(context.Background(), pl,
		Source{Base: record.Base{Sha: primary(t, repo)}, Portdir: portdir,
			Portfile: []byte("version 1.7\n")}, nil)
	assert.ErrorIs(t, err, ErrDrift)
}

// AN EMPTY Was IS A CLAIM AND NOT A MISSING FIELD: the planner found no
// such file. Over a base that holds one it is the same mistake read from
// the other side — and it is what a producer that simply forgot to
// record Was runs into on its first real portdir, out loud, instead of
// overwriting quietly.
func TestPrepareRefusesACreationOverAFileThatExists(t *testing.T) {
	repo, _ := newRepo(t)
	pl := aPlan()
	pl.Files = []plan.FileEdit{{Path: "files/patch-a.diff", Content: "--- new\n"}}

	_, err := Prepare(context.Background(), pl,
		Source{Base: record.Base{Sha: primary(t, repo)}, Portdir: portdir,
			Portfile: []byte("version 1.7\n"),
			Files:    map[string][]byte{"files/patch-a.diff": []byte("--- it was here all along\n")}}, nil)
	assert.ErrorIs(t, err, ErrDrift)
}

// AND A GENUINE CREATION GOES THROUGH.
func TestPrepareAcceptsANewFileTheBaseDoesNotHold(t *testing.T) {
	repo, _ := newRepo(t)
	pl := aPlan()
	pl.Files = []plan.FileEdit{{Path: "files/patch-new.diff", Content: "--- new\n"}}

	p, err := Prepare(context.Background(), pl,
		Source{Base: record.Base{Sha: primary(t, repo)}, Portdir: portdir,
			Portfile: []byte("version 1.7\n")}, nil)
	require.NoError(t, err)
	require.Len(t, p.Files, 2)
}

// A COHORT COMMIT MUST NAME EVERY PORTFILE IT EDITS.
//
// The body skipped Subjects[0] because a bump's subject line already
// names the headline. A cohort has no headline member — its subject is
// the change it is FOR ("cmark: update to 0.31.2, bump dependents") and
// cmark is not one of the ports it revbumps. Measured: the commit body
// listed four of the five Portfiles it changed, while the pull request
// body next door listed all five.
func TestACohortBodyNamesEveryMemberItEdits(t *testing.T) {
	p := Prepared{
		Summary: "cmark: update to 0.31.2, bump dependents",
		Subjects: []record.Subject{
			{Port: "Aseprite", Portdir: "graphics/Aseprite"},
			{Port: "nheko", Portdir: "net/nheko"},
		},
	}
	msg := Message(p)
	assert.Contains(t, msg, "Aseprite", "the first subject is edited too, and the subject line does not name it")
	assert.Contains(t, msg, "nheko")
}

// AND A BUMP STILL DOES NOT SAY ITS HEADLINE TWICE, which is the rule
// the skip was written for and is still right.
func TestABumpBodyDoesNotRepeatTheSubjectsHeadline(t *testing.T) {
	p := Prepared{
		Summary: "jq: update to 1.8.2",
		Subjects: []record.Subject{
			{Port: "jq", Portdir: "sysutils/jq"},
			{Port: "oniguruma", Portdir: "devel/oniguruma"},
		},
	}
	msg := Message(p)
	assert.Contains(t, msg, "oniguruma")
	assert.Equal(t, 1, strings.Count(msg, "jq"), "named once, in the subject")
}
