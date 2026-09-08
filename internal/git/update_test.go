package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// twoCommits is a repository and two distinct commit shas to move refs
// between: the fixture's own HEAD, and one commit written over it that
// no ref names — which is also CommitTree's own promise, that a commit
// object exists before and independently of the ref that will carry it.
func twoCommits(t *testing.T) (*Repo, string, string) {
	t.Helper()
	r := newRepo(t)
	ctx := context.Background()
	a, err := r.RevParse(ctx, "HEAD")
	require.NoError(t, err)
	tree, err := r.GraftTree(ctx, a, []File{{Path: "sysutils/jq/Portfile", Content: []byte("version 1.8\n")}})
	require.NoError(t, err)
	b, err := r.CommitTree(ctx, tree, []string{a}, "jq: update to 1.8")
	require.NoError(t, err)
	require.NotEqual(t, a, b)
	return r, a, b
}

// plant writes a ref straight through git, outside UpdateRefs, so a
// test can set up the state a batch is then run against.
func plant(t *testing.T, r *Repo, args ...string) {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", r.Root}, args...)...).CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
}

// refValue is what a ref holds according to git itself, with absence
// distinguishable from a value — the assertion side of every case
// below, kept independent of the package's own refNow so a bug in the
// classifier cannot make its own test pass.
func refValue(t *testing.T, r *Repo, ref string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", r.Root, "rev-parse", "--verify", "--quiet", ref).Output()
	if err != nil {
		return ""
	}
	return string(out[:len(out)-1])
}

