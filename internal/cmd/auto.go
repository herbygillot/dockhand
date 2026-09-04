package cmd

// Auto mode: who is running this invocation, and how dockhand comes to
// know it.
//
// It is DECLARED, never inferred. A person is the invoker of every verb
// unless the invocation says otherwise, and there are exactly three ways
// to say otherwise: the `auto` verb (the unattended reconciler pass a
// cron or launchd entry runs), the persistent --auto flag on any verb,
// and DOCKHAND_AUTO in the environment. Nothing asks whether a terminal
// is attached. tool.IsTerminal exists and is right there, and reaching
// for it would make the answer depend on how the process was started
// rather than on what the operator said — a pipe, a CI runner or a
// `script` wrapper would each silently move a human's authority onto a
// machine, or the reverse, with nothing in the invocation to read.
//
// What the answer is FOR is provenance: the mint writes it onto the
// record as AskedBy, so a later question about how a change reached
// review is a query rather than an estimate. It is not an authorization.
// The one gate that turns on an invoker takes one as a parameter at its
// own call site; reading a Driver back off a record to decide what the
// unattended road may do would let a change authorize itself by
// claiming its own history.

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/runstate"
)

const (
	// autoFlag is the persistent flag's name, and autoEnv its
	// environment spelling — the same pairing --prefix and --tree have,
	// so an unattended machine can declare itself once in a launchd plist
	// instead of on every verb.
	autoFlag = "auto"
	autoEnv  = "DOCKHAND_AUTO"

	// agentEnv is the AI agent marker. It names which agent was driving
	// and nothing else: it is recorded beside AskedBy and read by no
	// gate, so setting it can neither grant nor withhold anything.
	agentEnv = "AI_AGENT"

	// autoVerbAnnotation marks the verb that IS the declaration. An
	// annotation rather than a comparison against the command's name:
	// the name is display text, and a rename that quietly unhooked the
	// unattended entrypoint from unattended mode is the one edit this
	// must survive.
	autoVerbAnnotation = "dockhand/declares-auto"
)

// resolveInvoker answers who declared this run, once, before any Action
// executes. Resolving it per verb would let two verbs in one invocation
// disagree about the same invocation; resolving it here is what makes
// "declared, never inferred" a property a test can check rather than a
// convention.
//
// The order is the order of specificity. The `auto` verb is the
// declaration itself and cannot be talked out of it — an unattended
// reconciler pass is unattended whatever else the command line says.
// Otherwise the flag decides, and the environment is what the flag
// falls back to when the command line did not mention it, so
// `--auto=false` withdraws a standing DOCKHAND_AUTO for one invocation
// and DOCKHAND_AUTO=1 cannot smuggle a machine into a verb a person
// typed --auto=false on.
func resolveInvoker(c *cobra.Command) (record.Driver, error) {
	if _, declared := c.Annotations[autoVerbAnnotation]; declared {
		return record.Machine, nil
	}
	auto, err := c.Flags().GetBool(autoFlag)
	if err != nil {
		return "", err
	}
	if !c.Flags().Changed(autoFlag) {
		if auto, err = autoFromEnv(); err != nil {
			return "", err
		}
	}
	if auto {
		return record.Machine, nil
	}
	return record.Human, nil
}

// autoFromEnv reads the environment's declaration.
//
// Unset and empty are no declaration. Anything else must parse as a
// boolean and is a usage error when it does not: the two ways to be
// lenient here are to read an unrecognized value as true — which hands
// a machine a person's authority over a typo — and to read it as false,
// which silently ignores what an operator wrote into a plist and never
// says so.
func autoFromEnv() (bool, error) {
	v, ok := os.LookupEnv(autoEnv)
	if !ok || v == "" {
		return false, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, usagef("%s=%q is not a boolean; set it to 1 to declare this machine unattended, or unset it", autoEnv, v)
	}
	return b, nil
}

// PromoteIsHumanError is promote meeting a run that declared itself
// unattended.
//
// promote publishes on a person's authority: it is the verb a
// maintainer types when they have looked at the change and decided to
// spend a reviewer's attention on it. There is exactly one machine
// publish path and it is the reconciler's own slot, reached by `dockhand
// auto`; a promote that published as the machine instead would be a
// second one, and `dockhand promote --auto` would be the bypass that
// makes the first one's gate decorative.
//
// The refusal is in the destination band because that is whose problem
// it is: the change is fine, and the road it was offered will not take
// it from this invoker.
type PromoteIsHumanError struct{}

