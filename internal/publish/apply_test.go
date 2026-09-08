package publish

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/change"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/git/gittest"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/statestore"
)

// scriptedForge is a gh seam with no gh behind it: the argv goes in a
// log a test can assert about, and the answers are the ones GitHub would
// give. It is a func and not a struct because gh.Runner is a func — the
// seam's whole point is that a test hands in a scripted GitHub without
// mutating a global.
func scriptedForge(log *[]string, answer func(args []string) (string, error)) func(ctx context.Context, args ...string) (string, error) {
	return func(_ context.Context, args ...string) (string, error) {
		*log = append(*log, strings.Join(args, " "))
		return answer(args)
	}
}

// published is a repository shaped like a promotion: a ports tree, a
// bare fork with the two remotes a promoted branch has, and a minted
// branch that has not been pushed.
func published(t *testing.T) (*git.Repo, *statestore.Store, string) {
	t.Helper()
	repo := gittest.PortsTree(t, tools)
	gittest.BareFork(t, repo, "me", "fork")
	head, err := repo.RevParse(t.Context(), "HEAD")
	require.NoError(t, err)
	sha := gittest.Commit(t, repo, "dockhand/jq-1.8", head,
		"sysutils/jq/Portfile", "version 1.8\n", "jq: update to 1.8")
	st := statestore.Open(repo)
	// THE CHANGE RECORD IS PLANTED, because in production one always
	// stands: a permit exists only because Gather read the change it is
	// over, and Apply re-reads it at the effect boundary to notice a
	// hold, a closure or a supersession that landed in the window. A
	// fixture with no record was a fixture no production road produces.
	require.NoError(t, st.Amend(t.Context(), func(tx *statestore.Txn) error {
		tx.PutChange(record.Change{
			ID: "chg-1", State: record.ChangeMinted, Branch: "dockhand/jq-1.8",
			Tip: sha, Content: "tree-1", Destination: record.ToPublished,
		})
		return nil
	}))
	return repo, st, sha
}

// EACH STEP IS RECORDED BEFORE IT IS ATTEMPTED AND ITS OUTCOME AFTER, so
// a crash between the push and the pull request leaves a publication
// whose push is known to have happened rather than nothing at all. This
// walks the whole permit against a real fork and a scripted forge and
// then reads the row back.
func TestApplyRecordsEachStepBeforeItPerformsIt(t *testing.T) {
	repo, st, sha := published(t)
	var log []string
	forge := scriptedForge(&log, func(args []string) (string, error) {
		switch {
		case args[0] == "api":
			return "[]", nil // no pull request for this head yet
		case args[0] == "pr" && args[1] == "create":
			return "https://github.com/macports/macports-ports/pull/1234\n", nil
		}
		return "", nil
	})
	env := Env{Repo: repo, State: st, Forge: forge, Version: "1.2.3"}

	f := facts(func(f *Facts) {
		f.Tip, f.Change.Tip, f.Own = sha, sha, []string{sha}
		f.Attempts[0].Sha = sha
	})
	p, _, err := Authorize(f, Pace{})
	require.NoError(t, err)
	require.Equal(t, []record.StepKind{record.PushBranch, record.OpenPR}, p.Steps())

	out, err := Apply(t.Context(), env, p)
	require.NoError(t, err)
	assert.Equal(t, []record.StepKind{record.PushBranch, record.OpenPR}, out.Completed)
	assert.Equal(t, 1234, out.Number)
	assert.Equal(t, "https://github.com/macports/macports-ports/pull/1234", out.URL)

	// The branch really is on the fork: the push is a real push, and the
	// remote is asked rather than the tracking cache.
	has, err := repo.RemoteHas(t.Context(), "fork", "dockhand/jq-1.8")
	require.NoError(t, err)
	assert.True(t, has)

	// The row, keyed by the CHANGE and not by the commit at the tip.
	s := readState(t, st)
	require.Len(t, s.Publications, 1)
	for _, row := range s.Publications {
		assert.Equal(t, record.ChangeID("chg-1"), row.Change)
		assert.Equal(t, record.Human, row.By)
		assert.Equal(t, record.ContentID("tree-1"), row.Content)
		assert.Equal(t, "1.8", row.Target)
		assert.Equal(t, 1234, row.Number)
		assert.Equal(t, record.Open, row.Outcome)
		push, ok := stepOf(row, record.PushBranch)
		require.True(t, ok)
		assert.Equal(t, record.Finished, push.Phase)
		open, ok := stepOf(row, record.OpenPR)
		require.True(t, ok)
		assert.Equal(t, record.Finished, open.Phase)
		assert.Equal(t, 1, open.Attempt)
	}

	// The forge saw the revalidation read and then exactly one write.
	require.Len(t, log, 2)
	assert.True(t, strings.HasPrefix(log[0], "api "), "the first call re-asks about this branch's own PR")
	assert.Contains(t, log[1], "pr create --repo macports/macports-ports --head me:dockhand/jq-1.8")
}

