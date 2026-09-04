package cmd

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/runstate"
)

// The two verbs that answer a proposal. A finding proposes and never
// executes, so the proposal and the answer to it are two acts, and
// these are the two answers: build the cohort, or say no and have that
// recorded.
//
// Both are deliberately small. What accepting a proposal IS — planning
// each member from the branch tip, grafting one commit, inheriting the
// evidence, submitting the cohort's own verification — is the engine's,
// for the reason promote's is: a sweep will one day answer a proposal
// the same way, and the two must not be two implementations.

// cohortAction accepts a branch's revbump proposal: one more commit on
// the branch that already carries the change, revbumping the dependents
// the measurement put forward.
//
// It names a BRANCH and not a port, which is what makes it a different
// invocation of the same verb rather than a different verb. A revision
// bump is a revision bump; what --for changes is who decides which
// ports get one — here it is a proposal a person is accepting, and the
// reason is the criterion that proposal was measured on, so the verb's
// own --reason has nothing to add.
type cohortAction struct {
	branch   string
	noVerify bool
	test     bool
	trace    bool
	on       string
	exclude  []string
}

var _ Action = cohortAction{}

func (a cohortAction) Execute(ctx context.Context, rs *runstate.Context) error {
	repo, err := rs.Repo(ctx)
	if err != nil {
		return err
	}
	release, err := releaseFlag(a.on)
	if err != nil {
		return err
	}
	return rs.Deps().BuildCohort(ctx, repo, a.branch, engine.CohortOpts{
		NoVerify: a.noVerify, Test: a.test, Trace: a.trace, Platform: release, Exclude: a.exclude})
}

// dismissAction records that a person looked at a branch's proposals
// and said no.
//
// It changes no bytes and cancels nothing: the measurement stays on the
// note, and what moves is the answer to it. That is the whole point of
// recording a dismissal rather than deleting a finding — a proposal
// that vanished when declined would be proposed again on the next pass,
// and the pull request would lose the fact that a person considered it.
type dismissAction struct {
	target string
}

var _ Action = dismissAction{}

func (a dismissAction) Execute(ctx context.Context, rs *runstate.Context) error {
	repo, err := rs.Repo(ctx)
	if err != nil {
		return err
	}
	return rs.Deps().Dismiss(ctx, repo, a.target)
}

// Dismiss builds the dismiss subcommand.
func Dismiss() *cobra.Command {
	return &cobra.Command{
		Use:   "dismiss <branch|port>",
		Short: "Record that you looked at a branch's proposals and said no",
		Args:  exactArgs(1),
		RunE: runE(func(_ *cobra.Command, args []string) (Action, error) {
			return dismissAction{target: args[0]}, nil
		}),
	}
}

// cohortMode declares bump-revision's plural invocation: the --for flag
// that names a branch, and the action it produces.
//
// It reuses the shared realization flags rather than declaring its own,
// which is not thrift: --no-verify and --on mean exactly what they mean
// on the single-port road — do not submit a verification, and verify on
// this release — and a cohort mode with its own spellings for them
// would be two vocabularies for one question.
//
// The combinations it refuses are the ones that would silently do
// nothing. --plan, --diff and --in-place are realizations of a plan the
// caller is holding, and a cohort is N plans grafted onto a branch that
// already exists; there is no single document to print and no working
// tree to edit. --verify is the pre-mint gate, and nothing is minted
// here: the branch already exists and its evidence is inherited.
//
// --test and --trace are carried rather than refused, because the
// cohort's own verification is a verification: each member's rev+1
// names an archive that does not exist, so it rebuilds from source
// against the new library, and that rebuild is the evidence the whole
// proposal was for.
func cohortMode(c *cobra.Command, f *intentFlags) func() (Action, error) {
	var branch string
	var exclude []string
	c.Flags().StringVar(&branch, "for", "",
		"accept the revbump proposal on this branch: revbump its dependents as one more commit (takes no port argument)")
	c.Flags().StringSliceVar(&exclude, "exclude", nil,
		"leave these members out of the change entirely: not bumped, not built (comma-separated)")
	return func() (Action, error) {
		if branch == "" {
			if len(exclude) > 0 {
				return nil, usagef("--exclude names members of a proposal; it needs the --for that accepts one")
			}
			return nil, nil
		}
		switch {
		case f.opts.PlanOnly, f.opts.Diff, f.opts.InPlace:
			return nil, usagef("--for grafts a commit onto an existing branch; it takes neither --plan, --diff nor --in-place")
		case f.riders, f.noRiders:
			return nil, usagef("--for carries no housekeeping riders: a cohort commit revbumps other people's ports and makes no other edit")
		case f.replace:
			return nil, usagef("--for extends a branch rather than minting one; --replace has nothing to replace")
		case f.verifyIt:
			return nil, usagef("--verify gates a mint, and --for mints nothing: the cohort's own verification is submitted after the commit unless --no-verify says otherwise")
		}
		return cohortAction{branch: branch, noVerify: f.noVerify,
			test: f.opts.Test, trace: f.opts.Trace, on: f.on, exclude: exclude}, nil
	}
}