// THE WIRE. Build step 3's done criterion names two of these cases by
// hand — a create line against a standing ref is refused, and a symref
// in the namespace does not carry a move onto its target — and the rest
// are the rest of the field convention, because the convention is only
// safe if every form of it was measured rather than assumed.
//
// The one that would be silent is the first. In -z mode an empty
// <oldvalue> FIELD means "unverified" and not "must not exist": measured
// on git 2.55.0, `update <ref> NUL <new> NUL <empty>` moved a standing
// ref and exited 0. A RefUpdate with Old "" means "the ref must not
// exist", so if UpdateRefs emitted that as an update with an empty old
// field, every create in the design would silently clobber whatever
// stood at the name. It emits the `create` command instead, and that is
// what this table holds it to.
func TestTheWireMeansWhatRefUpdatesFieldsMean(t *testing.T) {
	const ns = "refs/heads/dockhand/"
	for _, tc := range []struct {
		name string
		// setup plants the state the batch runs against, and returns
		// nothing: what it planted is asserted by check.
		setup func(t *testing.T, r *Repo, a, b string)
		batch func(a, b string) []RefUpdate
		moved *RefMoved // nil: the batch is expected to succeed
		check func(t *testing.T, r *Repo, a, b string)
	}{{
		name: "a create line against a standing ref is refused, and the ref does not move",
		setup: func(t *testing.T, r *Repo, a, b string) {
			plant(t, r, "update-ref", ns+"jq-1.8", a)
		},
		batch: func(a, b string) []RefUpdate {
			return []RefUpdate{{Ref: ns + "jq-1.8", New: b}}
		},
		moved: &RefMoved{Ref: ns + "jq-1.8", Expected: "", Found: "@a"},
		check: func(t *testing.T, r *Repo, a, b string) {
			assert.Equal(t, a, refValue(t, r, ns+"jq-1.8"),
				"the name in flight kept its own tip; an empty old field would have taken it")
		},
	}, {
		name: "a create line against an absent ref lands it",
		batch: func(a, b string) []RefUpdate {
			return []RefUpdate{{Ref: ns + "jq-1.8", New: b}}
		},
		check: func(t *testing.T, r *Repo, a, b string) {
			assert.Equal(t, b, refValue(t, r, ns+"jq-1.8"))
		},
	}, {
		name: "an update line moves a ref that holds its expected old",
		setup: func(t *testing.T, r *Repo, a, b string) {
			plant(t, r, "update-ref", ns+"jq-1.8", a)
		},
		batch: func(a, b string) []RefUpdate {
			return []RefUpdate{{Ref: ns + "jq-1.8", New: b, Old: a}}
		},
		check: func(t *testing.T, r *Repo, a, b string) {
			assert.Equal(t, b, refValue(t, r, ns+"jq-1.8"))
		},
	}, {
		name: "an update line whose expected old is wrong is refused",
		setup: func(t *testing.T, r *Repo, a, b string) {
			plant(t, r, "update-ref", ns+"jq-1.8", b)
		},
		batch: func(a, b string) []RefUpdate {
			return []RefUpdate{{Ref: ns + "jq-1.8", New: a, Old: a}}
		},
		moved: &RefMoved{Ref: ns + "jq-1.8", Expected: "@a", Found: "@b"},
		check: func(t *testing.T, r *Repo, a, b string) {
			assert.Equal(t, b, refValue(t, r, ns+"jq-1.8"))
		},
	}, {
		name: "New equal to Old is an assertion: the expected value is still checked",
		setup: func(t *testing.T, r *Repo, a, b string) {
			plant(t, r, "update-ref", ns+"jq-1.8", b)
		},
		batch: func(a, b string) []RefUpdate {
			return []RefUpdate{{Ref: ns + "jq-1.8", New: a, Old: a}}
		},
		moved: &RefMoved{Ref: ns + "jq-1.8", Expected: "@a", Found: "@b"},
	}, {
		name: "New equal to Old over the right value moves nothing and succeeds",
		setup: func(t *testing.T, r *Repo, a, b string) {
			plant(t, r, "update-ref", ns+"jq-1.8", a)
		},
		batch: func(a, b string) []RefUpdate {
			return []RefUpdate{{Ref: ns + "jq-1.8", New: a, Old: a}}
		},
		check: func(t *testing.T, r *Repo, a, b string) {
			assert.Equal(t, a, refValue(t, r, ns+"jq-1.8"))
		},
	}, {
		name: "a delete line carries the value the ref must hold",
		setup: func(t *testing.T, r *Repo, a, b string) {
			plant(t, r, "update-ref", ns+"jq-1.8", a)
		},
		batch: func(a, b string) []RefUpdate {
			return []RefUpdate{{Ref: ns + "jq-1.8", Old: a}}
		},
		check: func(t *testing.T, r *Repo, a, b string) {
			assert.Empty(t, refValue(t, r, ns+"jq-1.8"))
		},
	}, {
		name: "a delete line whose expected old is wrong keeps the ref",
		setup: func(t *testing.T, r *Repo, a, b string) {
			plant(t, r, "update-ref", ns+"jq-1.8", b)
		},
		batch: func(a, b string) []RefUpdate {
			return []RefUpdate{{Ref: ns + "jq-1.8", Old: a}}
		},
		moved: &RefMoved{Ref: ns + "jq-1.8", Expected: "@a", Found: "@b"},
		check: func(t *testing.T, r *Repo, a, b string) {
			assert.Equal(t, b, refValue(t, r, ns+"jq-1.8"),
				"an empty old field would have deleted it unchecked")
		},
	}, {
		// The one a hand can plant and nothing else would catch. Without
		// `option no-deref` an update line for a symref moves its TARGET
		// — measured — so a symref planted at refs/heads/dockhand/<name>
		// makes dockhand's own namespace a lever on somebody else's
		// branch. Under no-deref the symref itself is overwritten as a
		// plain ref, which is dockhand's own name to overwrite.
		name: "a symref in the namespace does not carry a move onto its target",
		setup: func(t *testing.T, r *Repo, a, b string) {
			plant(t, r, "update-ref", "refs/heads/someone-elses-work", a)
			plant(t, r, "symbolic-ref", ns+"jq-1.8", "refs/heads/someone-elses-work")
		},
		batch: func(a, b string) []RefUpdate {
			return []RefUpdate{{Ref: ns + "jq-1.8", New: b, Old: a}}
		},
		check: func(t *testing.T, r *Repo, a, b string) {
			assert.Equal(t, a, refValue(t, r, "refs/heads/someone-elses-work"),
				"the target stood still; without no-deref this line moves it")
			assert.Equal(t, b, refValue(t, r, ns+"jq-1.8"), "the name dockhand owns took the write")
			out, err := exec.Command("git", "-C", r.Root, "symbolic-ref", "-q", ns+"jq-1.8").CombinedOutput()
			assert.Error(t, err, "it is a plain ref now: %s", out)
		},
	}, {
		name: "a symref in the namespace does not carry a delete onto its target",
		setup: func(t *testing.T, r *Repo, a, b string) {
			plant(t, r, "update-ref", "refs/heads/someone-elses-work", a)
			plant(t, r, "symbolic-ref", ns+"jq-1.8", "refs/heads/someone-elses-work")
		},
		batch: func(a, b string) []RefUpdate {
			return []RefUpdate{{Ref: ns + "jq-1.8", Old: a}}
		},
		check: func(t *testing.T, r *Repo, a, b string) {
			assert.Equal(t, a, refValue(t, r, "refs/heads/someone-elses-work"),
				"the target survived; without no-deref this line deletes it")
			assert.Empty(t, refValue(t, r, ns+"jq-1.8"))
		},
	}, {
		// An option applies to the NEXT command only (measured: with one
		// leading `option no-deref` and two symref lines, the first was
		// spared and the second moved its target), so every line in the
		// batch needs its own.
		name: "every line gets its own no-deref, not just the first",
		setup: func(t *testing.T, r *Repo, a, b string) {
			plant(t, r, "update-ref", "refs/heads/first-target", a)
			plant(t, r, "symbolic-ref", ns+"one", "refs/heads/first-target")
			plant(t, r, "update-ref", "refs/heads/second-target", a)
			plant(t, r, "symbolic-ref", ns+"two", "refs/heads/second-target")
		},
		batch: func(a, b string) []RefUpdate {
			return []RefUpdate{
				{Ref: ns + "one", New: b, Old: a},
				{Ref: ns + "two", New: b, Old: a},
			}
		},
		check: func(t *testing.T, r *Repo, a, b string) {
			assert.Equal(t, a, refValue(t, r, "refs/heads/first-target"))
			assert.Equal(t, a, refValue(t, r, "refs/heads/second-target"),
				"the second line's target is the one a single leading option leaves exposed")
		},
	}} {
		t.Run(tc.name, func(t *testing.T) {
			r, a, b := twoCommits(t)
			if tc.setup != nil {
				tc.setup(t, r, a, b)
			}
			err := r.UpdateRefs(context.Background(), tc.batch(a, b))
			if tc.moved == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, ErrRefMoved)
				var moved *RefMoved
				require.ErrorAs(t, err, &moved)
				// The expectations are written with @a and @b for the
				// two shas, which are only known once the fixture is
				// built.
				sha := func(s string) string {
					switch s {
					case "@a":
						return a
					case "@b":
						return b
					}
					return s
				}
				assert.Equal(t, tc.moved.Ref, moved.Ref)
				assert.Equal(t, moved.Expected, sha(tc.moved.Expected))
				assert.Equal(t, moved.Found, sha(tc.moved.Found))
			}
			if tc.check != nil {
				tc.check(t, r, a, b)
			}
		})
	}
}