// A FORGE CALL THAT ERRORED LEAVES THE STEP Uncertain AND NEVER A PHASE
// CLAIMING TO KNOW (rule 7): `gh pr create` prints the URL and then
// fails on the response, a push completes and the connection drops, and
// rule 6 forbids recovering the difference by reading the words that
// came back. The push before it stays Finished, which is the whole point
// of recording each step on its own.
func TestApplyLeavesAFailedForgeWriteUncertain(t *testing.T) {
	repo, st, sha := published(t)
	var log []string
	forge := scriptedForge(&log, func(args []string) (string, error) {
		if args[0] == "api" {
			return "[]", nil
		}
		return "", assertErr
	})
	env := Env{Repo: repo, State: st, Forge: forge}

	f := facts(func(f *Facts) {
		f.Tip, f.Change.Tip, f.Own = sha, sha, []string{sha}
		f.Attempts[0].Sha = sha
	})
	p, _, err := Authorize(f, Pace{})
	require.NoError(t, err)
	out, err := Apply(t.Context(), env, p)
	require.ErrorIs(t, err, assertErr)
	assert.Equal(t, []record.StepKind{record.PushBranch}, out.Completed)

	s := readState(t, st)
	require.Len(t, s.Publications, 1)
	for _, row := range s.Publications {
		push, _ := stepOf(row, record.PushBranch)
		assert.Equal(t, record.Finished, push.Phase, "the push is known to have happened")
		open, ok := stepOf(row, record.OpenPR)
		require.True(t, ok)
		assert.Equal(t, record.Uncertain, open.Phase)
		assert.Equal(t, assertErr.Error(), open.Detail)
	}

	// AND AN UNCERTAIN OPENING COUNTS AGAINST A MACHINE'S ALLOWANCE. A
	// crash between the push and the answer may have opened a pull
	// request, and a machine must not spend what it cannot account for.
	machineRow := record.Publication{ID: "pub-m", Change: "chg-9", By: record.Machine,
		Steps: []record.Step{{Kind: record.OpenPR, Phase: record.Uncertain, At: clock}}}
	s.Publications["pub-m"] = machineRow
	assert.Equal(t, 1, Spent(s, clock).Within(6*time.Hour, clock))
}

// A PERMIT WHOSE BRANCH MOVED IS STALE, AND THE PUSH DOES NOT HAPPEN.
// An `accept` in another terminal, a person's own `git commit`, a
// `bump --replace` — any of them between Gather and here and the branch
// carries bytes nobody authorized.
func TestApplyRefusesWhenTheBranchMovedUnderThePermit(t *testing.T) {
	repo, st, sha := published(t)
	var log []string
	env := Env{Repo: repo, State: st, Forge: scriptedForge(&log, func([]string) (string, error) {
		return "[]", nil
	})}

	f := facts(func(f *Facts) {
		f.Tip, f.Change.Tip, f.Own = sha, sha, []string{sha}
		f.Attempts[0].Sha = sha
	})
	p, _, err := Authorize(f, Pace{})
	require.NoError(t, err)

	moved := gittest.Commit(t, repo, "dockhand/jq-1.9", sha,
		"sysutils/jq/Portfile", "version 1.9\n", "jq: update to 1.9")
	gittest.MoveBranch(t, repo, "dockhand/jq-1.8", moved)

	_, err = Apply(t.Context(), env, p)
	require.ErrorIs(t, err, ErrStale)

	// Nothing was pushed and nothing was recorded: the revalidation is
	// before the first irreversible act, not after it.
	has, err := repo.RemoteHas(t.Context(), "fork", "dockhand/jq-1.8")
	require.NoError(t, err)
	assert.False(t, has)
	// Nothing was RECORDED either, asserted over the rows rather than
	// over the absence of a state ref: the fixture now plants a change,
	// which is the shape production always has.
	after, rerr := st.Read(t.Context())
	require.NoError(t, rerr)
	assert.Empty(t, after.Publications, "no row was opened for a permit that never acted")
}

