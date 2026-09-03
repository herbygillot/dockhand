package cmd

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/runstate"
	"github.com/herbygillot/dockhand/internal/verdict"
)

// verifyAction proves a port builds as it sits, in a pristine
// environment, without writing anything. This is state verification —
// it tests the portdir's current content, whoever produced it, which
// is what makes human edits after a dockhand change verifiable at all.
type verifyAction struct {
	target string
	on     []string
	test   bool
	trace  bool
}

var _ Action = verifyAction{}

func (a verifyAction) Execute(ctx context.Context, rs *runstate.Context) error {
	// The in-flight branch wins, resolved the way every sibling verb
	// resolves it — a branch name outright, or a port name that names
	// exactly one dockhand branch. The branch is the unit (D21), and
	// verifying one means verifying its tip sha, whoever made it. A
	// target with no in-flight branch falls through to state
	// verification of the working tree, which a portdir path always
	// reaches directly.
	eng := rs.Deps()
	if repo, err := rs.Repo(ctx); err == nil {
		branch, rerr := eng.Resolve(ctx, repo, a.target)
		var ambiguous *verdict.AmbiguousTargetError
		switch {
		case rerr == nil:
			return verifyBranch(ctx, rs, eng, repo, a.target, branch, a.on, a.test, a.trace)
		case errors.As(rerr, &ambiguous):
			// The fall-through below verifies the working tree, which is
			// the right answer for a target with no branch and the wrong
			// one for a target with several: silently verifying something
			// the user did not name is the failure this refusal exists to
			// prevent.
			return rerr
		}
	}
	var single string
	if len(a.on) > 1 {
		return usagef("state verification of a portdir takes one release; a branch takes lists and \"all\"")
	}
	if len(a.on) == 1 {
		single = a.on[0]
	}
	release, err := releaseFlag(single)
	if err != nil {
		return err
	}
	targets, err := resolveTargets(rs, false, []string{a.target})
	if err != nil {
		return err
	}
	if len(targets) != 1 {
		return usagef("verify takes exactly one port; %q names %d", a.target, len(targets))
	}
	portName := targets[0].Subport
	if portName == "" {
		portName = filepath.Base(filepath.Clean(targets[0].Portdir))
	}
	// State verification takes the archive as it finds it. Whether a
	// port's binary archive still matches the bytes under test is a
	// property of the CHANGE — a version bump names an archive that does
	// not exist yet, a checksum refresh leaves one that predates it —
	// and state verification has no change to read: it tests a portdir
	// as it sits, whoever wrote it. Forcing a source build for every
	// invocation would spend an hour to answer a question nobody asked,
	// so the answer is the same one MacPorts would give unprompted.
	_, err = eng.RunVerification(ctx, portName, targets[0].Portdir, release, a.test, false)
	return err
}

// verifyBranch submits a branch's tip for verification in the
// background, exactly as the mint that created it would have: the
// changed portdir is derived from git — diff against the merge base
// with the primary branch, so a human commit's changes count too — and
// materialized from the object database. A job already running for the
// tip is left alone; a running job the branch has moved past is
// canceled first, its worker released and its note marked superseded,
// because a verdict about an abandoned sha is a slot spent on nothing.
// Each release's submit is a claim under the repository's submit lock,
// the same claim status's pump makes: a verify racing a status over
// one deferred run used to start it twice.
func verifyBranch(ctx context.Context, rs *runstate.Context, eng *engine.Engine, repo *git.Repo, target, branch string, on []string, test, trace bool) error {
	tip, err := repo.RevParse(ctx, branch)
	if err != nil {
		return err
	}
	prov, err := rs.VerifyProvider(ctx)
	if err != nil {
		return err
	}
	releases, err := resolveReleaseSet(on, prov.Capabilities().Platforms, true)
	if err != nil {
		return err
	}
	if trace && len(releases) > 1 {
		return usagef("--trace follows one build; name one release with --on")
	}
	if err := eng.SupersedeStale(ctx, repo, branch, tip); err != nil {
		return err
	}

	rel, err := engine.ChangedPortdirs(ctx, repo, branch, tip)
	if err != nil {
		return err
	}
	portName, err := eng.SubjectOf(ctx, repo, target, branch, tip, rel)
	if err != nil {
		return err
	}

	var deferred int
	for _, r := range releases {
		started, err := eng.SubmitRelease(ctx, repo, branch, tip, rel, portName, r, test)
		var vde *engine.VerifyDeferredError
		if errors.As(err, &vde) {
			// No slot for this platform right now: recorded, reported,
			// and the remaining releases still get their chance.
			deferred++
			continue
		}
		if err != nil {
			return err
		}
		if trace && started {
			if err := eng.FollowStarted(ctx, repo, tip, portName, r.Name, prov); err != nil {
				return err
			}
		}
	}
	if deferred > 0 {
		return &engine.VerifyDeferredError{Branch: branch,
			// Deferrals have different remedies — a full machine frees on
			// its own, a missing capability needs provisioning — and the
			// per-release lines above name each one. A field run caught
			// this summary promising "when a slot frees" to a deferral
			// no freed slot would ever help.
			Reason: fmt.Sprintf("%d release(s) deferred — each line above names its remedy; `dockhand status` retries them as remedies are met", deferred)}
	}
	return nil
}

// Verify builds the verify subcommand.
func Verify() *cobra.Command {
	var on []string
	var test, trace bool
	c := &cobra.Command{
		Use:   "verify <branch|port|subport|portdir>",
		Short: "Verify a branch's tip in a pristine VM — or a port as it sits",
		Args:  exactArgs(1),
		RunE: runE(func(_ *cobra.Command, args []string) (Action, error) {
			return verifyAction{target: args[0], on: on, test: test, trace: trace}, nil
		}),
	}
	c.Flags().StringSliceVar(&on, "on", nil,
		`macOS releases to verify on, or "all" (default: the newest provisioned base)`)
	c.Flags().BoolVar(&test, "test", false,
		"also run the port's test suite (`port test`) after the install")
	c.Flags().BoolVar(&trace, "trace", false,
		"stay attached after submitting: stream the build log until it finishes")
	return c
}
