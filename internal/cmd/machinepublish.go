package cmd

// The machine publish permission: one build-time constant, spent in one
// place, and false.
//
// Ring 3 is other people's attention. dockhand spends it by pushing the
// fork branch and by creating or editing a pull request against
// upstream, and nothing else it does costs anybody anything they did not
// ask for. The unattended road that would spend it — the reconciler's
// publish slot — is built, gated and tested; what has NOT been ruled on
// is the trust ladder that would say how many pull requests a machine
// may open, on which ports, at what pace, and on what record of past
// merges. Until that exists, the answer is no, and the answer is a
// constant rather than a flag so that no invocation, no environment
// variable and no configuration file can be the thing that changed it.
//
// THE ZERO VALUE IS THE REFUSAL, all the way down. This constant is
// spent into runstate.Context.MachinePublish and from there into
// engine.Deps.MachinePublish, a bool named for the permission GRANTED,
// so every engine built anywhere else — every test, every future
// composition root, every caller who never heard of this file — refuses
// unattended publication because it never granted it. A field named for
// a permission withheld would have inverted that and nobody would have
// noticed until a pass had opened pull requests.

import (
	"context"
	"strings"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/gh"
	"github.com/herbygillot/dockhand/internal/runstate"
)

// machinePublishEnabled is whether THIS BUILD lets a machine spend ring
// 3. It is false, and flipping it is what the trust ladder's ruling will
// authorize — one line, because the road it opens landed with its cap
// and its pacing already in place.
const machinePublishEnabled = false

// MachinePublishNoVerifierError is `--to-pr` declared unattended on a
// machine that cannot verify at all.
//
// The flag means two different things depending on whether a verifier
// exists, and this is the arm where it means the impossible one. With a
// verifier, --to-pr binds the change to the reconciler's slot and the
// slot publishes it once it has a pass. Without one there will never be
// a pass, so nothing will ever publish it that way, and the only reading
// of --to-pr left is "publish it now, on the invoker's authority" — an
// immediate ring-3 spend, which is a person's act. An unattended run has
// no authority to lend it, and there is no second machine publish path
// for it to borrow.
//
// It is its own refusal rather than the build gate's because it would
// still be the answer on a build that granted the machine everything:
// this run cannot earn the evidence its own road requires.
type MachinePublishNoVerifierError struct{}

func (e *MachinePublishNoVerifierError) Error() string {
	return "--to-pr in auto mode needs a local verifier: with one the change is queued for the reconciler's own publish slot, " +
		"and without one the only publication left is an immediate one on a person's authority, which an unattended run does not have"
}

// DockhandExit: the refused band's machine gate — a person asking for
// the same thing would be allowed it.
func (e *MachinePublishNoVerifierError) DockhandExit() int { return exitcode.MachineGate }

// Code names the refusal for a machine.
func (e *MachinePublishNoVerifierError) Code() string { return "machine-publish-no-verifier" }

// forgeWriteVerbs are the gh invocations that change something somebody
// else can see.
//
// Listed as a table rather than reasoned about at the call: the guard
// below is a backstop for engine code that does not know the gate
// exists, so what it recognizes must be readable at a glance and
// extendable by anyone adding a gh call. `pr view` and `pr list` are
// deliberately absent — reading spends nothing.
var forgeWriteVerbs = [][]string{
	{"pr", "create"},
	{"pr", "edit"},
	{"pr", "comment"},
	{"pr", "merge"},
	{"pr", "close"},
	{"pr", "reopen"},
	{"pr", "ready"},
	{"pr", "review"},
	{"pr", "lock"},
	{"pr", "unlock"},
	{"issue", "create"},
	{"issue", "comment"},
	{"issue", "close"},
	{"release", "create"},
	{"repo", "fork"},
	{"repo", "edit"},
}

// writeMethods are the HTTP methods that change something, for the raw
// `gh api` seam.
var writeMethods = map[string]bool{
	"POST": true, "PATCH": true, "PUT": true, "DELETE": true,
}