// A PULL REQUEST THAT APPEARED IN THE WINDOW IS STALE TOO. The permit's
// whole shape turns on the branch's own pull request — whether there are
// steps at all, whether the second one opens or refreshes, whether the
// merged dead end applies — so a permit granted over "there is none" may
// not be spent against one that now exists.
func TestApplyRefusesWhenAPullRequestAppearedInTheWindow(t *testing.T) {
	repo, st, sha := published(t)
	var log []string
	env := Env{Repo: repo, State: st, Forge: scriptedForge(&log, func(args []string) (string, error) {
		return `[{"number":7,"state":"open","html_url":"u","head":{"ref":"dockhand/jq-1.8","sha":"` + sha + `"}}]`, nil
	})}

	f := facts(func(f *Facts) {
		f.Tip, f.Change.Tip, f.Own = sha, sha, []string{sha}
		f.Attempts[0].Sha = sha
	})
	p, _, err := Authorize(f, Pace{})
	require.NoError(t, err)
	_, err = Apply(t.Context(), env, p)
	require.ErrorIs(t, err, ErrStale)
}

// --no-pr STOPS AT THE PUSH and asks the forge nothing at all: there is
// no pull request to revalidate, so a permit that names no forge is not
// one this road re-asks about.
func TestApplyUnderNoPRPushesAndAsksTheForgeNothing(t *testing.T) {
	repo, st, sha := published(t)
	var log []string
	env := Env{Repo: repo, State: st, Forge: scriptedForge(&log, func([]string) (string, error) {
		t.Fatal("--no-pr asked the forge something")
		return "", nil
	})}

	f := facts(func(f *Facts) {
		f.Tip, f.Change.Tip, f.Own = sha, sha, []string{sha}
		f.Attempts[0].Sha = sha
		f.Asks.NoPR = true
		f.Forge.Upstream, f.Forge.ForkOwner = "", ""
	})
	p, _, err := Authorize(f, Pace{})
	require.NoError(t, err)
	out, err := Apply(t.Context(), env, p)
	require.NoError(t, err)
	assert.Equal(t, []record.StepKind{record.PushBranch}, out.Completed)
	assert.Zero(t, out.Number, "a push with no pull request is the publication whose number stays zero")
	assert.Empty(t, log)
}

// A MACHINE'S OPENING COUNTS AGAINST THE MACHINE'S ALLOWANCE, WHOEVER
// MADE THE ROW.
//
// `promote --no-pr` leaves a person's row: By Human, Outcome Open, a
// push and no pull request. The change then moves past that content, the
// dispatcher's publish slot takes it up, and Apply continues THAT row —
// one open row per change — and opens a real pull request on it. With By
// stamped once at creation, publish.Spent skipped the row entirely, so
// the machine could open N such pull requests beyond --publish-max with
// the pace still reporting the allowance unspent.
func TestAMachineOpeningOnAPersonsRowCountsAgainstThePace(t *testing.T) {
	repo, st, sha := published(t)
	require.NoError(t, st.Amend(t.Context(), func(tx *statestore.Txn) error {
		tx.PutPublication(record.Publication{
			ID: "pub-01", Change: "chg-1", By: record.Human, Outcome: record.Open, Content: "tree-0",
			Steps: []record.Step{{Kind: record.PushBranch, Phase: record.Finished, At: clock.Add(-time.Hour), Attempt: 1}},
		})
		return nil
	}))
	var log []string
	forge := scriptedForge(&log, func(args []string) (string, error) {
		switch {
		case args[0] == "api":
			return "[]", nil // the person asked for no pull request, so there is none
		case args[0] == "pr" && args[1] == "create":
			return "https://github.com/macports/macports-ports/pull/77\n", nil
		}
		return "", nil
	})
	env := Env{Repo: repo, State: st, Forge: forge, Version: "1.2.3"}

	before := Spent(readState(t, st), clock)
	require.Zero(t, before.Within(MaxWindow, clock), "the person's row spent nothing of the machine's allowance")

	f := facts(machine, func(f *Facts) {
		f.Tip, f.Change.Tip, f.Own = sha, sha, []string{sha}
		f.Attempts[0].Sha = sha
	})
	p, _, err := Authorize(f, DefaultPace)
	require.NoError(t, err)
	require.Equal(t, []record.StepKind{record.PushBranch, record.OpenPR}, p.Steps())
	out, err := Apply(t.Context(), env, p)
	require.NoError(t, err)
	require.Equal(t, 77, out.Number)

	s := readState(t, st)
	require.Len(t, s.Publications, 1, "one open row per change: the machine continued the person's")
	row := s.Publications["pub-01"]
	assert.Equal(t, record.Machine, row.By, "By is who opened the pull request")
	assert.Equal(t, 1, Spent(s, clock).Within(MaxWindow, clock),
		"the opening the machine performed is the opening the pace counts")
}