// All or nothing is git's, and it is what lets a record and the refs it
// implies land in one act: one line git will not take rolls back every
// other line in the batch, including the ones it had already locked.
func TestARefusedBatchLandsNoneOfItsOtherLines(t *testing.T) {
	r, a, b := twoCommits(t)
	ctx := context.Background()
	plant(t, r, "update-ref", "refs/dockhand/state", a)
	plant(t, r, "update-ref", "refs/heads/dockhand/jq-1.8", a)

	// A batch that would move the state ref, create a pin and delete a
	// branch — with the branch line expecting a value the branch does
	// not hold.
	err := r.UpdateRefs(ctx, []RefUpdate{
		{Ref: "refs/dockhand/state", New: b, Old: a},
		{Ref: "refs/dockhand/verify/01JAX", New: b},
		{Ref: "refs/heads/dockhand/jq-1.8", Old: b},
	})
	require.ErrorIs(t, err, ErrRefMoved)

	assert.Equal(t, a, refValue(t, r, "refs/dockhand/state"), "the state ref did not move")
	assert.Empty(t, refValue(t, r, "refs/dockhand/verify/01JAX"), "the pin was not created")
	assert.Equal(t, a, refValue(t, r, "refs/heads/dockhand/jq-1.8"), "the branch was not deleted")
}

