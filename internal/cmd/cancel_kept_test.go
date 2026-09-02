package cmd

// "Done debugging, the slot back please" previously had no verb short
// of discarding the branch (field case macports-ports-46): cancel now
// releases a failed run's kept environment while the verdict stands.

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/runstate"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/verifytest"
)

func TestCancelReleasesAKeptFailureEnvironment(t *testing.T) {
	repo, sha := lifecycleRepo(t)
	ctx := context.Background()
	n, err := ledger.Open(repo).LoadOrStart(ctx, sha, "jq")
	require.NoError(t, err)
	n.Runs["Testos"] = record.Run{State: "failed", Handle: "fake-1",
		Job: verify.Job{Provider: "fake", ID: "fake-1"}, Detail: "Failed to build jq: boom"}
	require.NoError(t, ledger.Open(repo).Write(ctx, n))

	fake := &verifytest.Fake{}
	var out, errb bytes.Buffer
	rs := &runstate.Context{TreeRoot: repo.Root, Tools: testFinder(), Out: &out, Err: &errb,
		Verifier: func(context.Context) (verify.Verifier, error) { return fake, nil }}

	require.NoError(t, cancelAction{target: "jq"}.Execute(ctx, rs))
	assert.Equal(t, []string{"fake-1"}, fake.Released)
	assert.Contains(t, out.String(), "released kept environment of dockhand/jq-1.8 on Testos")
	assert.Contains(t, out.String(), "the failed verdict stands")

	after, err := ledger.Open(repo).Read(ctx, sha)
	require.NoError(t, err)
	r := after.Runs["Testos"]
	assert.Equal(t, record.Failed, r.State, "cancel frees the environment, never the evidence")
	assert.Empty(t, r.Handle)
	assert.Contains(t, r.Detail, "Failed to build jq: boom — kept environment released")
}

func TestCancelWithNothingToFreeSaysSo(t *testing.T) {
	repo, sha := lifecycleRepo(t)
	ctx := context.Background()
	n, err := ledger.Open(repo).LoadOrStart(ctx, sha, "jq")
	require.NoError(t, err)
	n.Runs["Testos"] = record.Run{State: "passed"}
	require.NoError(t, ledger.Open(repo).Write(ctx, n))

	fake := &verifytest.Fake{}
	var out, errb bytes.Buffer
	rs := &runstate.Context{TreeRoot: repo.Root, Tools: testFinder(), Out: &out, Err: &errb,
		Verifier: func(context.Context) (verify.Verifier, error) { return fake, nil }}

	require.NoError(t, cancelAction{target: "jq"}.Execute(ctx, rs))
	assert.Contains(t, errb.String(), "no running verification or kept environment")
	assert.Empty(t, fake.Released)
}

// SubjectOf's authority order, field-driven (pcre2 built as pcre):
// the user's own word wins, then the note's recorded port; the
// evaluation-derived tier is exercised in the engine's own
// changed-context tests.
func TestSubjectOfHonorsTargetThenNote(t *testing.T) {
	repo, sha := lifecycleRepo(t)
	ctx := context.Background()
	eng := (&runstate.Context{TreeRoot: repo.Root, Tools: testFinder()}).Deps()

	// The user typed a port name and it matched the branch: that name
	// is the port, portdir base be damned.
	name, err := eng.SubjectOf(ctx, repo, "jq2", "dockhand/jq2-1.8", sha, "sysutils/jq")
	require.NoError(t, err)
	assert.Equal(t, "jq2", name)

	// Target is the branch itself: the note's recorded port answers.
	n, err := ledger.Open(repo).LoadOrStart(ctx, sha, "jq2")
	require.NoError(t, err)
	n.Runs["Testos"] = record.Run{State: "deferred", Detail: "slots busy"}
	require.NoError(t, ledger.Open(repo).Write(ctx, n))
	name, err = eng.SubjectOf(ctx, repo, "dockhand/jq-1.8", "dockhand/jq-1.8", sha, "sysutils/jq")
	require.NoError(t, err)
	assert.Equal(t, "jq2", name, "the note was written from the plan's subport at bump time")
}
