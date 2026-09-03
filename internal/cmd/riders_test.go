package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/intent"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/record"
)

// The two switches are one choice, and the mapping is where a command
// line becomes a policy. The default is the zero value on purpose: a run
// that says nothing about riders gets them.
func TestRiderPolicyReadsThePairAsOneChoice(t *testing.T) {
	for _, tc := range []struct {
		name             string
		riders, noRiders bool
		want             intent.RiderPolicy
	}{
		{"neither", false, false, intent.RidersAlong},
		{"--riders", true, false, intent.RidersOnly},
		{"--no-riders", false, true, intent.RidersNone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := intentFlags{riders: tc.riders, noRiders: tc.noRiders}
			assert.Equal(t, tc.want, f.riderPolicy())
		})
	}
	var zero intentFlags
	assert.Equal(t, intent.RidersAlong, zero.riderPolicy(),
		"a run that says nothing about riders carries them")
}

// The combinations only the flag set can judge, judged at the cobra
// boundary. --riders against the verification switches is the
// interesting one: a housekeeping change moves nothing a build could
// disagree with, so asking a VM about it is asking a question with no
// possible answer.
func TestRiderFlagsRefuseTheContradictions(t *testing.T) {
	for _, tc := range []struct {
		name string
		f    intentFlags
	}{
		{"both ends of one policy", intentFlags{riders: true, noRiders: true}},
		{"--riders with --verify", intentFlags{riders: true, verifyIt: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.f.check()
			require.Error(t, err)
			assert.Equal(t, exitcode.Usage, ExitCode(err))
		})
	}

	f := intentFlags{riders: true}
	f.opts.Trace = true
	require.Equal(t, exitcode.Usage, ExitCode(f.check()), "--trace follows a verification there is none of")
}

// A housekeeping change is minted and left alone. The destination is the
// record's own word, as --no-verify writes it, because the drain reads
// it back to know that nobody is owed a verdict.
func TestRidersMintWithoutAskingForAVerdict(t *testing.T) {
	f := intentFlags{riders: true}
	require.NoError(t, f.check())
	assert.Equal(t, record.ToBranch, f.opts.Destination)

	var along intentFlags
	require.NoError(t, along.check())
	assert.NotEqual(t, record.ToBranch, along.opts.Destination,
		"an ordinary change still asks for its verdict")
}

// --riders drops the headline, so the verb's own parameter checks are
// moot and skipped rather than answered. A revbump's --reason is the
// case that shows it: demanding a justification for an edit nobody is
// making would be a refusal about a change that is not being planned.
func TestRidersSkipsTheVerbsOwnParameterChecks(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, macports.PortfileName),
		[]byte("PortSystem 1.0\nname anyport\nversion 1.0\n"), 0o644))

	assert.Equal(t, exitcode.Usage, code(t, "bump-revision", dir, "--plan"),
		"without --riders the reason is still required")
	assert.NotEqual(t, exitcode.Usage, code(t, "bump-revision", dir, "--plan", "--riders"),
		"with it the verb's parameters are not read, so a missing one is not a contradiction")
}

// A verb's caution belongs to its headline edit, and --riders plans no
// headline. refresh's caution — upstream re-rolled the artifact,
// establish why before this goes anywhere public — was printed over a
// housekeeping plan whose single edit is a comment line and whose
// predicted delta is empty, with nothing fetched and no checksum
// compared. That is a cause named for an event that did not occur, in
// the operator-facing stream, which is the same untruth the bodies were
// cleaned of in this step.
func TestRidersDoNotPrintTheVerbsCaution(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, macports.PortfileName),
		[]byte("PortSystem 1.0\nname anyport\nversion 1.0\n"), 0o644))

	var out, errb bytes.Buffer
	require.Equal(t, exitcode.OK,
		execute(context.Background(), "test", []string{"refresh-checksums", dir, "--plan", "--riders"}, &out, &errb))
	assert.Contains(t, out.String(), `"modeline"`, "the housekeeping plan is what was made")
	assert.NotContains(t, errb.String(), "supply-chain",
		"no checksum moved, so nothing is owed the caution")
	assert.NotContains(t, errb.String(), "re-rolled the artifact")
}

// Every verb's planner receives the run's rider policy.
//
// The catalogue test proves the flag is REGISTERED on all three verbs;
// this proves it is wired. bump-revision registered --no-riders and
// dropped it on the floor: its New built a planner without the field, so
// the switch was accepted, documented and inert, and a maintainer who
// asked for an undistracted revbump diff got a modeline in the branch,
// the note and the pull request body. Nothing in the suite could see it,
// because every test that exercised the policy did so at the intent
// layer where the field was already set.
//
// Two planners built from the same parameters under different policies
// must differ. That is the whole assertion, and it holds for a fourth
// verb without being written again — which is the point, since the flag
// is declared once for all of them.
func TestEveryVerbCarriesTheRiderPolicyIntoItsPlanner(t *testing.T) {
	for _, v := range intentCatalogue() {
		t.Run(v.Name, func(t *testing.T) {
			base := intent.Params{Version: "1.0", Reason: "a stated reason"}
			for _, policy := range []intent.RiderPolicy{intent.RidersNone, intent.RidersOnly} {
				along, err := v.New(base)
				require.NoError(t, err)
				asked := base
				asked.Riders = policy
				got, err := v.New(asked)
				require.NoError(t, err)
				assert.NotEqual(t, along, got,
					"%s builds the same planner whichever rider policy the run carries, "+
						"so the flag is accepted and ignored", v.Name)
			}
		})
	}
}

// And the same fact from the outside, on the verb the wire was cut on: a
// plan asked to carry no housekeeping carries none.
func TestNoRidersReachesTheBumpRevisionPlan(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, macports.PortfileName),
		[]byte("PortSystem 1.0\nname anyport\nversion 1.0\nrevision 0\n"), 0o644))

	var out bytes.Buffer
	require.Equal(t, exitcode.OK,
		executeOut(t, &out, "bump-revision", dir, "--plan", "--reason", "probe", "--no-riders"))
	var withoutRiders planDoc
	require.NoError(t, json.Unmarshal(out.Bytes(), &withoutRiders), "stdout must be one plan: %s", out.String())
	assert.Nil(t, withoutRiders.Riders, "--no-riders carries none")
	assert.Len(t, withoutRiders.Edits, 1, "the revision line, and nothing else")

	// The default is what says the flag was doing something: the same
	// Portfile opens without a modeline, so the modeline rides.
	out.Reset()
	require.Equal(t, exitcode.OK,
		executeOut(t, &out, "bump-revision", dir, "--plan", "--reason", "probe"))
	var withRiders planDoc
	require.NoError(t, json.Unmarshal(out.Bytes(), &withRiders))
	assert.Equal(t, []string{"modeline"}, withRiders.Riders)
	assert.Len(t, withRiders.Edits, 2)
}

// planDoc reads back the parts of a --plan document these tests judge.
type planDoc struct {
	Riders []string         `json:"riders"`
	Edits  []map[string]any `json:"edits"`
}

// executeOut runs the command tree the way main does, with stdout
// captured and stderr discarded.
func executeOut(t *testing.T, out *bytes.Buffer, args ...string) int {
	t.Helper()
	return execute(context.Background(), "test", args, out, io.Discard)
}
