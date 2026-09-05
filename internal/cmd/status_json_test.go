package cmd

// Fixtures for the cmd-level branch tests: the ports-tree-shaped repo
// with one minted dockhand branch that the engine's own tests start
// from, built over gittest so the two packages share one fixture.

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/gh"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/git/gittest"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/runstate"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/verifytest"
)

// statusDoc is `status --json`'s published document, restated here
// rather than reached for. The renderer's own type is unexported on
// purpose — the document is a contract, not a value to pass around —
// and a test that names the keys it expects is checking the contract
// instead of agreeing with whatever the renderer currently marshals.
//
// There is no `cleaned` key (D27): status cleans nothing, so the key
// could never have been true, and a key that is always false is a
// promise the document cannot keep. `minted` took its place beside
// the branch, and `as_recorded` marks a --no-update read.
type statusDoc struct {
	Repository string `json:"repository"`
	AsRecorded bool   `json:"as_recorded"`
	Branches   []struct {
		Branch  string          `json:"branch"`
		Minted  bool            `json:"minted"`
		Tip     string          `json:"tip"`
		Note    *record.Record  `json:"note"`
		Drift   string          `json:"drift"`
		PR      *gh.PullRequest `json:"pr"`
		PRError string          `json:"pr_error"`
		Error   string          `json:"error"`
	} `json:"branches"`
	OrphanWorkers []struct {
		Name  string `json:"name"`
		Owner string `json:"owner"`
	} `json:"orphan_workers"`
	// Exit is the twin every JSON document carries: how the run ended,
	// said inside the document as well as beside it, for a consumer
	// that took stdout through a pipe and lost $?.
	Exit exitcode.Twin `json:"exit"`
}

// lifecycleRepo is a ports-tree-shaped git repo with one dockhand
// branch minted, its tip returned alongside.
func lifecycleRepo(t *testing.T) (*git.Repo, string) {
	t.Helper()
	ctx := context.Background()
	repo := gittest.PortsTree(t, testFinder())
	primary, err := repo.PrimaryBranch(ctx)
	require.NoError(t, err)
	sha := gittest.Commit(t, repo, "dockhand/jq-1.8", primary, "sysutils/jq/Portfile",
		"version 1.8\n", "jq: update to 1.8")
	return repo, sha
}

func TestStatusJSONReportsTheSettledTruth(t *testing.T) {
	repo, sha := lifecycleRepo(t)
	fake := &verifytest.Fake{
		States: map[string]verify.Status{"fake-1": {State: verify.Passed, Handle: "fake-1"}},
		Logs:   map[string]string{"fake-1": "--->  0 errors and 0 warnings found.\n"},
	}
	writeRuns(t, repo, sha, map[string]platRun{"Testos": runningOn("fake-1")})

	var out, errb bytes.Buffer
	rs := &runstate.Context{TreeRoot: repo.Root, Tools: testFinder(), Out: &out, Err: &errb,
		Verifier: func(context.Context) (verify.Verifier, error) { return fake, nil }}
	require.NoError(t, statusAction{json: true}.Execute(context.Background(), rs))

	var got statusDoc
	require.NoError(t, json.Unmarshal(out.Bytes(), &got), "stdout must be one JSON document: %s", out.String())
	assert.False(t, got.AsRecorded, "the pass polled")
	require.Len(t, got.Branches, 1)
	b := got.Branches[0]
	assert.Equal(t, "dockhand/jq-1.8", b.Branch)
	assert.True(t, b.Minted)
	assert.Equal(t, sha, b.Tip)
	require.NotNil(t, b.Note)
	// The run is keyed by subject and platform both, from day one: the
	// day a cohort lands, nothing has to re-key the notes that exist.
	run := b.Note.Runs[record.RunKey("jq", "Testos")]
	assert.Equal(t, record.Passed, run.State, "the JSON mode settles, same as the human one")
	assert.Equal(t, "clean", run.Lint)
	assert.Equal(t, "Testos", run.Platform, "the platform is on the run as well as in the key")
	assert.True(t, b.Note.Jobs["Testos"].Released, "a green environment is a wasted slot")
	assert.Nil(t, b.PR, "an unpromoted branch carries no PR object")
	assert.Equal(t, exitcode.Of(exitcode.OK, ""), got.Exit,
		"the document says how the run ended, for a consumer that lost $?")
}

