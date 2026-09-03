package engine

// Schema 3 moves the record's birth to the mint. Everything a subject
// knows is known there and nowhere later — the directory the change
// touched, the intent that made it, what it moves to, and how far the
// invocation's contract reaches — so these hold what a mint now writes,
// and what the pump then refuses to do with it.

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/edit"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/git/gittest"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/tool"
	"github.com/herbygillot/dockhand/internal/verify/verifytest"
)

// pumpTools reports tart present without one being on the machine.
// The drain's first gate asks whether this machine can verify at all,
// and what these two tests are about is what it does once past it — a
// question that must have the same answer on a runner with no
// hypervisor.
func pumpTools(t *testing.T) *tool.Finder {
	t.Helper()
	return tool.NewFinder(func(name string) (string, error) {
		if name == string(tool.Tart) {
			return filepath.Join(t.TempDir(), "tart"), nil
		}
		return exec.LookPath(name)
	})
}

// bumpPlan is the plan a bump of the fixture tree's jq produces: one
// edit over the Portfile the ports tree was initialized with, held to
// its precondition hash the way a real plan is.
func bumpPlan(t *testing.T, repo *git.Repo, intent, target string) *plan.Plan {
	t.Helper()
	const src = "version 1.7\n"
	return &plan.Plan{
		Format:         plan.Format,
		Intent:         intent,
		Port:           "jq",
		Slug:           "jq-" + target,
		Summary:        "jq: update to " + target,
		Portdir:        repo.Root + "/sysutils/jq",
		PortfileSHA256: edit.FileSHA256([]byte(src)),
		Edits: []edit.Edit{{
			Start: 8, End: 11, Old: "1.7", New: target, Reason: "version",
		}},
	}
}

// A mint with no verification asked for still writes a record. It is
// the only chance: nothing else on that road opens one, and the
// subject's facts are gone the moment the planner returns.
func TestMintBearsTheRecord(t *testing.T) {
	ctx := context.Background()
	repo := gittest.PortsTree(t, realTools)
	var out, errb bytes.Buffer
	eng := testEngine(t, repo, &verifytest.Fake{}, &out, &errb)

	require.NoError(t, runPlan(t, ctx, eng, bumpPlan(t, repo, "bump", "1.8"),
		Policy{Destination: record.ToBranch}))

	tip, err := repo.RevParse(ctx, "dockhand/jq-1.8")
	require.NoError(t, err)
	n, err := ledger.Open(repo).Read(ctx, tip)
	require.NoError(t, err)

	assert.Equal(t, record.Schema, n.Schema)
	assert.Equal(t, "jq-1.8", n.Slug)
	require.Len(t, n.Subjects, 1)
	s := n.Headline()
	assert.Equal(t, "jq", s.Port)
	assert.Equal(t, []string{"jq"}, s.Names,
		"[port] and not empty: a reader must be able to tell no subports from nobody asked")
	assert.Equal(t, "sysutils/jq", s.Portdir)
	assert.Equal(t, "bump", s.Intent)
	assert.Equal(t, "1.8", s.Target, "the planner held the slug and the port apart; so does the record")
	assert.Equal(t, record.ToBranch, n.Destination, "--no-verify narrows the contract, and the note says so")
	assert.Equal(t, record.Human, n.AskedBy)
	assert.Equal(t, record.MintedSingle, n.MintedVia)
	assert.Empty(t, n.Runs, "nothing was submitted")
	assert.Empty(t, n.Jobs)

	primary, err := repo.PrimaryBranch(ctx)
	require.NoError(t, err)
	base, err := repo.RevParse(ctx, primary)
	require.NoError(t, err)
	assert.Equal(t, base, n.Base.Sha)
	assert.False(t, n.Base.CommittedAt.IsZero(),
		"the base's age is how a reader tells a change written against a week-old tree from today's")
}

// The default road records that a verdict was asked for, which is what
// makes the drain willing to retry it.
func TestMintDefaultsToAVerdictDestination(t *testing.T) {
	ctx := context.Background()
	repo := gittest.PortsTree(t, realTools)
	var out, errb bytes.Buffer
	eng := testEngine(t, repo, &verifytest.Fake{}, &out, &errb)

	require.NoError(t, runPlan(t, ctx, eng, bumpPlan(t, repo, "bump", "1.8"), Policy{}))

	tip, err := repo.RevParse(ctx, "dockhand/jq-1.8")
	require.NoError(t, err)
	n, err := ledger.Open(repo).Read(ctx, tip)
	require.NoError(t, err)
	assert.Equal(t, record.ToVerdict, n.Destination)
	assert.Equal(t, record.Running, runOf(n, "Testos").State)
}

