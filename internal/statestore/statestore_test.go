package statestore

import (
	"context"
	"errors"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/git/gittest"
	"github.com/herbygillot/dockhand/internal/lockfile"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/tool"
)

// tools is the finder every fixture opens with: the real PATH search,
// because the git under test is the real one. This package drives real
// git for the same reason internal/git and internal/ledger do — what is
// being proven here is what git does with a batch, an empty tree and a
// compare-and-set, and a fake would prove something about the fake.
var tools = tool.NewFinder(nil)

// newStore is a ports-tree-shaped repository and a store over it, with
// no state ref yet: the state every checkout is in before dockhand has
// run once.
func newStore(t *testing.T) (*git.Repo, *Store) {
	t.Helper()
	repo := gittest.PortsTree(t, tools)
	return repo, Open(repo)
}

// plant runs git straight against the repository, outside every verb
// this package uses, so a test can be the writer who never took the
// lock — which is the only writer the compare-and-set is still there
// for.
func plant(t *testing.T, repo *git.Repo, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo.Root}, args...)...)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
	return string(out)
}

// refValue is what a ref holds according to git itself, absence
// included, kept independent of this package's own reading so that a
// bug in the store cannot make its own assertion pass.
func refValue(t *testing.T, repo *git.Repo, ref string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", repo.Root, "rev-parse", "--verify", "--quiet", ref).Output()
	if err != nil {
		return ""
	}
	return string(out[:len(out)-1])
}

// aChange is the smallest change record a test needs: an id, a state,
// and the commit it is bound to.
func aChange(id, tip string) record.Change {
	return record.Change{ID: record.ChangeID(id), State: record.ChangeMinted, Tip: tip}
}

// THE FIRST WRITE. A repository that has never run dockhand has no ref
// and no tree to graft onto, so the first amend is the one path allowed
// to swallow ErrNoState — and it lands over git's own empty tree with a
// create line, which is what refuses a peer that got there first.
func TestAmendCreatesTheRefInARepositoryThatHasNeverRunDockhand(t *testing.T) {
	repo, store := newStore(t)
	ctx := context.Background()
	require.Empty(t, refValue(t, repo, Ref), "the fixture starts with no state ref")

	require.NoError(t, store.Amend(ctx, func(tx *Txn) error {
		tx.PutChange(aChange("chg-1", "deadbeef"))
		return nil
	}))

	at := refValue(t, repo, Ref)
	require.NotEmpty(t, at, "the amend created the ref")

	st, err := store.Read(ctx)
	require.NoError(t, err)
	assert.Equal(t, at, st.At, "the state names the commit it was read from")
	require.Contains(t, st.Changes, "chg-1")
	assert.Equal(t, record.DocSchema, st.Changes["chg-1"].Schema, "the Put stamps the schema rather than trusting the caller")
	assert.Equal(t, "deadbeef", st.Changes["chg-1"].Tip)

	// The first commit is a root commit: there was nothing to be a
	// parent of it.
	parents := plant(t, repo, "rev-list", "--parents", "-1", Ref)
	assert.Equal(t, at+"\n", parents, "the first state commit has no parent")
}

// RULE 7, THE HEADLINE CASE. "dockhand has never run here" and
// "dockhand has run here and nothing is in flight" are the same empty
// value unless the store keeps them apart, and the difference matters
// most to the passes that destroy provider resources on the strength of
// an empty read.
func TestReadTellsAnAbsentRefFromAnEmptyOne(t *testing.T) {
	_, store := newStore(t)
	ctx := context.Background()

	_, err := store.Read(ctx)
	require.ErrorIs(t, err, ErrNoState)

	require.NoError(t, store.Amend(ctx, func(tx *Txn) error { return nil }))

	st, err := store.Read(ctx)
	require.NoError(t, err, "a ref holding nothing is an answer, not an absence")
	assert.NotEmpty(t, st.At)
	assert.Empty(t, st.Changes)
	assert.Empty(t, st.Attempts)
	assert.Empty(t, st.Leases)
	assert.Empty(t, st.Publications)
}