// AND IT MOVES ONE WAY ONLY. A person refreshing a pull request a
// dispatcher opened must not take that spend off the machine's books,
// which is the property the never-restamped rule was protecting.
func TestAPersonRefreshingAMachinesRowTakesNothingOffItsBooks(t *testing.T) {
	repo, st, sha := published(t)
	require.NoError(t, st.Amend(t.Context(), func(tx *statestore.Txn) error {
		tx.PutPublication(record.Publication{
			ID: "pub-01", Change: "chg-1", By: record.Machine, Outcome: record.Open, Number: 42,
			URL: "https://example.invalid/pull/1", Content: "tree-0",
			Steps: []record.Step{
				{Kind: record.PushBranch, Phase: record.Finished, At: clock.Add(-time.Hour), Attempt: 1},
				{Kind: record.OpenPR, Phase: record.Finished, At: clock.Add(-time.Hour), Attempt: 1},
			},
		})
		return nil
	}))
	var log []string
	forge := scriptedForge(&log, func(args []string) (string, error) {
		if args[0] == "api" {
			return `[{"number":42,"title":"jq: update to 1.8","state":"open","html_url":"https://example.invalid/pull/1","head":{"ref":"dockhand/jq-1.8","sha":"` + sha + `"}}]`, nil
		}
		return "", nil
	})
	env := Env{Repo: repo, State: st, Forge: forge, Version: "1.2.3"}

	f := facts(func(f *Facts) {
		f.Tip, f.Change.Tip, f.Own = sha, sha, []string{sha}
		f.Attempts[0].Sha = sha
		f.Forge.Own, f.Forge.OwnFound = openPR(42, "older"), true
		f.Asks.Force = true // a person's own re-push of a branch a pull request already stands on
	})
	p, _, err := Authorize(f, Pace{})
	require.NoError(t, err)
	require.Contains(t, p.Steps(), record.RefreshPR)
	_, err = Apply(t.Context(), env, p)
	require.NoError(t, err)

	s := readState(t, st)
	assert.Equal(t, record.Machine, s.Publications["pub-01"].By, "the machine's opening stays the machine's")
	assert.Equal(t, 1, Spent(s, clock).Within(MaxWindow, clock))
}

// THE PUBLICATION RACE. A commit arriving between the permit's tip check
// and the push used to be what git sent: the push named a BRANCH, so
// whatever the branch held when git ran left the machine, under a permit
// whose evidence, body and row all described the earlier commit. A probe
// moved the branch from inside the forge callback and watched the later
// commit land on the fork.
func TestApplyPushesTheAuthorizedObjectAndNotWhateverTheBranchHolds(t *testing.T) {
	repo, st, sha := published(t)
	var later string
	var log []string
	forge := scriptedForge(&log, func(args []string) (string, error) {
		if args[0] == "api" && later == "" {
			// The branch moves while the forge is being asked, which is the
			// window the tip check cannot cover.
			later = gittest.Commit(t, repo, "dockhand/jq-1.9", sha,
				"sysutils/jq/Portfile", "version 1.9\n", "later, unauthorised commit")
			gittest.MoveBranch(t, repo, "dockhand/jq-1.8", later)
		}
		return "[]", nil
	})
	env := Env{Repo: repo, State: st, Forge: forge}

	f := facts(func(f *Facts) {
		f.Tip, f.Change.Tip, f.Own = sha, sha, []string{sha}
		f.Attempts[0].Sha = sha
		f.Asks = Asks{NoPR: true}
	})
	p, _, err := Authorize(f, Pace{})
	require.NoError(t, err)

	_, err = Apply(t.Context(), env, p)
	require.NoError(t, err)
	require.NotEmpty(t, later, "the fixture did not move the branch")

	tip, err := repo.RemoteTip(t.Context(), "fork", "dockhand/jq-1.8")
	require.NoError(t, err)
	assert.Equal(t, sha, tip, "the authorized object")
	assert.NotEqual(t, later, tip, "and never the one that arrived in the window")
}