// readMethods are the two that do not. They are named rather than
// derived as "not a write", because the question the guard asks about
// `gh api` is whether the argv is PROVABLY a read — an unrecognized
// method word must fall through to the write reading, not out of it.
var readMethods = map[string]bool{"GET": true, "HEAD": true}

// bodyFlags are the ways a parameter is put on a `gh api` invocation.
// Their presence is what silently turns gh's default GET into a POST,
// which is the hole this list closes.
var bodyFlags = map[string]bool{
	"-f": true, "-F": true, "--field": true, "--raw-field": true, "--input": true,
}

// guardForgeWrites wraps the gh runner so that, on a build where a
// machine may not publish, a machine's write to the forge is refused
// before gh is executed.
//
// This is the layer that survives an author who never read the gate. The
// engine's own gate sits inside its push and its forge-write funnels,
// which dominates the tree as it stands and as it is likely to grow; the
// runner here sits UNDER all of it, so no argv any engine file can
// assemble reaches GitHub as a write while the invoker is a machine and
// the constant is false. It costs one string comparison per gh call.
//
// The invoker is read off the run at call time and not captured: it is
// resolved in PersistentPreRunE, after the composition root builds this
// runner, and a snapshot taken here would be the empty string forever.
func guardForgeWrites(rc *runstate.Context, inner gh.Runner) gh.Runner {
	return func(ctx context.Context, args ...string) (string, error) {
		if !isForgeWrite(args) {
			return inner(ctx, args...)
		}
		if err := engine.GateRing3(rc.Invoker, rc.MachinePublish); err != nil {
			return "", err
		}
		return inner(ctx, args...)
	}
}

// isForgeWrite reports whether this gh argv changes something on the
// forge: a write subcommand, or `gh api` with a method that writes.
func isForgeWrite(args []string) bool {
	if len(args) == 0 {
		return false
	}
	if args[0] == "api" {
		return apiWrites(args)
	}
	if len(args) < 2 {
		return false
	}
	for _, verb := range forgeWriteVerbs {
		if args[0] == verb[0] && args[1] == verb[1] {
			return true
		}
	}
	return false
}

// apiWrites reads whether a `gh api` invocation changes anything.
//
// THE METHOD IS NOT ALWAYS ON THE ARGV. gh defaults to GET and takes an
// explicit method as `-X POST`, `--method POST` or `--method=POST` —
// every spelling is read below — but it ALSO switches to POST on its own
// the moment any parameter is present. `gh api repos/o/r/pulls -f
// title=... -f head=... -f base=...` opens a pull request with no -X
// anywhere on it, and a guard reading only the explicit method let that
// through: a guard with a spelling for a hole is exactly what the
// comment above this function has always warned against, and this was
// one.
//
// So a body flag is read as a write unless the argv states a read method
// outright, which is the only shape that can prove otherwise: `-X GET -f
// q=...` is gh's own way to spell a parameterized read and stays one.
//
// graphql is a write whatever else the argv says. A query and a mutation
// are the same argv shape to this guard — both arrive as `-f query=...`
// against one endpoint — so there is nothing here that could tell them
// apart, and the reading that fails closed is the one this file takes
// everywhere else.
func apiWrites(args []string) bool {
	if len(args) >= 2 && args[1] == "graphql" {
		return true
	}
	body, method := false, ""
	for i, a := range args {
		switch {
		case (a == "-X" || a == "--method") && i+1 < len(args):
			method = strings.ToUpper(args[i+1])
		case strings.HasPrefix(a, "--method="):
			method = strings.ToUpper(strings.TrimPrefix(a, "--method="))
		case strings.HasPrefix(a, "-X") && len(a) > 2:
			method = strings.ToUpper(a[2:])
		case bodyFlags[a]:
			body = true
		case strings.HasPrefix(a, "--field="), strings.HasPrefix(a, "--raw-field="),
			strings.HasPrefix(a, "--input="),
			strings.HasPrefix(a, "-f") && len(a) > 2, strings.HasPrefix(a, "-F") && len(a) > 2:
			// The joined forms — `-fkey=value`, `--field=key=value` — which
			// gh accepts as readily as the separated ones.
			body = true
		}
	}
	if writeMethods[method] {
		return true
	}
	return body && !readMethods[method]
}
