package engine

// Extend, exposed and proven, with no verb in front of it. What is
// tested here is what a cohort verb will stand on: the branch advances
// under a lease, the new tip inherits the change rather than being born
// again, and the environments the old tip was holding go back.

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/git/gittest"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/verifytest"
)

// dismissedAt is a stamp a person's answer already carries, so a test
// can tell a copied disposition from a freshly proposed one.
var dismissedAt = time.Date(2026, 8, 30, 9, 0, 0, 0, time.UTC)

// twoPortRepo is a ports tree with two portdirs in it and a minted
// branch that has moved one of them.
//
// Both directories exist before the change, which is not a convenience:
// a graft writes a blob into a tree that is already there, so a cohort
// member's portdir has to be in the tree the branch is grown from — as
// every real member's is.
func twoPortRepo(t *testing.T) (*git.Repo, string) {
	t.Helper()
	repo := gittest.Init(t, realTools, "", map[string]string{
		"sysutils/jq/Portfile":        "version 1.7\n",
		"textproc/oniguruma/Portfile": "version 6.9\n",
	})
	primary, err := repo.PrimaryBranch(context.Background())
	require.NoError(t, err)
	sha := gittest.Commit(t, repo, "dockhand/jq-1.8", primary,
		"sysutils/jq/Portfile", "version 1.8\n", "jq: update to 1.8")
	return repo, sha
}

// extendable is a minted branch with a record that has been through a
// verification: subjects, riders, a ticket, a base, and two findings
// one of which a person has already answered.
func extendable(t *testing.T, repo *git.Repo, sha string) {
	t.Helper()
	ctx := context.Background()
	n, err := ledger.Open(repo).LoadOrStart(ctx, sha)
	require.NoError(t, err)
	n.Slug = "jq-1.8"
	n.Subjects = []record.Subject{{Port: "jq", Names: []string{"jq"}, Portdir: "sysutils/jq",
		Intent: "bump", Target: "1.8"}}
	n.Destination = record.ToVerdict
	n.AskedBy = record.Human
	n.MintedVia = record.MintedSingle
	n.Riders = []string{"modeline"}
	n.ClosesTicket = "12345"
	n.Base = record.Base{Sha: "beef", CommittedAt: dismissedAt}
	n.Findings = []record.Finding{
		{Kind: "abi", Ports: []string{"jq"}, Criterion: "libjq.1 moved to libjq.2",
			Candidates:  []record.Candidate{{Port: "jq-tools", Proposed: true, Reason: "links libjq"}},
			Disposition: record.Proposed},
		{Kind: "comment", Ports: []string{"jq"}, Quote: "bump the revision by hand",
			Disposition: record.Dismissed, At: dismissedAt},
	}
	require.NoError(t, ledger.Open(repo).Write(ctx, n))
}

// oneFile is the commit an extension carries: enough to be a commit,
// which is all git asks of it.
func oneFile(path, content, message string) git.Commit {
	return git.Commit{
		Files:   []git.File{{Path: path, Content: []byte(content)}},
		Message: message,
	}
}

