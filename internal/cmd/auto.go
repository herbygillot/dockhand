package cmd

// Auto mode: who is running this invocation, and how dockhand comes to
// know it.
//
// It is DECLARED, never inferred. A person is the invoker of every verb
// unless the invocation says otherwise, and there are exactly two ways
// to say otherwise: the persistent --auto flag on any verb, and
// DOCKHAND_AUTO in the environment. Nothing asks whether a terminal is
// attached. tool.IsTerminal exists and is right there, and reaching for
// it would make the answer depend on how the process was started rather
// than on what the operator said — a pipe, a CI runner or a `script`
// wrapper would each silently move a human's authority onto a machine,
// or the reverse, with nothing in the invocation to read.
//
// There used to be a third way, the `auto` verb, which was the
// unattended reconciler pass and the declaration in one. D27 retired
// it: the pass is `dockhand cycle`, and `dockhand cycle --auto` is the
// unattended entrypoint — the same pass under the same declaration
// every other verb takes, with the publish slot handed in because the
// invoker is the machine (ruled 2026-09-05 with D27's implementation,
// pending the maintainer). A verb that was its own declaration was one
// more way to say the same thing, and one a rename could quietly unhook.
//
// What the answer is FOR is provenance: the mint writes it onto the
// record as AskedBy, so a later question about how a change reached
// review is a query rather than an estimate. It is not an authorization.
// The one gate that turns on an invoker takes one as a parameter at its
// own call site; reading a Driver back off a record to decide what the
// unattended road may do would let a change authorize itself by
// claiming its own history.

import (
	"os"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/record"
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
)

// resolveInvoker answers who declared this run, once, before any Action
// executes. Resolving it per verb would let two verbs in one invocation
// disagree about the same invocation; resolving it here is what makes
// "declared, never inferred" a property a test can check rather than a
// convention.
//
// The flag decides, and the environment is what the flag falls back to
// when the command line did not mention it, so `--auto=false` withdraws
// a standing DOCKHAND_AUTO for one invocation and DOCKHAND_AUTO=1 cannot
// smuggle a machine into a verb a person typed --auto=false on.
func resolveInvoker(c *cobra.Command) (record.Driver, error) {
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
// cycle --auto`; a promote that published as the machine instead would
// be a second one, and `dockhand promote --auto` would be the bypass
// that makes the first one's gate decorative.
//
// The refusal is in the destination band because that is whose problem
// it is: the change is fine, and the road it was offered will not take
// it from this invoker.
type PromoteIsHumanError struct{}

func (e *PromoteIsHumanError) Error() string {
	return "promote publishes on a person's authority and this run declared itself unattended (`--auto` or " + autoEnv +
		"); the unattended road is `dockhand cycle --auto`, which publishes through the reconciler's own slot or not at all"
}

// DockhandExit: the refused band's machine gate — an automatic
// publication a policy refused, where a human asking for the same thing
// would be allowed it.
func (e *PromoteIsHumanError) DockhandExit() int { return exitcode.MachineGate }

// Code names the refusal for a machine.
func (e *PromoteIsHumanError) Code() string { return "promote-is-human" }