// The push records its EXACT TARGET, which is what lets a later deletion
// address the copy this publication made rather than infer one from
// local refs.
func TestApplyRecordsTheForkItPushedTo(t *testing.T) {
	repo, st, sha := published(t)
	var log []string
	env := Env{Repo: repo, State: st, Forge: scriptedForge(&log, func([]string) (string, error) {
		return "[]", nil
	})}
	f := facts(func(f *Facts) {
		f.Tip, f.Change.Tip, f.Own = sha, sha, []string{sha}
		f.Attempts[0].Sha = sha
		f.Asks = Asks{NoPR: true}
	})
	p, _, err := Authorize(f, Pace{})
	require.NoError(t, err)
	_, err = Apply(t.Context(), env, p)
	require.NoError(t, err)

	after, err := st.Read(t.Context())
	require.NoError(t, err)
	require.Len(t, after.Publications, 1)
	for _, row := range after.Publications {
		assert.True(t, row.Fork.Pushed())
		assert.Equal(t, "fork", row.Fork.Remote)
		assert.Equal(t, "dockhand/jq-1.8", row.Fork.Branch)
		assert.Equal(t, sha, row.Fork.OID, "the object, so a deletion can assert it")
	}
}

// A HOLD APPLIED IN THE WINDOW STOPS THE PUBLICATION. Revalidation
// asked git about the tip and the forge about the pull request, and
// never asked the STORE about the change — so a person's `hold`, landing
// between Gather and the push, left the permit valid. Serializing passes
// does not help: `hold` takes no pass lock.
func TestApplyRefusesWhenAHoldLandedInTheWindow(t *testing.T) {
	repo, st, sha := published(t)
	var log []string
	forge := scriptedForge(&log, func(args []string) (string, error) {
		if args[0] == "api" {
			require.NoError(t, st.Amend(t.Context(), func(tx *statestore.Txn) error {
				return change.HoldIn(tx, "chg-1", "stop publication",
					record.OwnerID{Root: "/w/ports"}, clock)
			}))
		}
		return "[]", nil
	})
	env := Env{Repo: repo, State: st, Forge: forge}

	f := facts(func(f *Facts) {
		f.Tip, f.Change.Tip, f.Own = sha, sha, []string{sha}
		f.Attempts[0].Sha = sha
	})
	p, _, err := Authorize(f, Pace{})
	require.NoError(t, err)

	_, err = Apply(t.Context(), env, p)
	require.ErrorIs(t, err, ErrStale)

	tip, terr := repo.RemoteTip(t.Context(), "fork", "dockhand/jq-1.8")
	require.NoError(t, terr)
	assert.Empty(t, tip, "nothing was pushed")
}

// The same boundary over the other two ways a change stops being
// publishable while a permit is in flight.
func TestApplyRefusesAChangeClosedOrSupersededInTheWindow(t *testing.T) {
	for name, mutate := range map[string]func(*record.Change){
		"closed":     func(c *record.Change) { c.State = record.ChangePublished },
		"superseded": func(c *record.Change) { c.SupersededBy = "chg-2" },
	} {
		t.Run(name, func(t *testing.T) {
			repo, st, sha := published(t)
			var log []string
			forge := scriptedForge(&log, func(args []string) (string, error) {
				if args[0] == "api" {
					require.NoError(t, st.Amend(t.Context(), func(tx *statestore.Txn) error {
						c := tx.State().Changes["chg-1"]
						mutate(&c)
						tx.PutChange(c)
						return nil
					}))
				}
				return "[]", nil
			})
			f := facts(func(f *Facts) {
				f.Tip, f.Change.Tip, f.Own = sha, sha, []string{sha}
				f.Attempts[0].Sha = sha
			})
			p, _, err := Authorize(f, Pace{})
			require.NoError(t, err)

			_, err = Apply(t.Context(), Env{Repo: repo, State: st, Forge: forge}, p)
			require.ErrorIs(t, err, ErrStale)
		})
	}
}