// The whole inheritance in one pass: the branch moves to a real new
// commit whose parent is the old tip, and the record on it is the
// change carried forward rather than a change born again.
func TestExtendCarriesTheChangeOntoTheNewTip(t *testing.T) {
	ctx := context.Background()
	repo, sha := twoPortRepo(t)
	extendable(t, repo, sha)
	eng := testState(t, repo, &verifytest.Fake{})

	tip, err := eng.Extend(ctx, repo, ExtendRequest{
		Branch:      "dockhand/jq-1.8",
		ExpectedTip: sha,
		Commit:      oneFile("textproc/oniguruma/Portfile", "revision 1\n", "oniguruma: bump revision"),
		Subjects: []record.Subject{{Port: "oniguruma", Names: []string{"oniguruma"},
			Portdir: "textproc/oniguruma", Intent: "bump-revision", Target: "rev1",
			Reason: "links libjq, whose ABI moved"}},
	})
	require.NoError(t, err)
	assert.NotEqual(t, sha, tip)

	// The advance is a commit on the branch, not a new branch and not a
	// rewrite: the ref is at the new tip and the new tip's parent is the
	// old one.
	at, err := repo.RevParse(ctx, "dockhand/jq-1.8")
	require.NoError(t, err)
	assert.Equal(t, tip, at)
	parent, err := repo.RevParse(ctx, tip+"^")
	require.NoError(t, err)
	assert.Equal(t, sha, parent)

	n, err := ledger.Open(repo).Read(ctx, tip)
	require.NoError(t, err)

	// The union, with the headline still at the head: the branch is
	// named for jq and an arriving member does not rename the change.
	assert.Equal(t, []string{"jq", "oniguruma"}, n.Ports())
	assert.Equal(t, []string{"sysutils/jq", "textproc/oniguruma"}, n.Portdirs())
	assert.Equal(t, "bump-revision", n.Subjects[1].Intent)

	// Evidence points at where the findings were measured. Full sha,
	// not the abbreviation the messages print.
	require.NotNil(t, n.Evidence)
	assert.Equal(t, sha, n.Evidence.From)

	// Everything that describes the change, and nothing that describes
	// a run: the new tip has been built by nobody.
	assert.Equal(t, "jq-1.8", n.Slug)
	assert.Equal(t, record.ToVerdict, n.Destination)
	assert.Equal(t, record.Human, n.AskedBy)
	assert.Equal(t, record.MintedSingle, n.MintedVia)
	assert.Equal(t, []string{"modeline"}, n.Riders)
	assert.Equal(t, "12345", n.ClosesTicket)
	assert.Equal(t, "beef", n.Base.Sha, "the base is the change's, never re-derived from the extension")
	assert.Empty(t, n.Runs)
	assert.Empty(t, n.Jobs)
}

// A finding a person dismissed stays dismissed. Re-proposing it would
// ask them a question they have already answered, which is the whole
// reason a disposition is recorded rather than a finding deleted.
func TestExtendCopiesFindingsWithTheirDispositions(t *testing.T) {
	ctx := context.Background()
	repo, sha := twoPortRepo(t)
	extendable(t, repo, sha)
	eng := testState(t, repo, &verifytest.Fake{})

	tip, err := eng.Extend(ctx, repo, ExtendRequest{
		Branch: "dockhand/jq-1.8", ExpectedTip: sha,
		Commit: oneFile("textproc/oniguruma/Portfile", "revision 1\n", "oniguruma: bump revision"),
	})
	require.NoError(t, err)

	n, err := ledger.Open(repo).Read(ctx, tip)
	require.NoError(t, err)
	require.Len(t, n.Findings, 2)
	assert.Equal(t, record.Proposed, n.Findings[0].Disposition)
	assert.Equal(t, record.Dismissed, n.Findings[1].Disposition)
	assert.True(t, n.Findings[1].At.Equal(dismissedAt), "the answer keeps the time it was given")
	assert.Equal(t, "libjq.1 moved to libjq.2", n.Findings[0].Criterion)
	require.Len(t, n.Findings[0].Candidates, 1)
	assert.Equal(t, "jq-tools", n.Findings[0].Candidates[0].Port)

	// The candidates are a copy and not the old record's own slice: the
	// verb that answers a finding will mutate one of these.
	old, err := ledger.Open(repo).Read(ctx, sha)
	require.NoError(t, err)
	n.Findings[0].Candidates[0].Proposed = false
	assert.True(t, old.Findings[0].Candidates[0].Proposed)
}