// A submission records the guest apart from the verdicts, and stamps
// the session that owns it. The claim is the field the protocol will
// read once the submit lock is retired; today it is provenance.
func TestSubmitRecordsTheGuestAndItsClaim(t *testing.T) {
	ctx := context.Background()
	repo := gittest.PortsTree(t, realTools)
	var out, errb bytes.Buffer
	eng := testEngine(t, repo, &verifytest.Fake{}, &out, &errb)

	require.NoError(t, runPlan(t, ctx, eng, bumpPlan(t, repo, "bump", "1.8"), Policy{Test: true}))

	tip, err := repo.RevParse(ctx, "dockhand/jq-1.8")
	require.NoError(t, err)
	n, err := ledger.Open(repo).Read(ctx, tip)
	require.NoError(t, err)

	job, ok := n.Jobs["Testos"]
	require.True(t, ok, "one guest per release")
	assert.Equal(t, "fake-1", job.Job.ID)
	assert.True(t, job.Test, "the test suite is asked of an environment, so the job remembers it")
	assert.False(t, job.Released)
	require.NotNil(t, job.Claim)
	assert.NotEmpty(t, job.Claim.By, "an unclaimed job and one claimed by nobody are different facts")
	assert.Equal(t, job.Job.Started.UTC(), job.Claim.At,
		"the claim is stamped from the guest's own start, not a second clock read")

	assert.Equal(t, record.Running, runOf(n, "Testos").State)
	assert.Equal(t, "Testos", runOf(n, "Testos").Platform)
}

// A re-derivation at an unchanged version must ignore the binary
// archive: the archive that matches predates the change, so a pass
// earned against it verified nothing. A version bump must not, because
// the archive it would ignore does not exist.
func TestFromSourceIsWrittenForARederivationOnly(t *testing.T) {
	for _, tc := range []struct {
		intent, target string
		want           bool
	}{
		{"refresh-checksums", "checksums", true},
		{"bump", "1.8", false},
		{"bump-revision", "rev1", false},
	} {
		t.Run(tc.intent, func(t *testing.T) {
			ctx := context.Background()
			repo := gittest.PortsTree(t, realTools)
			fake := &verifytest.Fake{}
			var out, errb bytes.Buffer
			eng := testEngine(t, repo, fake, &out, &errb)

			require.NoError(t, runPlan(t, ctx, eng, bumpPlan(t, repo, tc.intent, tc.target), Policy{}))

			require.Len(t, fake.Submitted, 1)
			if tc.want {
				assert.Equal(t, []string{"jq"}, fake.Submitted[0].FromSource,
					"the archive that matches this change predates it")
			} else {
				assert.Empty(t, fake.Submitted[0].FromSource)
			}
			tip, err := repo.RevParse(ctx, "dockhand/"+bumpPlan(t, repo, tc.intent, tc.target).Slug)
			require.NoError(t, err)
			n, err := ledger.Open(repo).Read(ctx, tip)
			require.NoError(t, err)
			assert.Equal(t, tc.want, runOf(n, "Testos").FromSource, "and the note remembers which it was")
		})
	}
}

// Two branches for one port are two branch names, so the in-flight
// refusal never fires between them. The newer one is the change now,
// and the older one gains the field that says why it will learn
// nothing more.
func TestMintSupersedesTheOlderBranchForThePort(t *testing.T) {
	ctx := context.Background()
	repo := gittest.PortsTree(t, realTools)
	var out, errb bytes.Buffer
	eng := testEngine(t, repo, &verifytest.Fake{}, &out, &errb)

	require.NoError(t, runPlan(t, ctx, eng, bumpPlan(t, repo, "bump", "1.8"), Policy{Destination: record.ToBranch}))
	require.NoError(t, runPlan(t, ctx, eng, bumpPlan(t, repo, "bump", "1.9"), Policy{Destination: record.ToBranch}))

	old, err := repo.RevParse(ctx, "dockhand/jq-1.8")
	require.NoError(t, err)
	n, err := ledger.Open(repo).Read(ctx, old)
	require.NoError(t, err)
	assert.Equal(t, "dockhand/jq-1.9", n.SupersededBy)

	// And not the other way about: a mint knows which of two branches is
	// the newer, which is exactly why the field is written here.
	newer, err := repo.RevParse(ctx, "dockhand/jq-1.9")
	require.NoError(t, err)
	n, err = ledger.Open(repo).Read(ctx, newer)
	require.NoError(t, err)
	assert.Empty(t, n.SupersededBy)
}