// The four kinds ride one flat tree with the kind in the name, and they
// come back as themselves. The ids carry hyphens on purpose: a reader
// that split a name at the first one would file half of every id under
// the wrong kind.
func TestReadReturnsAllFourKinds(t *testing.T) {
	repo, store := newStore(t)
	ctx := context.Background()
	started := time.Now().UTC().Truncate(time.Second)

	require.NoError(t, store.Amend(ctx, func(tx *Txn) error {
		tx.PutChange(aChange("chg-01HZ-a", "abc"))
		tx.PutAttempt(record.Attempt{ID: "att-01HZ-b", Change: "chg-01HZ-a", Sha: "abc", Platform: "sequoia", Started: started, Phase: record.Requested})
		tx.PutLease(record.Lease{Request: "req-01HZ-c", Change: "chg-01HZ-a", Platform: "sequoia", Phase: record.Active})
		tx.PutPublication(record.Publication{ID: "pub-01HZ-d", Change: "chg-01HZ-a", Outcome: record.Open})
		return nil
	}))

	st, err := store.Read(ctx)
	require.NoError(t, err)
	assert.Contains(t, st.Changes, "chg-01HZ-a")
	assert.Contains(t, st.Attempts, "att-01HZ-b")
	assert.Contains(t, st.Leases, "req-01HZ-c", "a lease is keyed by the request token recovery joins on")
	assert.Contains(t, st.Publications, "pub-01HZ-d")
	assert.Equal(t, started, st.Attempts["att-01HZ-b"].Started.UTC(), "the record round-trips through the tree")

	// The tree is FLAT, with the kind in the name, because GraftTree
	// refuses a path whose parent directory is not already in the base
	// tree — a directory layout would make each kind's first record
	// permanently unwritable.
	names := plant(t, repo, "ls-tree", "--name-only", Ref)
	assert.Contains(t, names, "change-chg-01HZ-a.json")
	assert.Contains(t, names, "attempt-att-01HZ-b.json")
	assert.Contains(t, names, "lease-req-01HZ-c.json")
	assert.Contains(t, names, "publication-pub-01HZ-d.json")
}

// A record put in one mutator is visible to the next in the same
// closure, which is what makes the order of mutators inside one
// transaction a real order.
func TestTxnStateShowsWhatTheClosureHasAlreadyPut(t *testing.T) {
	_, store := newStore(t)

	require.NoError(t, store.Amend(context.Background(), func(tx *Txn) error {
		tx.PutChange(aChange("chg-1", "abc"))
		c, ok := tx.State().Changes["chg-1"]
		require.True(t, ok, "the put is visible to the next mutator")
		c.Tip = "def"
		tx.PutChange(c)
		return nil
	}))

	st, err := store.Read(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "def", st.Changes["chg-1"].Tip, "the last word in the closure is the one the tree gets")
}

// THE RECORD AND THE REF LAND IN ONE ACT. That is the whole of R23: the
// branch a mint creates is a line in the same batch as the record that
// explains it, so a crash between them is not a state.
func TestAmendCarriesTheRefLinesTheClosureQueued(t *testing.T) {
	repo, store := newStore(t)
	ctx := context.Background()
	head, err := repo.RevParse(ctx, "HEAD")
	require.NoError(t, err)

	require.NoError(t, store.Amend(ctx, func(tx *Txn) error {
		tx.PutChange(aChange("chg-1", head))
		if err := tx.Ref("refs/heads/dockhand/jq-1.8", head, ""); err != nil {
			return err
		}
		return tx.Ref("refs/dockhand/verify/chg-1", head, "")
	}))

	assert.Equal(t, head, refValue(t, repo, "refs/heads/dockhand/jq-1.8"))
	assert.Equal(t, head, refValue(t, repo, "refs/dockhand/verify/chg-1"))
	st, err := store.Read(ctx)
	require.NoError(t, err)
	assert.Contains(t, st.Changes, "chg-1")
}