// Two sessions extending one branch must not both win. The loser is
// told where the branch actually is, and changes nothing.
func TestExtendRefusesTheSessionWhoseTipMoved(t *testing.T) {
	ctx := context.Background()
	repo, sha := twoPortRepo(t)
	extendable(t, repo, sha)
	eng := testState(t, repo, &verifytest.Fake{})

	winner, err := eng.Extend(ctx, repo, ExtendRequest{
		Branch: "dockhand/jq-1.8", ExpectedTip: sha,
		Commit: oneFile("textproc/oniguruma/Portfile", "revision 1\n", "oniguruma: bump revision"),
	})
	require.NoError(t, err)

	// The second session still holds the tip it read before the first
	// one committed.
	_, err = eng.Extend(ctx, repo, ExtendRequest{
		Branch: "dockhand/jq-1.8", ExpectedTip: sha,
		Commit: oneFile("textproc/oniguruma/Portfile", "revision 2\n", "oniguruma: bump revision twice"),
	})
	require.ErrorIs(t, err, git.ErrTipMoved)
	assert.Contains(t, err.Error(), git.Abbrev(winner), "the refusal says where the branch actually is")

	at, err := repo.RevParse(ctx, "dockhand/jq-1.8")
	require.NoError(t, err)
	assert.Equal(t, winner, at, "the loser moved nothing")
	_, err = ledger.Open(repo).Read(ctx, winner)
	require.NoError(t, err, "and the winner's record is the one that stands")
}