// git names the ref it refused over only in stderr prose, and rule 6
// forbids recovering a fact by reading words. So the refusal is
// classified by RE-READING the batch, in batch order, and the state ref
// is always the first line — a lost race on the store's own
// compare-and-set is named before any ref behind it, because that is the
// one Amend retries on and every other one it hands to the caller.
func TestARefusedBatchIsClassifiedByReReadingItInOrder(t *testing.T) {
	ctx := context.Background()

	t.Run("the state ref lost the race, and it is named first", func(t *testing.T) {
		r, a, b := twoCommits(t)
		plant(t, r, "update-ref", "refs/dockhand/state", b)
		plant(t, r, "update-ref", "refs/heads/dockhand/jq-1.8", b)

		err := r.UpdateRefs(ctx, []RefUpdate{
			{Ref: "refs/dockhand/state", New: a, Old: a},
			{Ref: "refs/heads/dockhand/jq-1.8", New: a, Old: a},
		})
		var moved *RefMoved
		require.ErrorAs(t, err, &moved)
		assert.Equal(t, "refs/dockhand/state", moved.Ref, "both lines lost; the store's own is the answer")
		assert.Equal(t, moved.Expected, a)
		assert.Equal(t, moved.Found, b)
	})

	t.Run("the state ref stood and a branch moved, so the branch is named", func(t *testing.T) {
		r, a, b := twoCommits(t)
		plant(t, r, "update-ref", "refs/dockhand/state", a)
		plant(t, r, "update-ref", "refs/heads/dockhand/jq-1.8", b)

		err := r.UpdateRefs(ctx, []RefUpdate{
			{Ref: "refs/dockhand/state", New: b, Old: a},
			{Ref: "refs/heads/dockhand/jq-1.8", New: a, Old: a},
		})
		var moved *RefMoved
		require.ErrorAs(t, err, &moved)
		assert.Equal(t, "refs/heads/dockhand/jq-1.8", moved.Ref)
		assert.Equal(t, moved.Expected, a)
		assert.Equal(t, moved.Found, b)
		assert.Contains(t, moved.Error(), "refs/heads/dockhand/jq-1.8", "the message names the ref, not git's prose")
	})

	t.Run("a create line refused reports absence as the expectation", func(t *testing.T) {
		r, a, b := twoCommits(t)
		plant(t, r, "update-ref", "refs/dockhand/verify/01JAX", b)

		err := r.UpdateRefs(ctx, []RefUpdate{{Ref: "refs/dockhand/verify/01JAX", New: a}})
		var moved *RefMoved
		require.ErrorAs(t, err, &moved)
		assert.Empty(t, moved.Expected, `a create expected absence, and "" is how absence is spelled`)
		assert.Equal(t, moved.Found, b)
	})

	t.Run("a ref that vanished is Found empty and not a re-read that failed", func(t *testing.T) {
		r, a, _ := twoCommits(t)
		err := r.UpdateRefs(ctx, []RefUpdate{{Ref: "refs/heads/dockhand/gone", New: a, Old: a}})
		var moved *RefMoved
		require.ErrorAs(t, err, &moved)
		assert.Equal(t, moved.Expected, a)
		assert.Empty(t, moved.Found, "the ref is not there; that is an answer, not a failure to ask")
	})
}