func (e *PromoteIsHumanError) Error() string {
	return "promote publishes on a person's authority and this run declared itself unattended (`--auto`, " + autoEnv +
		", or `dockhand auto`); the unattended road is `dockhand auto`, which publishes through the reconciler's own slot or not at all"
}

// DockhandExit: the refused band's machine gate — an automatic
// publication a policy refused, where a human asking for the same thing
// would be allowed it.
func (e *PromoteIsHumanError) DockhandExit() int { return exitcode.MachineGate }

// Code names the refusal for a machine.
func (e *PromoteIsHumanError) Code() string { return "promote-is-human" }

// autoAction is one unattended reconciler pass: observe every branch,
// judge what it found, retire what merged, consider what was asked to be
// published, and start what was deferred.
//
// It is the same pass `status` runs, and deliberately so — the sweep and
// the report reached the same verdicts by two code paths once, and the
// only way to know they agreed was to run both. What differs is who is
// asking: this verb declares the invoker a machine, and the roads that
// turn on an invoker read that declaration instead of guessing at it.
//
// THIS IS THE ONE VERB THAT HANDS IN A PUBLISH SLOT. `status` and
// `clean` pass none and must not — neither may open a pull request as a
// side effect of being run — so the slot's road, its per-pass cap, its
// pacing and its 62-while-pending exit are reachable from here and from
// nowhere else. On this build the road refuses at its first line, which
// means wiring it changes no observable behaviour today; that is the
// point. The alternative was to leave the entrypoint unwired and
// discover on the day the constant flips that the flip alone published
// nothing, which is the second change the constant was supposed not to
// need.
//
// The worker audit is not asked for. It is a rendering `status` wants
// and this pass has nobody to render it to.
type autoAction struct{}

var _ Action = autoAction{}

func (autoAction) Execute(ctx context.Context, rs *runstate.Context) error {
	slot := &engine.PublishSlot{}
	rep, err := rs.Deps().Reconcile(ctx, engine.ReconcileOpts{Drain: true, Publish: slot})
	if err != nil {
		return err
	}
	rep.Text(rs.Out, rs.Err)
	sayPassRefusal(rs, slot)
	// What the pass should exit with, which is the WAITING and never the
	// refusing: see PublishSlot.Outcome. A change whose verification is
	// still going, a forge that would not answer, a cap that stopped the
	// pass short — those are 62, ask again later. A closed road is not.
	return slot.Outcome()
}

// sayPassRefusal states a refusal that was about the PASS rather than
// about any branch — today, the build gate, asked once before the slot
// considers a single candidate.
//
// Branch-scoped outcomes are already under their branches in the report,
// which is where every other phase's prose goes. This one has no branch
// to go under, and printing it under each of them would bury the report
// it was printed in. It is on stderr, once, so an operator reading a
// cron log can tell a pass that considered publication and was refused
// from a pass that never had a publish road at all.
func sayPassRefusal(rs *runstate.Context, slot *engine.PublishSlot) {
	for _, r := range slot.Results {
		if r.Err != nil && r.Branch == "" {
			fmt.Fprintln(rs.Err, r.Err)
		}
	}
}

// Auto builds the auto subcommand: the entrypoint a cron or launchd
// entry names.
func Auto() *cobra.Command {
	return &cobra.Command{
		Use:   "auto",
		Short: "Run one unattended reconciler pass, as the machine",
		Long: "Run one unattended reconciler pass: observe every dockhand branch, retire what\n" +
			"merged, consider what was asked to be published, and start what was deferred.\n\n" +
			"Publication is the reconciler's own slot and is refused on this build: no machine\n" +
			"may spend a reviewer's attention until the trust ladder has been ruled on.\n\n" +
			"This verb declares the invoker to be the machine rather than a person. That is\n" +
			"a declaration and never a detection — dockhand does not ask whether a terminal\n" +
			"is attached — and it is recorded as provenance on what the pass mints. The\n" +
			"same declaration is available on any verb as --auto or as " + autoEnv + "=1.",
		Args:        noArgs,
		Annotations: map[string]string{autoVerbAnnotation: "the verb is the declaration"},
		RunE: runE(func(*cobra.Command, []string) (Action, error) {
			return autoAction{}, nil
		}),
	}
}