// A pass that never reached a report still publishes how it ended. The
// caller asked for JSON; giving them an English sentence on stderr and
// an empty stdout is the blind spot --plan's decline document was added
// to close, and leaving it open for the other JSON verb would have made
// the promise in docs/cli.md — that a consumer which lost $? still
// knows how the run ended — true only when nothing went wrong.
func TestStatusJSONSaysHowAFailedPassEnded(t *testing.T) {
	var out, errb bytes.Buffer
	rs := &runstate.Context{TreeRoot: t.TempDir(), Tools: testFinder(), Out: &out, Err: &errb}

	err := statusAction{json: true}.Execute(context.Background(), rs)

	require.ErrorIs(t, err, git.ErrNotARepo, "the error still travels; the document does not replace it")
	var got statusDoc
	require.NoError(t, json.Unmarshal(out.Bytes(), &got), "stdout must be one JSON document: %s", out.String())
	assert.Equal(t, exitcode.Of(exitcode.NotARepo, "not-a-repo"), got.Exit)
	assert.Equal(t, got.Exit.Code, ExitCode(err), "the document and $? are one fact")
	assert.Empty(t, got.Branches, "a pass that did not run reports no branches")

	// And the human mode is untouched: it says nothing on stdout.
	out.Reset()
	require.Error(t, statusAction{}.Execute(context.Background(), rs))
	assert.Empty(t, out.String())
}

// `status --json` over a merged pull request cleans nothing (D27): the
// branch stands, no demolition prose reaches either stream, and the
// document carries no `cleaned` key at all — a consumer reading the
// old key as "not yet" would be reading a promise status no longer
// makes. What it does say is that the pull request merged, which is
// what `dockhand cycle` will act on.
func TestStatusJSONNeverCleans(t *testing.T) {
	repo, _ := lifecycleRepo(t)
	fake := &verifytest.Fake{}
	gh := &ghFake{login: "herbygillot",
		ownPRs: `[{"number":9,"state":"closed","merged_at":"2026-09-01T00:00:00Z","html_url":"https://x/9"}]`}

	// Promote-shape the branch: a tracked remote is what makes judge
	// look the PR up.
	gittest.BareFork(t, repo, "herbygillot", "herby")
	require.NoError(t, repo.Push(context.Background(), "herby", "dockhand/jq-1.8"))

	var out, errb bytes.Buffer
	rs := &runstate.Context{TreeRoot: repo.Root, Tools: testFinder(), Out: &out, Err: &errb, Gh: gh.run,
		Verifier: func(context.Context) (verify.Verifier, error) { return fake, nil }}
	require.NoError(t, statusAction{json: true}.Execute(context.Background(), rs))

	var got statusDoc
	require.NoError(t, json.Unmarshal(out.Bytes(), &got), "stdout must be one JSON document: %s", out.String())
	require.Len(t, got.Branches, 1)
	require.NotNil(t, got.Branches[0].PR, "the merged pull request is reported")
	assert.True(t, got.Branches[0].Minted)
	assert.NotContains(t, out.String(), `"cleaned"`, "the key retired with the deletion")
	assert.NotContains(t, errb.String(), "discarded", "no demolition, so no demolition prose")
	assert.True(t, repo.HasBranch(context.Background(), "dockhand/jq-1.8"), "status deletes nothing")
}

// `status --json --no-update` is the ledger as written, and the
// document says so: `as_recorded` is true, a running run reads as it
// was recorded, and nothing was polled to learn otherwise.
func TestStatusJSONNoUpdateSaysAsRecorded(t *testing.T) {
	repo, sha := lifecycleRepo(t)
	writeRuns(t, repo, sha, map[string]platRun{"Testos": runningOn("fake-1")})

	var out, errb bytes.Buffer
	rs := &runstate.Context{TreeRoot: repo.Root, Tools: testFinder(), Out: &out, Err: &errb,
		Verifier: func(context.Context) (verify.Verifier, error) {
			t.Fatal("--no-update composed the verifier")
			return nil, nil
		}}
	require.NoError(t, statusAction{json: true, noUpdate: true}.Execute(context.Background(), rs))

	var got statusDoc
	require.NoError(t, json.Unmarshal(out.Bytes(), &got), "stdout must be one JSON document: %s", out.String())
	assert.True(t, got.AsRecorded)
	require.Len(t, got.Branches, 1)
	require.NotNil(t, got.Branches[0].Note)
	run := got.Branches[0].Note.Runs[record.RunKey("jq", "Testos")]
	assert.Equal(t, record.Running, run.State, "as written, not as it would settle")
	assert.False(t, got.Branches[0].Note.Jobs["Testos"].Released)
	assert.Empty(t, got.OrphanWorkers, "no worker audit was run")
}