// The store owns three names and no others. A mutator that could queue
// a line for refs/heads/master is a store that moves a person's branch.
func TestRefRefusesEveryNameTheStoreDoesNotOwn(t *testing.T) {
	_, store := newStore(t)

	for _, name := range []string{
		"refs/heads/master",
		"refs/heads/dockhand-hidden/jq",
		"refs/dockhand/verify",  // the namespace itself is not a ref
		"refs/dockhand/verify/", // and neither is one with no id
		"refs/notes/dockhand/verify",
		"refs/remotes/origin/main",
		Ref, // only Amend's own first line may name the state ref
	} {
		t.Run(name, func(t *testing.T) {
			err := store.Amend(context.Background(), func(tx *Txn) error {
				return tx.Ref(name, "abc", "")
			})
			require.ErrorIs(t, err, ErrForeignRef)
			require.ErrorContains(t, err, name)
		})
	}
}

// A line that names neither a value nor an expectation is refused where
// the caller can still act on it, and under git's own sentinel: an empty
// expected value must never read as "no check".
func TestRefRefusesALineWithNeitherValue(t *testing.T) {
	_, store := newStore(t)

	err := store.Amend(context.Background(), func(tx *Txn) error {
		return tx.Ref("refs/heads/dockhand/jq", "", "")
	})
	require.ErrorIs(t, err, git.ErrBadUpdate)
}

// THE ONE COALESCING RULE. `bump --replace` of a same-version bump
// re-mints the same branch name in the transaction that supersedes the
// old change, and git refuses two lines for one ref in a batch. The
// delete's expected-old is kept, so no assertion is weakened.
func TestRefCoalescesADeleteThenACreateOfOneName(t *testing.T) {
	repo, store := newStore(t)
	ctx := context.Background()
	const branch = "refs/heads/dockhand/jq-1.8"
	old := gittest.Commit(t, repo, "dockhand/jq-1.8", "HEAD", "sysutils/jq/Portfile", "version 1.8\n", "jq: 1.8")
	head, err := repo.RevParse(ctx, "HEAD")
	require.NoError(t, err)

	require.NoError(t, store.Amend(ctx, func(tx *Txn) error {
		if err := tx.Ref(branch, "", old); err != nil {
			return err
		}
		return tx.Ref(branch, head, "")
	}))
	assert.Equal(t, head, refValue(t, repo, branch), "the two lines became one update")

	// And the kept expectation still bites: the same pair against a
	// branch that has moved is refused as the foreign move it is.
	plant(t, repo, "update-ref", branch, old)
	err = store.Amend(ctx, func(tx *Txn) error {
		if err := tx.Ref(branch, "", head); err != nil {
			return err
		}
		return tx.Ref(branch, old, "")
	})
	require.ErrorIs(t, err, git.ErrRefMoved)
	assert.Equal(t, old, refValue(t, repo, branch))
}

// Any OTHER second line for one name is the closure's own bug, and it
// is refused before a batch is built rather than by git afterwards.
func TestRefRefusesAnUncoalescableDuplicate(t *testing.T) {
	_, store := newStore(t)

	err := store.Amend(context.Background(), func(tx *Txn) error {
		if err := tx.Ref("refs/heads/dockhand/jq", "abc", ""); err != nil {
			return err
		}
		return tx.Ref("refs/heads/dockhand/jq", "def", "")
	})
	require.ErrorIs(t, err, git.ErrBadUpdate)
}

