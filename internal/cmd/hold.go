package cmd

import (
	"context"
	"time"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/runstate"
)

// holdAction stops a change from proceeding until a person releases it.
//
// A hold is the brake, and it is deliberately the bluntest thing in the
// tool: it changes no bytes, cancels nothing that is already running,
// and reaches no network. All it does is write a sentence onto the
// change's record, and every road that would act on that change asks
// first — the publication, the verification the pump would start, the
// deletion a merged pull request would earn.
//
// It is a person's instrument and it refuses a person too. Every other
// gate of this shape in the tool is nil for a human by construction,
// because a human standing there is the whole argument for the looser
// rule; this one has the opposite argument. The commonest use is
// somebody stopping themselves — a change they want to sit on over a
// release, an upstream they want to hear from first — and a hold that
// `dockhand promote` walked past would be a note nobody had to obey.
type holdAction struct {
	target string
	reason string
}

var _ Action = holdAction{}

func (a holdAction) Execute(ctx context.Context, rs *runstate.Context) error {
	repo, err := rs.Repo(ctx)
	if err != nil {
		return err
	}
	// The clock read is the verb's, handed down. record stamps nothing
	// itself — the wire format is a leaf, and a value that read its own
	// clock could not be pinned by a test — so the moment a hold went on
	// is always the caller's, here as at the re-witness that holds on a
	// checksum mismatch.
	return rs.Deps().Hold(ctx, repo, a.target, a.reason, time.Now())
}

// Hold builds the hold subcommand.
func Hold() *cobra.Command {
	var reason string
	c := &cobra.Command{
		Use:   "hold <branch|port>",
		Short: "Stop a change from publishing, verifying or being cleaned",
		Long: `Stop a change from proceeding until you release it.

A held change is passed over by every road that would act on it: it is
not published, the deferred runs on it are not started, and a merged
pull request does not retire its branch. Nothing is undone and nothing
is canceled — a verification already running finishes and records its
verdict, because a hold is about what happens next.

The reason is a sentence for whoever reads the branch later, yourself
included. Holding an already-held branch is refused rather than
silently rewriting the reason somebody gave; ` + "`dockhand unhold`" + ` first.

Changes minted against a prerelease target are held automatically: a
release candidate is a legitimate thing to ask for by name, and not
something dockhand carries onward on its own.`,
		Args: exactArgs(1),
		RunE: runE(func(_ *cobra.Command, args []string) (Action, error) {
			return holdAction{target: args[0], reason: reason}, nil
		}),
	}
	c.Flags().StringVarP(&reason, "message", "m", "",
		"why the change is being held, recorded on its note")
	return c
}

// unholdAction releases a hold. It starts nothing: the next pass picks
// the change up on its own terms, and a release that also submitted
// would make lifting a hold a bigger act than placing one.
type unholdAction struct {
	target string
}

var _ Action = unholdAction{}

func (a unholdAction) Execute(ctx context.Context, rs *runstate.Context) error {
	repo, err := rs.Repo(ctx)
	if err != nil {
		return err
	}
	return rs.Deps().Unhold(ctx, repo, a.target)
}

// Unhold builds the unhold subcommand.
func Unhold() *cobra.Command {
	return &cobra.Command{
		Use:   "unhold <branch|port>",
		Short: "Release a held change so it can proceed again",
		Long: `Release a hold.

Nothing is started by releasing it: the next ` + "`dockhand cycle`" + ` pass
starts what was deferred, and a promotion is still yours to ask for.
Releasing a branch nothing is holding is refused, so a script cannot
read "the hold is lifted" out of a hold that was never there.`,
		Args: exactArgs(1),
		RunE: runE(func(_ *cobra.Command, args []string) (Action, error) {
			return unholdAction{target: args[0]}, nil
		}),
	}
}