// Nobody asked for a verdict about a --no-verify branch, so the pump
// must not invent one: starting a build there spends a slot and an hour
// of the machine on an answer the user declined.
func TestPumpNeverDrainsABranchDestination(t *testing.T) {
	ctx := context.Background()
	repo, sha := engineRepo(t)
	fake := &verifytest.Fake{}
	eng := testState(t, repo, fake)
	eng.Tools = pumpTools(t)

	n := mintedNote(t, repo, sha)
	n.Destination = record.ToBranch
	n.Runs[record.RunKey("jq", "Testos")] = record.Run{
		State: record.Queued, Platform: "Testos", Detail: "all slots busy"}
	require.NoError(t, ledger.Open(repo).Write(ctx, n))

	eng.PumpDeferred(ctx, repo, []string{"dockhand/jq-1.8"})
	assert.Empty(t, fake.Submitted, "a branch destination is never drained")

	// The same record, once somebody has asked: `dockhand verify` says a
	// verdict is wanted, and the drain agrees from then on.
	_, err := eng.SubmitRelease(ctx, repo, "dockhand/jq-1.8", sha,
		[]Member{{Port: "jq", Portdir: "sysutils/jq"}},
		fake.Capabilities().Platforms[0], false)
	require.NoError(t, err)
	again, err := ledger.Open(repo).Read(ctx, sha)
	require.NoError(t, err)
	assert.Equal(t, record.ToVerdict, again.Destination)
}

// A queued run has no job behind it, so the platforms projection — which
// answers what was submitted — cannot find it. The drain walks the runs
// for exactly that reason.
func TestPumpFindsAQueuedRunThatNoJobNames(t *testing.T) {
	ctx := context.Background()
	repo, sha := engineRepo(t)
	fake := &verifytest.Fake{}
	eng := testState(t, repo, fake)
	eng.Tools = pumpTools(t)

	n := mintedNote(t, repo, sha)
	n.Runs[record.RunKey("jq", "Testos")] = record.Run{
		State: record.Queued, Platform: "Testos", Detail: "all slots busy"}
	require.NoError(t, ledger.Open(repo).Write(ctx, n))
	require.Empty(t, n.Platforms(), "the fixture must model a run with no guest behind it")

	eng.PumpDeferred(ctx, repo, []string{"dockhand/jq-1.8"})

	require.Len(t, fake.Submitted, 1, "the queued run is what the drain exists to start")
	assert.Equal(t, []string{"jq"}, fake.Submitted[0].Ports)
	again, err := ledger.Open(repo).Read(ctx, sha)
	require.NoError(t, err)
	assert.Equal(t, record.Running, runOf(again, "Testos").State)
}

// The Closes: trailer, from the plan's field to the commit's bytes.
//
// It is the whole of ruling 5: the ticket reaches the commit message,
// where the project's guidelines want it and where nothing rewrites it,
// and it reaches nothing else. The subject is still the plan's summary
// and the branch is still named after the change.
func TestMintWritesTheClosesTrailer(t *testing.T) {
	ctx := context.Background()
	repo := gittest.PortsTree(t, realTools)
	var out, errb bytes.Buffer
	eng := testEngine(t, repo, &verifytest.Fake{}, &out, &errb)

	p := bumpPlan(t, repo, "bump", "1.8")
	p.ClosesTicket = "71234"
	require.NoError(t, runPlan(t, ctx, eng, p, Policy{Destination: record.ToBranch}))

	tip, err := repo.RevParse(ctx, "dockhand/jq-1.8")
	require.NoError(t, err, "the branch is named after the change, not after the ticket")

	subject, err := repo.Subject(ctx, tip)
	require.NoError(t, err)
	assert.Equal(t, "jq: update to 1.8", subject, "a trailer is not a subject")
	assert.Equal(t, "jq: update to 1.8\n\nCloses: https://trac.macports.org/ticket/71234\n",
		commitBody(t, repo, tip), "the full URL, in the last paragraph, where git reads a trailer")

	n, err := ledger.Open(repo).Read(ctx, tip)
	require.NoError(t, err)
	assert.Equal(t, "71234", n.ClosesTicket,
		"the record carries the number so the pull request body need not be told again")
}

// A plan with no ticket mints exactly the message it always did.
func TestMintWithoutATicketIsTheSummaryAlone(t *testing.T) {
	ctx := context.Background()
	repo := gittest.PortsTree(t, realTools)
	var out, errb bytes.Buffer
	eng := testEngine(t, repo, &verifytest.Fake{}, &out, &errb)

	require.NoError(t, runPlan(t, ctx, eng, bumpPlan(t, repo, "bump", "1.8"),
		Policy{Destination: record.ToBranch}))

	tip, err := repo.RevParse(ctx, "dockhand/jq-1.8")
	require.NoError(t, err)
	assert.Equal(t, "jq: update to 1.8\n", commitBody(t, repo, tip))

	n, err := ledger.Open(repo).Read(ctx, tip)
	require.NoError(t, err)
	assert.Empty(t, n.ClosesTicket)
}

// commitBody is a commit's whole message, which Repo.Subject
// deliberately is not: the trailer is the part below the subject, so
// reading only the first line would pass whatever this writes.
func commitBody(t *testing.T, repo *git.Repo, sha string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", repo.Root, "log", "-1", "--format=%B", sha).Output()
	require.NoError(t, err)
	return string(out)
}