// ALL OR NOTHING. A foreign hand that moved a branch between the
// closure's read and the commit takes the whole batch down — the state
// ref does not move, the record is not written — and the refusal is
// returned as the *git.RefMoved it is, NOT retried: the closure would
// derive the same expected-old from the same record and lose the same
// way.
func TestAmendRollsTheWholeBatchBackOnAForeignMove(t *testing.T) {
	repo, store := newStore(t)
	ctx := context.Background()
	const branch = "refs/heads/dockhand/jq-1.8"
	tip := gittest.Commit(t, repo, "dockhand/jq-1.8", "HEAD", "sysutils/jq/Portfile", "version 1.8\n", "jq: 1.8")
	head, err := repo.RevParse(ctx, "HEAD")
	require.NoError(t, err)
	require.NoError(t, store.Amend(ctx, func(tx *Txn) error {
		tx.PutChange(aChange("chg-1", tip))
		return nil
	}))
	before := refValue(t, repo, Ref)

	// A person's own `git branch -f` between the read and the commit.
	plant(t, repo, "update-ref", branch, head)

	runs := 0
	err = store.Amend(ctx, func(tx *Txn) error {
		runs++
		c := tx.State().Changes["chg-1"]
		c.State = record.ChangeDiscarded
		tx.PutChange(c)
		return tx.Ref(branch, "", tip) // demolish, expecting the tip the record holds
	})

	var moved *git.RefMoved
	require.ErrorAs(t, err, &moved)
	require.ErrorIs(t, err, git.ErrRefMoved)
	require.NotErrorIs(t, err, ErrConcurrent, "a foreign hand is not a race")
	assert.Equal(t, branch, moved.Ref, "the refusal names the ref that moved")
	assert.Equal(t, 1, runs, "a foreign move is not retried")
	assert.Equal(t, before, refValue(t, repo, Ref), "the state ref did not move")
	assert.Equal(t, head, refValue(t, repo, branch), "the branch was not deleted")

	st, err := store.Read(ctx)
	require.NoError(t, err)
	assert.Equal(t, record.ChangeMinted, st.Changes["chg-1"].State, "the record did not change either")
}

// THE LOST RACE, which under the flock takes a writer who never took
// it: a stray `git update-ref`, an older build, a future tool. The
// closure is run AGAIN over a fresh read, which is why it must be a pure
// function of the state it is handed, and the peer's write survives.
func TestAmendRetriesWhenTheStateRefLostItsRace(t *testing.T) {
	repo, store := newStore(t)
	ctx := context.Background()
	require.NoError(t, store.Amend(ctx, func(tx *Txn) error {
		tx.PutChange(aChange("chg-1", "abc"))
		return nil
	}))

	runs := 0
	require.NoError(t, store.Amend(ctx, func(tx *Txn) error {
		runs++
		if runs == 1 {
			// A peer commits over the state the closure was handed,
			// without ever taking the lock.
			peerTree, err := repo.GraftTree(ctx, Ref, []git.File{{Path: "change-chg-2.json", Content: []byte(`{"schema":1,"id":"chg-2","state":"minted","content":""}` + "\n")}})
			require.NoError(t, err)
			peer, err := repo.CommitTree(ctx, peerTree, []string{tx.State().At}, "peer\n")
			require.NoError(t, err)
			plant(t, repo, "update-ref", Ref, peer)
		}
		c := tx.State().Changes["chg-1"]
		c.State = record.ChangePublished
		tx.PutChange(c)
		return nil
	}))

	assert.Equal(t, 2, runs, "the closure ran again over a fresh read")
	st, err := store.Read(ctx)
	require.NoError(t, err)
	assert.Equal(t, record.ChangePublished, st.Changes["chg-1"].State, "this writer's change landed")
	assert.Contains(t, st.Changes, "chg-2", "and the peer's write was not clobbered")
}

// A peer that writes in a loop without the lock is a bounded wait and
// then a named refusal, never a hang.
func TestAmendGivesUpOnAPeerThatNeverStops(t *testing.T) {
	repo, store := newStore(t)
	ctx := context.Background()
	require.NoError(t, store.Amend(ctx, func(tx *Txn) error { return nil }))

	runs := 0
	err := store.Amend(ctx, func(tx *Txn) error {
		runs++
		peer, cerr := repo.CommitTree(ctx, EmptyTree, []string{tx.State().At}, "peer\n")
		require.NoError(t, cerr)
		plant(t, repo, "update-ref", Ref, peer)
		tx.PutChange(aChange("chg-1", "abc"))
		return nil
	})
	require.ErrorIs(t, err, ErrConcurrent)
	assert.Equal(t, amendTries, runs, "bounded, then returned")
}

