package cmd

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/intent"
)

// One flag answered two questions until S10 — what to do about a branch
// already in flight, and whether to re-derive a port at the version it
// already carries — so a user who wanted the second bought the first as
// a side effect, which on a standing branch is a demolition. These are
// the assertions that the split happened and stays split.

// --force is gone from the intent verbs, and gone loudly: cobra refuses
// an unknown flag rather than ignoring it, so a script still typing it
// stops instead of quietly replacing a branch it did not mean to.
func TestIntentVerbsNoLongerTakeForce(t *testing.T) {
	for _, verb := range intentCatalogue() {
		t.Run(verb.Name, func(t *testing.T) {
			tr := captureExecute(t, verb.Name, "--force", "jq")
			assert.Equal(t, exitcode.Usage, tr.exit)
			assert.Contains(t, tr.stderr, "unknown flag: --force")
		})
	}
}

// promote's --force keeps its name. It is git's own word for what it
// does, and it does a different thing to a different object: a
// force-push, with lease, of a branch dockhand already published.
func TestPromoteKeepsForceAndSaysItIsNotReplace(t *testing.T) {
	c := Promote()
	f := c.Flags().Lookup("force")
	require.NotNil(t, f, "promote's --force is not part of the split")
	assert.Contains(t, f.Usage, "with lease")
	assert.Contains(t, f.Usage, "--replace",
		"the help text is where a reader is told the two are not the same act")
	assert.Nil(t, c.Flags().Lookup("replace"), "promote pushes; it demolishes nothing local")
}

// The two halves land where they belong: --replace on every intent,
// because any of them can meet a branch in flight, and --recheck on
// bump alone, because only a bump has a version to re-derive at.
func TestTheSplitLandsOnTheRightVerbs(t *testing.T) {
	for _, c := range intentCommands() {
		t.Run(c.Name(), func(t *testing.T) {
			assert.NotNil(t, c.Flags().Lookup("replace"))
			assert.Nil(t, c.Flags().Lookup("force"))
			if c.Name() == "bump" {
				assert.NotNil(t, c.Flags().Lookup("recheck"))
				return
			}
			assert.Nil(t, c.Flags().Lookup("recheck"),
				"a revbump and a refresh have no version to re-derive at")
		})
	}
}

// --replace is the in-flight policy and nothing else, so it needs a
// realization that mints. Under --plan, --diff or --in-place it would
// be a flag that cannot act — which is how --force came to be read as
// the other thing in the first place.
func TestReplaceNeedsTheBranchRealization(t *testing.T) {
	for _, tc := range []struct {
		name string
		f    intentFlags
	}{
		{"--plan", intentFlags{replace: true, opts: engine.Policy{PlanOnly: true}}},
		{"--diff", intentFlags{replace: true, opts: engine.Policy{Diff: true}}},
		{"--in-place", intentFlags{replace: true, opts: engine.Policy{InPlace: true}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.f.check()
			require.Error(t, err)
			assert.Equal(t, exitcode.Usage, ExitCode(err))
			assert.Contains(t, err.Error(), "--replace")
		})
	}

	f := intentFlags{replace: true}
	require.NoError(t, f.check())
	assert.Equal(t, engine.Replace, f.opts.OnInFlight,
		"on the default realization it is the policy it was always meant to be")

	var none intentFlags
	require.NoError(t, none.check())
	assert.Equal(t, engine.Refuse, none.opts.OnInFlight, "refusing is the default")
}

// --recheck is a plan-time parameter with an engine consequence, and
// one flag must produce both: a re-derivation at a standing version
// names a binary archive that predates the change, so a pass earned by
// unpacking it would have verified nothing about the distfile the run
// went and fetched.
func TestRecheckIsBothAParameterAndAPolicy(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want bool
	}{
		{"a plain bump builds against the archive", []string{"--to", "2.0"}, false},
		{"--recheck asks for the source build", []string{"--to", "1.0", "--recheck"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var (
				f      intentFlags
				params intent.Params
			)
			c := &cobra.Command{Use: "bump"}
			check := bumpVerb.Flags(c, &params, &f)
			require.NotNil(t, check)
			require.NoError(t, c.Flags().Parse(tc.args))
			require.NoError(t, check())

			assert.Equal(t, tc.want, params.Recheck, "what the planner is told")
			assert.Equal(t, tc.want, f.opts.FromSource, "what the engine is told")
		})
	}
}