// When every ref in the batch stands exactly where its line expected,
// the refusal was git's own — a lock a person's porcelain holds, a full
// disk, a packed-refs someone else is rewriting — and it is neither
// ErrRefMoved nor anything else this package invented. It leaves as it
// came, untyped, and unretried: a retry that cannot say what it retries
// is check-then-act.
func TestARefusalNoRefExplainsLeavesUntyped(t *testing.T) {
	r, a, b := twoCommits(t)
	ctx := context.Background()
	plant(t, r, "update-ref", "refs/heads/dockhand/jq-1.8", a)

	// A stale .lock beside the ref is exactly the shape a person's
	// interrupted porcelain leaves, and git will not take the ref while
	// it is there.
	gitDir, err := r.git(ctx, "rev-parse", "--absolute-git-dir")
	require.NoError(t, err)
	lock := filepath.Join(gitDir, "refs", "heads", "dockhand", "jq-1.8.lock")
	require.NoError(t, os.WriteFile(lock, nil, 0o644))
	t.Cleanup(func() { _ = os.Remove(lock) })

	err = r.UpdateRefs(ctx, []RefUpdate{{Ref: "refs/heads/dockhand/jq-1.8", New: b, Old: a}})
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrRefMoved, "the ref is where the line said it was; nothing moved")
	require.NotErrorIs(t, err, ErrBadUpdate)
	assert.Equal(t, a, refValue(t, r, "refs/heads/dockhand/jq-1.8"))
}