// A closure's own error aborts the transaction and moves nothing.
func TestAmendWritesNothingWhenTheClosureRefuses(t *testing.T) {
	repo, store := newStore(t)
	ctx := context.Background()
	sentinel := errors.New("the mutator said no")

	err := store.Amend(ctx, func(tx *Txn) error {
		tx.PutChange(aChange("chg-1", "abc"))
		return sentinel
	})
	require.ErrorIs(t, err, sentinel)
	assert.Empty(t, refValue(t, repo, Ref), "nothing was written")
}

// THE LOCK IS THE OTHER HALF OF THE RULING. A contended writer gets
// lockfile.ErrHeld — a named refusal a caller can report — rather than
// racing in silence and leaving unreachable objects behind.
func TestAmendRefusesWhileAPeerHoldsTheLedgerLock(t *testing.T) {
	repo, store := newStore(t)
	ctx := context.Background()
	path, err := repo.CommonDirFile(ctx, Lock)
	require.NoError(t, err)
	unlock, err := lockfile.Acquire(ctx, path, 0)
	require.NoError(t, err)
	t.Cleanup(unlock)

	// The real deadline is thirty seconds; the property is the refusal,
	// not the waiting.
	restore := lockDeadline
	lockDeadline = 50 * time.Millisecond
	t.Cleanup(func() { lockDeadline = restore })

	ran := false
	err = store.Amend(ctx, func(tx *Txn) error { ran = true; return nil })
	require.ErrorIs(t, err, lockfile.ErrHeld)
	assert.False(t, ran, "the closure never ran")
	assert.Empty(t, refValue(t, repo, Ref))
}

// The lock is the LEDGER's, one for the state ref and the note export
// together, and it resolves through GIT_COMMON_DIR so that linked
// worktrees of one repository share it as they share the ref.
func TestTheLockLivesInTheCommonGitDir(t *testing.T) {
	repo, _ := newStore(t)
	path, err := repo.CommonDirFile(context.Background(), Lock)
	require.NoError(t, err)
	assert.Contains(t, path, ".git/"+Lock)
}

// refs/dockhand gets no reflog under any other value, so the ref that is
// the authority for live leases would be the one ref in the repository
// with no recovery path.
func TestAmendEnsuresTheReflogSetting(t *testing.T) {
	repo, store := newStore(t)
	ctx := context.Background()
	// `git init` writes `logallrefupdates = true`, which is exactly the
	// value the design says is not enough: it covers refs/heads,
	// refs/remotes, refs/notes and HEAD, and not refs/dockhand.
	before, set, err := repo.Config(ctx, "core.logAllRefUpdates")
	require.NoError(t, err)
	require.True(t, set)
	require.NotEqual(t, "always", before, "the fixture starts on the value that does not cover refs/dockhand")

	require.NoError(t, store.Amend(ctx, func(tx *Txn) error {
		tx.PutChange(aChange("chg-1", "abc"))
		return nil
	}))

	value, set, err := repo.Config(ctx, "core.logAllRefUpdates")
	require.NoError(t, err)
	require.True(t, set)
	assert.Equal(t, "always", value)
	assert.Contains(t, plant(t, repo, "reflog", "show", Ref), "dockhand", "the state ref now has a reflog to recover from")
}