// A commit that records nothing is refused before the lease is spent:
// the guard is in front of the ref read, so a no-op extension cannot
// even lose a race it should never have entered.
func TestExtendRefusesACommitThatRecordsNothing(t *testing.T) {
	ctx := context.Background()
	repo, sha := twoPortRepo(t)
	extendable(t, repo, sha)
	eng := testState(t, repo, &verifytest.Fake{})

	_, err := eng.Extend(ctx, repo, ExtendRequest{
		Branch: "dockhand/jq-1.8", ExpectedTip: sha,
		Commit: git.Commit{Message: "nothing at all"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "a commit is at least one file")
	at, err := repo.RevParse(ctx, "dockhand/jq-1.8")
	require.NoError(t, err)
	assert.Equal(t, sha, at)
}

// The old tip's environments go back, and its passes do not. A run
// still building is building the wrong commit; a failed run's kept
// environment documents code the branch has moved past. A pass is what
// Evidence.From has just named as this record's evidence, so
// superseding it would erase what the new record points at.
func TestExtendSupersedesTheOldTipsRunsAndLeavesItsPasses(t *testing.T) {
	ctx := context.Background()
	repo, sha := twoPortRepo(t)
	extendable(t, repo, sha)

	n, err := ledger.Open(repo).LoadOrStart(ctx, sha)
	require.NoError(t, err)
	n.Subjects = []record.Subject{{Port: "jq", Names: []string{"jq"}, Portdir: "sysutils/jq"}}
	n.Jobs["Testos"] = record.JobRecord{Job: verify.Job{Provider: "fake", ID: "fake-1"}}
	n.Jobs["Sequoia"] = record.JobRecord{Job: verify.Job{Provider: "fake", ID: "fake-2"}, Handle: "fake-2"}
	n.Jobs["Sonoma"] = record.JobRecord{Job: verify.Job{Provider: "fake", ID: "fake-3"}}
	n.Runs[record.RunKey("jq", "Testos")] = record.Run{State: record.Running, Platform: "Testos"}
	n.Runs[record.RunKey("jq", "Sequoia")] = record.Run{State: record.Failed, Platform: "Sequoia",
		Detail: "Failed to build jq"}
	n.Runs[record.RunKey("jq", "Sonoma")] = record.Run{State: record.Passed, Platform: "Sonoma"}
	require.NoError(t, ledger.Open(repo).Write(ctx, n))

	fake := &verifytest.Fake{}
	tip, err := testState(t, repo, fake).Extend(ctx, repo, ExtendRequest{
		Branch: "dockhand/jq-1.8", ExpectedTip: sha,
		Commit: oneFile("textproc/oniguruma/Portfile", "revision 1\n", "oniguruma: bump revision"),
	})
	require.NoError(t, err)

	assert.ElementsMatch(t, []string{"fake-1", "fake-2"}, fake.Released,
		"the running build and the kept debug environment; the pass held nothing")

	old, err := ledger.Open(repo).Read(ctx, sha)
	require.NoError(t, err)
	assert.Equal(t, record.Superseded, runFor(old, "jq", "Testos").State)
	assert.Contains(t, runFor(old, "jq", "Testos").Detail, git.Abbrev(tip))
	assert.Equal(t, record.Superseded, runFor(old, "jq", "Sequoia").State)
	assert.Equal(t, record.Passed, runFor(old, "jq", "Sonoma").State,
		"the evidence the new tip inherited is still standing where it was earned")
	assert.False(t, old.Jobs["Sonoma"].Released)
}

// A sweep that fails still names what was made. By the time the old
// tip's environments are asked for, the commit is written, the ref is
// advanced under the lease and the new record is complete — so an error
// that came back with an empty tip would leave a caller reporting a
// failure it cannot name, and retrying from a tip that has already
// moved would be refused as a moved lease.
func TestExtendNamesTheTipWhenTheStaleSweepFails(t *testing.T) {
	ctx := context.Background()
	repo, sha := twoPortRepo(t)
	extendable(t, repo, sha)

	n, err := ledger.Open(repo).LoadOrStart(ctx, sha)
	require.NoError(t, err)
	n.Subjects = []record.Subject{{Port: "jq", Names: []string{"jq"}, Portdir: "sysutils/jq"}}
	n.Jobs["Testos"] = record.JobRecord{Job: verify.Job{Provider: "fake", ID: "fake-1"}}
	n.Runs[record.RunKey("jq", "Testos")] = record.Run{State: record.Running, Platform: "Testos"}
	require.NoError(t, ledger.Open(repo).Write(ctx, n))

	// No provider wired in, so the sweep cannot ask anyone to release
	// the environment the old tip is still holding.
	tip, err := testState(t, repo, nil).Extend(ctx, repo, ExtendRequest{
		Branch: "dockhand/jq-1.8", ExpectedTip: sha,
		Commit: oneFile("textproc/oniguruma/Portfile", "revision 1\n", "oniguruma: bump revision"),
	})
	require.Error(t, err)
	require.NotEmpty(t, tip, "the commit landed, and a caller must be able to say which")
	assert.Contains(t, err.Error(), git.Abbrev(tip))
	assert.Contains(t, err.Error(), "environments were not released")

	at, err := repo.RevParse(ctx, "dockhand/jq-1.8")
	require.NoError(t, err)
	assert.Equal(t, tip, at, "the branch stands at the tip the error named")
	grown, err := ledger.Open(repo).Read(ctx, tip)
	require.NoError(t, err)
	assert.Equal(t, sha, grown.Evidence.From, "and that tip carries its record")
}

// An answer written onto the old tip while the commit was being made
// is carried across. The lease closes the race on the ref; it does not
// close this one, which is two git subprocesses wide — and a finding a
// person dismissed that came back proposed on the next commit would
// ask them a question they have already answered.
func TestExtendCarriesAnAnswerWrittenWhileItWasCommitting(t *testing.T) {
	ctx := context.Background()
	repo, sha := twoPortRepo(t)
	extendable(t, repo, sha)

	n, err := ledger.Open(repo).LoadOrStart(ctx, sha)
	require.NoError(t, err)
	n.Findings = []record.Finding{{Kind: "abi", Ports: []string{"jq"},
		Criterion: "libjq.1 moved to libjq.2", Disposition: record.Proposed}}
	require.NoError(t, ledger.Open(repo).Write(ctx, n))

	// The notes lock is what makes the window observable. Held here,
	// the extend can commit and advance the ref — neither takes it —
	// and then waits for it before writing the new tip's record, which
	// is exactly the moment a peer's answer lands.
	unlock, err := repo.LockNotes(ctx)
	require.NoError(t, err)

	type outcome struct {
		tip string
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		tip, err := testState(t, repo, &verifytest.Fake{}).Extend(ctx, repo, ExtendRequest{
			Branch: "dockhand/jq-1.8", ExpectedTip: sha,
			Commit: oneFile("textproc/oniguruma/Portfile", "revision 1\n", "oniguruma: bump revision"),
		})
		done <- outcome{tip, err}
	}()

	// The branch moving is the proof that the commit is made and any
	// read taken before it is stale.
	require.Eventually(t, func() bool {
		at, err := repo.RevParse(ctx, "dockhand/jq-1.8")
		return err == nil && at != sha
	}, 30*time.Second, 10*time.Millisecond, "the extend must reach its commit")

	// The peer, writing under the lock this test is holding — which is
	// what ledger.Write is for.
	old, err := ledger.Open(repo).Read(ctx, sha)
	require.NoError(t, err)
	old.Findings[0].Disposition = record.Dismissed
	require.NoError(t, ledger.Open(repo).Write(ctx, old))
	unlock()

	got := <-done
	require.NoError(t, got.err)
	grown, err := ledger.Open(repo).Read(ctx, got.tip)
	require.NoError(t, err)
	require.Len(t, grown.Findings, 1)
	assert.Equal(t, record.Dismissed, grown.Findings[0].Disposition,
		"the record the new tip is built from is the one read under the lock")
}

// A branch with no record at all still extends: the new tip gets a
// record of its own naming the members it was given, and Evidence
// still says where it came from even though there was nothing to
// inherit.
func TestExtendWorksOnABranchWithNoRecord(t *testing.T) {
	ctx := context.Background()
	repo, sha := twoPortRepo(t)
	eng := testState(t, repo, &verifytest.Fake{})

	tip, err := eng.Extend(ctx, repo, ExtendRequest{
		Branch: "dockhand/jq-1.8", ExpectedTip: sha,
		Commit:   oneFile("textproc/oniguruma/Portfile", "revision 1\n", "oniguruma: bump revision"),
		Subjects: []record.Subject{{Port: "oniguruma", Portdir: "textproc/oniguruma"}},
	})
	require.NoError(t, err)

	n, err := ledger.Open(repo).Read(ctx, tip)
	require.NoError(t, err)
	assert.Equal(t, []string{"oniguruma"}, n.Ports())
	require.NotNil(t, n.Evidence)
	assert.Equal(t, sha, n.Evidence.From)
}

// A subject the ledger adopted from a run key carries a port and
// nothing else. An extension that knows where it lives may say so —
// that takes nothing away — but it may not overwrite what a mint
// already stated.
func TestExtendFillsABlankSubjectAndOverwritesNothingStated(t *testing.T) {
	ctx := context.Background()
	repo, sha := twoPortRepo(t)
	n, err := ledger.Open(repo).LoadOrStart(ctx, sha)
	require.NoError(t, err)
	n.Subjects = []record.Subject{
		{Port: "jq", Names: []string{"jq"}, Portdir: "sysutils/jq", Intent: "bump", Target: "1.8"},
		{Port: "oniguruma"},
	}
	require.NoError(t, ledger.Open(repo).Write(ctx, n))

	tip, err := testState(t, repo, &verifytest.Fake{}).Extend(ctx, repo, ExtendRequest{
		Branch: "dockhand/jq-1.8", ExpectedTip: sha,
		Commit: oneFile("textproc/oniguruma/Portfile", "revision 1\n", "oniguruma: bump revision"),
		Subjects: []record.Subject{
			{Port: "jq", Portdir: "elsewhere/jq", Intent: "refresh"},
			{Port: "oniguruma", Portdir: "textproc/oniguruma", Intent: "bump-revision"},
		},
	})
	require.NoError(t, err)

	got, err := ledger.Open(repo).Read(ctx, tip)
	require.NoError(t, err)
	require.Len(t, got.Subjects, 2)
	assert.Equal(t, "sysutils/jq", got.Subjects[0].Portdir, "the minted copy is the good one")
	assert.Equal(t, "bump", got.Subjects[0].Intent)
	assert.Equal(t, "textproc/oniguruma", got.Subjects[1].Portdir, "and a blank is filled rather than left")
	assert.Equal(t, "bump-revision", got.Subjects[1].Intent)
}