// ErrBadUpdate is the batch this package will not put on the wire, and
// each of these is refused BEFORE git is run — which for the third case
// is the whole point. Measured on git 2.55.0: `update <ref> NUL NUL`
// warns "missing <new-oid>, treating as zero", exits 0, and deletes the
// ref without checking anything. A caller that lost the sha it meant to
// assert would destroy the ref it was asserting.
func TestUpdateRefsRefusesABatchItCannotMean(t *testing.T) {
	for _, tc := range []struct {
		name    string
		updates []RefUpdate
	}{
		{"an empty batch", nil},
		{"a line with no ref", []RefUpdate{{New: "abc"}}},
		{"a line with neither a new value nor an expected one",
			[]RefUpdate{{Ref: "refs/heads/dockhand/jq-1.8"}}},
		{"a ref named twice", []RefUpdate{
			{Ref: "refs/heads/dockhand/jq-1.8", New: "abc"},
			{Ref: "refs/heads/dockhand/jq-1.8", New: "def", Old: "abc"},
		}},
		{"a ref name carrying the field separator itself",
			[]RefUpdate{{Ref: "refs/heads/dockhand/jq\x001.8", New: "abc"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, a, _ := twoCommits(t)
			plant(t, r, "update-ref", "refs/heads/dockhand/jq-1.8", a)

			err := r.UpdateRefs(context.Background(), tc.updates)
			require.ErrorIs(t, err, ErrBadUpdate)
			assert.Equal(t, a, refValue(t, r, "refs/heads/dockhand/jq-1.8"),
				"refused here, so git never saw it and nothing moved")
		})
	}
}

// The reflog covers a create and an update, which is what makes
// core.logAllRefUpdates=always worth setting for a namespace git gives
// no reflog to otherwise — and it does NOT cover a delete: git removes a
// ref's reflog with the ref, so the recovery path for a demolished
// branch is the state ref's own history and never the reflog of the
// branch that is gone.
func TestTheReflogCoversCreatesAndUpdatesAndDiesWithADelete(t *testing.T) {
	r, a, b := twoCommits(t)
	ctx := context.Background()
	plant(t, r, "config", "core.logAllRefUpdates", "always")
	const ref = "refs/dockhand/state"

	require.NoError(t, r.UpdateRefs(ctx, []RefUpdate{{Ref: ref, New: a}}))
	require.NoError(t, r.UpdateRefs(ctx, []RefUpdate{{Ref: ref, New: b, Old: a}}))
	log, err := r.git(ctx, "reflog", "show", "--format=%H %gs", ref)
	require.NoError(t, err)
	assert.Contains(t, log, reflogMessage, "the -m is there for the person reading it")
	assert.Contains(t, log, a)
	assert.Contains(t, log, b)

	require.NoError(t, r.UpdateRefs(ctx, []RefUpdate{{Ref: ref, Old: b}}))
	_, err = r.git(ctx, "reflog", "show", ref)
	assert.Error(t, err, "the reflog went with the ref")
}

// CommitTree writes an object and nothing else. Under R23 that is the
// division: this package writes the commit, and the ONLY thing that may
// then name it is a statestore.Amend batch. An empty parents list writes
// a root commit, which the shipped unexported commit could not do — it
// hardcoded `-p parent` — and which the state ref's first write needs.
func TestCommitTreeWritesAnObjectAndNoRef(t *testing.T) {
	r, a, _ := twoCommits(t)
	ctx := context.Background()

	tree, err := r.RevParse(ctx, a+"^{tree}")
	require.NoError(t, err)

	root, err := r.CommitTree(ctx, tree, nil, "the first state")
	require.NoError(t, err)
	parents, err := r.git(ctx, "log", "-1", "--format=%P", root)
	require.NoError(t, err)
	assert.Empty(t, parents, "no parents list, no parent")

	child, err := r.CommitTree(ctx, tree, []string{root}, "the second state")
	require.NoError(t, err)
	parents, err = r.git(ctx, "log", "-1", "--format=%P", child)
	require.NoError(t, err)
	assert.Equal(t, root, parents)

	// Neither is named by any ref: they are gc fodder until a batch
	// takes them, which is exactly what the design wants.
	names, err := r.git(ctx, "for-each-ref", "--format=%(objectname)")
	require.NoError(t, err)
	assert.NotContains(t, names, root)
	assert.NotContains(t, names, child)

	// The message reaches the object as its bytes stand: -F takes it
	// whole and git appends nothing.
	body, err := r.git(ctx, "log", "-1", "--format=%B", root)
	require.NoError(t, err)
	assert.Equal(t, "the first state", body)
}

// update-ref does not refuse what `branch -D` and `branch -f` refuse: it
// deletes a branch a worktree has checked out and moves one under its
// worktree's index (measured, both). The guard is therefore an
// observation a road makes BEFORE its batch, and this is the read that
// observation is.
func TestCheckedOutAtNamesTheWorktreeHoldingABranch(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()

	// Nothing is checked out under the namespace to begin with, and "" is
	// the answer rather than an error.
	where, err := r.CheckedOutAt(ctx, "dockhand/jq-1.8")
	require.NoError(t, err)
	assert.Empty(t, where)

	wt := filepath.Join(t.TempDir(), "linked")
	plant(t, r, "worktree", "add", "--quiet", "-b", "dockhand/jq-1.8", wt)

	where, err = r.CheckedOutAt(ctx, "dockhand/jq-1.8")
	require.NoError(t, err)
	resolved, _ := filepath.EvalSymlinks(wt)
	assert.Equal(t, resolved, where)

	// The primary branch is checked out in the main worktree, which is a
	// worktree like any other as far as this read is concerned.
	primary, err := r.PrimaryBranch(ctx)
	require.NoError(t, err)
	where, err = r.CheckedOutAt(ctx, primary)
	require.NoError(t, err)
	assert.NotEmpty(t, where)

	// The name is matched whole. A prefix match would read this branch's
	// worktree as the shorter name's.
	where, err = r.CheckedOutAt(ctx, "dockhand/jq")
	require.NoError(t, err)
	assert.Empty(t, where, "dockhand/jq is not dockhand/jq-1.8")

	// And it is the batch's blindness this guards, not the batch's
	// refusal: the delete goes through regardless.
	tip := refValue(t, r, "refs/heads/dockhand/jq-1.8")
	require.NoError(t, r.UpdateRefs(ctx, []RefUpdate{{Ref: "refs/heads/dockhand/jq-1.8", Old: tip}}))
	assert.Empty(t, refValue(t, r, "refs/heads/dockhand/jq-1.8"),
		"update-ref deletes a checked-out branch where branch -D refuses; rule 1 puts the guard in app")
}

// RefsUnder enumerates a namespace nothing else does — doctor's pin row,
// the population a recreated state ref leaves behind. It is path-wise,
// the same match Branches relies on, and an empty namespace is an empty
// answer rather than a failure.
func TestRefsUnderListsANamespacePathWise(t *testing.T) {
	r, a, _ := twoCommits(t)
	ctx := context.Background()

	under, err := r.RefsUnder(ctx, "refs/dockhand/verify/")
	require.NoError(t, err)
	assert.Empty(t, under, "a namespace nothing has written to holds nothing")

	plant(t, r, "update-ref", "refs/dockhand/verify/01JAX", a)
	plant(t, r, "update-ref", "refs/dockhand/verify/01JBY", a)
	plant(t, r, "update-ref", "refs/dockhand/verify-scratch", a)
	plant(t, r, "update-ref", "refs/dockhand/state", a)

	under, err = r.RefsUnder(ctx, "refs/dockhand/verify/")
	require.NoError(t, err)
	assert.Equal(t, []string{"refs/dockhand/verify/01JAX", "refs/dockhand/verify/01JBY"}, under,
		"the slash keeps verify-scratch and the state ref out")
}

// A foreign effect's outcome is observed from the foreign side.
// refs/remotes/ is a cache of this machine's own last push or fetch, and
// a copy deleted on the remote leaves it standing — so RemoteHas asks
// the remote, and PushedTo, which reads the cache, is the verb that must
// not be used for this question.
func TestRemoteHasAsksTheRemoteAndNotTheTrackingRef(t *testing.T) {
	r, a, _ := twoCommits(t)
	ctx := context.Background()

	fork := t.TempDir()
	out, err := exec.Command("git", "init", "--bare", "--quiet", fork).CombinedOutput()
	require.NoError(t, err, "%s", out)
	plant(t, r, "remote", "add", "fork", fork)

	plant(t, r, "update-ref", "refs/heads/dockhand/jq-1.8", a)
	has, err := r.RemoteHas(ctx, "fork", "dockhand/jq-1.8")
	require.NoError(t, err)
	assert.False(t, has, "nothing has been pushed yet")

	require.NoError(t, r.Push(ctx, "fork", "dockhand/jq-1.8"))
	has, err = r.RemoteHas(ctx, "fork", "dockhand/jq-1.8")
	require.NoError(t, err)
	assert.True(t, has)

	// A copy deleted on the remote by another hand. The tracking ref
	// still names it — which is precisely the stale answer a sequencer
	// would retry a push-delete against forever.
	out, err = exec.Command("git", "-C", fork, "update-ref", "-d", "refs/heads/dockhand/jq-1.8").CombinedOutput()
	require.NoError(t, err, "%s", out)

	cached, err := r.PushedTo(ctx, "dockhand/jq-1.8")
	require.NoError(t, err)
	assert.Equal(t, "fork", cached, "the local cache still says the copy is there")

	has, err = r.RemoteHas(ctx, "fork", "dockhand/jq-1.8")
	require.NoError(t, err)
	assert.False(t, has, "the remote's own word is that it is gone")

	// The name is matched whole: the fully qualified ref is the pattern,
	// so a shorter name is not a copy of a longer one.
	require.NoError(t, r.UpdateRefs(ctx, []RefUpdate{{Ref: "refs/heads/dockhand/jq-1.8", New: a, Old: a}}))
	require.NoError(t, r.Push(ctx, "fork", "dockhand/jq-1.8"))
	has, err = r.RemoteHas(ctx, "fork", "dockhand/jq")
	require.NoError(t, err)
	assert.False(t, has, "dockhand/jq is not dockhand/jq-1.8")

	// A remote that cannot be asked is an error and never "absent"
	// (rule 7): the caller writes Uncertain on it and looks again.
	_, err = r.RemoteHas(ctx, "no-such-remote", "dockhand/jq-1.8")
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrRefMoved)
}