// THE TRIPWIRE, AND IT IS A REFUSAL. encoding/json ignores fields it
// does not know and zero-fills the ones it is missing, so a document
// from before a shape change reads SILENTLY and answers wrongly — a
// lease with no owner, a crossing that reads as unknown. DocSchema turns
// that into something a person can act on, and the remedy is to recreate
// the ref, never to migrate it in place.
func TestReadRefusesADocumentFromAnotherShape(t *testing.T) {
	repo, store := newStore(t)
	ctx := context.Background()
	require.NoError(t, store.Amend(ctx, func(tx *Txn) error {
		tx.PutChange(aChange("chg-1", "abc"))
		return nil
	}))

	for name, body := range map[string]string{
		"change-chg-2.json": `{"schema":99,"id":"chg-2","state":"minted","content":""}`,
		"souvenir.txt":      "a hand left this here\n",
	} {
		t.Run(name, func(t *testing.T) {
			tree, err := repo.GraftTree(ctx, Ref, []git.File{{Path: name, Content: []byte(body)}})
			require.NoError(t, err)
			at, err := repo.RevParse(ctx, Ref)
			require.NoError(t, err)
			commit, err := repo.CommitTree(ctx, tree, []string{at}, "a foreign shape\n")
			require.NoError(t, err)
			plant(t, repo, "update-ref", Ref, commit)
			t.Cleanup(func() { plant(t, repo, "update-ref", Ref, at) })

			_, err = store.Read(ctx)
			require.ErrorIs(t, err, ErrDocShape)
			require.ErrorContains(t, err, name)

			// And an Amend refuses too, because every Amend begins with
			// this read: a foreign document is never written over.
			require.ErrorIs(t, store.Amend(ctx, func(tx *Txn) error { return nil }), ErrDocShape)
		})
	}
}

// A record whose id cannot be a flat tree entry is refused with the
// whole transaction, because a silent skip would write every other
// record of a closure that had already lost one.
func TestAmendRefusesARecordThatCannotBeNamed(t *testing.T) {
	repo, store := newStore(t)
	ctx := context.Background()

	for _, id := range []string{"", "sub/dir"} {
		err := store.Amend(ctx, func(tx *Txn) error {
			tx.PutChange(aChange(id, "abc"))
			return nil
		})
		require.Error(t, err)
		assert.Empty(t, refValue(t, repo, Ref), "nothing was written")
	}
}

// Owed and Live are the two questions a reclaim pass asks, and they are
// asked of the record rather than of the machine.
func TestOwedAndLiveReadTheLeases(t *testing.T) {
	me := record.OwnerID{Root: "/ports", Host: "h", PID: 4821, Since: time.Now().UTC()}
	// A JSON round trip is what a real read gives back, and it is what
	// breaks == on a time.Time: the monotonic reading does not survive.
	them := record.OwnerID{Root: "/ports", Host: "other", PID: 22, Since: me.Since}
	done := time.Now().UTC()

	st := newState("abc")
	st.Leases["held"] = record.Lease{Request: "held", Owner: me, Platform: "sequoia"}
	st.Leases["owed"] = record.Lease{Request: "owed", Owner: me, Release: &record.Release{Requested: done}}
	st.Leases["theirs"] = record.Lease{Request: "theirs", Owner: them, Release: &record.Release{Requested: done}}
	st.Leases["back"] = record.Lease{Request: "back", Owner: me, Release: &record.Release{Requested: done, Done: &done}}

	owed := st.Owed(me)
	require.Len(t, owed, 1, "a foreign obligation is reported elsewhere and never seized here")
	assert.Equal(t, "owed", owed[0].Request)

	live := []string{}
	for _, l := range st.Live() {
		live = append(live, l.Request)
	}
	assert.ElementsMatch(t, []string{"held", "owed", "theirs"}, live,
		"an environment is in use until the provider confirms it back")
}

// Ownership is compared field by field, because a time.Time decoded
// from JSON and one built in this process name the same instant and
// differ under ==.
func TestOwedMatchesAnOwnerThatCameBackThroughTheTree(t *testing.T) {
	_, store := newStore(t)
	ctx := context.Background()
	me := record.OwnerID{Root: "/ports", Host: "h", PID: 4821, Since: time.Now()}
	done := time.Now()

	require.NoError(t, store.Amend(ctx, func(tx *Txn) error {
		tx.PutLease(record.Lease{Request: "req-1", Owner: me, Release: &record.Release{Requested: done}})
		return nil
	}))

	st, err := store.Read(ctx)
	require.NoError(t, err)
	require.Len(t, st.Owed(me), 1, "this checkout's own obligation is its own after a round trip")
}
