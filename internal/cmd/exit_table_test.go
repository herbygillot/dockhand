package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/checksums"
	"github.com/herbygillot/dockhand/internal/distfile"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/intent/bumprevision"
	"github.com/herbygillot/dockhand/internal/lockfile"
	"github.com/herbygillot/dockhand/internal/macports/build"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/portfetch"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/macports/portstyle"
	"github.com/herbygillot/dockhand/internal/macports/prefix"
	"github.com/herbygillot/dockhand/internal/macports/session"
	"github.com/herbygillot/dockhand/internal/macports/shim"
	"github.com/herbygillot/dockhand/internal/macports/tree"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/runstate"
	"github.com/herbygillot/dockhand/internal/sweep"
	"github.com/herbygillot/dockhand/internal/tcl/rpc"
	"github.com/herbygillot/dockhand/internal/tcl/shell"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/text"
	"github.com/herbygillot/dockhand/internal/tool"
	"github.com/herbygillot/dockhand/internal/upstream"
	upstreamforge "github.com/herbygillot/dockhand/internal/upstream/forge"
	"github.com/herbygillot/dockhand/internal/vendored"
	"github.com/herbygillot/dockhand/internal/verdict"
	"github.com/herbygillot/dockhand/internal/verify"
)

// This file is the exit table, pinned. Every error the command tree
// can hand ExitCode is built here the way the code builds it — typed
// errors through their literals, sentinels through their package vars,
// wrapped where the producing site wraps — and asserted into the band
// it lands in today. The mapping lives in two places (a Coder method
// on each typed error, and ExitCode's sentinel switch), so the table
// is the one view that shows both at once; a renumbering or a
// reclassification changes this file, and nothing else, in one diff.

// The bands are a contract for scripts branching on $? once dockhand
// ships. The numbers are pinned here, not merely the names, so a
// renumbering surfaces as a single failure naming the band rather than
// as a scattering of subtest failures whose cause is a package away.
// exitcode's names are the only ones: cmd has no table of its own that
// could drift from them.
func TestExitBandsAreTodaysValues(t *testing.T) {
	assert.Equal(t, 0, exitcode.OK)
	assert.Equal(t, 1, exitcode.Failure)
	assert.Equal(t, 2, exitcode.Usage)

	assert.Equal(t, 10, exitcode.PlanDeclined)
	assert.Equal(t, 11, exitcode.BranchInFlight)
	assert.Equal(t, 12, exitcode.AlreadyCurrent)
	assert.Equal(t, 13, exitcode.Ambiguous)

	assert.Equal(t, 20, exitcode.DuplicatePR)
	assert.Equal(t, 21, exitcode.PRMerged)
	assert.Equal(t, 22, exitcode.Superseded)
	assert.Equal(t, 23, exitcode.Held)
	assert.Equal(t, 24, exitcode.MachineGate)

	assert.Equal(t, 30, exitcode.NoMacPorts)
	assert.Equal(t, 31, exitcode.EvalStartup)
	assert.Equal(t, 32, exitcode.RootRefused)
	assert.Equal(t, 33, exitcode.ToolMissing)
	assert.Equal(t, 34, exitcode.NoVerifyEnv)
	assert.Equal(t, 35, exitcode.ProvisionFailed)
	assert.Equal(t, 36, exitcode.VerifierBusy)

	assert.Equal(t, 40, exitcode.NotPortsTree)
	assert.Equal(t, 41, exitcode.PortNotFound)
	assert.Equal(t, 42, exitcode.NotARepo)
	assert.Equal(t, 43, exitcode.Drift)
	assert.Equal(t, 44, exitcode.BranchNotFound)

	assert.Equal(t, 50, exitcode.FetchFailed)
	assert.Equal(t, 51, exitcode.WitnessUnreachable)
	assert.Equal(t, 52, exitcode.WitnessAPI)
	assert.Equal(t, 53, exitcode.LatestUnresolved)

	assert.Equal(t, 60, exitcode.VerifyQueued)
	assert.Equal(t, 61, exitcode.VerifyAwaitingSlot)
	assert.Equal(t, 62, exitcode.PromotionPending)

	assert.Equal(t, 70, exitcode.VerifyFailed)
	assert.Equal(t, 71, exitcode.VerifyBlocked)
	assert.Equal(t, 72, exitcode.VerifyUnsupported)
	assert.Equal(t, 73, exitcode.VerifyErrored)

	assert.Equal(t, 80, exitcode.MintedSubmitErrored)
	assert.Equal(t, 81, exitcode.PushedPRFailed)
	assert.Equal(t, 82, exitcode.PRRefreshFailed)
	assert.Equal(t, 83, exitcode.SweepHardErrors)
}

// The decade is the family, which is what lets a script write `case
// $?/10` once and keep working when a code it has never heard of is
// added beside the ones it knows. This holds that property over the
// whole contract: every code the table can produce classifies, and it
// classifies by its decade rather than by a list somebody has to
// maintain.
//
// The three below ten are the exception the numbering was built
// around: they predate the bands and keep the shell's own meanings, so
// they are named individually and a script written before dockhand had
// families still reads them right.
func TestExitFamiliesAreTheDecade(t *testing.T) {
	assert.Equal(t, "success", exitcode.Family(exitcode.OK))
	assert.Equal(t, "failure", exitcode.Family(exitcode.Failure))
	assert.Equal(t, "usage", exitcode.Family(exitcode.Usage))

	bands := map[string]int{
		"declined":    10,
		"refused":     20,
		"environment": 30,
		"tree":        40,
		"upstream":    50,
		"pending":     60,
		"verdict":     70,
		"partial":     80,
	}
	for family, base := range bands {
		for code := base; code < base+10; code++ {
			assert.Equal(t, family, exitcode.Family(code), "code %d", code)
		}
	}

	// Outside the contract there is no family, said as nothing rather
	// than guessed at from the nearest band: a caller that reads a code
	// dockhand did not write should learn that, not be told a story.
	for _, code := range []int{3, 4, 5, 6, 7, 8, 9, 90, 99, 127, -1} {
		assert.Empty(t, exitcode.Family(code), "code %d", code)
	}

	// And the property that matters: every row's band classifies, and
	// into the family its decade names. A code added to a band later is
	// already classified, which is the whole point of the decade.
	for _, row := range exitTable(t) {
		got := ExitCode(row.err)
		assert.NotEmpty(t, exitcode.Family(got), "%s: code %d has no family", row.name, got)
		if got > exitcode.Usage {
			assert.Equal(t, exitcode.Family(got/10*10), exitcode.Family(got),
				"%s: code %d does not read as its own decade", row.name, got)
		}
	}
}

// The twin a document carries is the status the process leaves behind.
// They are two publications of one fact and the only way they can
// disagree is if something derives one of them twice, so this holds
// them to the same classifier over the whole table.
func TestExitTwinAgreesWithTheExitCode(t *testing.T) {
	for _, row := range exitTable(t) {
		twin := TwinOf(row.err)
		assert.Equal(t, ExitCode(row.err), twin.Code, "%s: twin disagrees with the exit code", row.name)
		assert.Equal(t, exitcode.Family(twin.Code), twin.Family, "%s: twin's family is not its code's", row.name)
	}
	assert.Equal(t, exitcode.Twin{Code: 0, Family: "success"}, TwinOf(nil))
}

// exitRow is one line of the table: an error built as the code builds
// it, and the band it lands in.
type exitRow struct {
	name string
	err  error
	want int
	// is pins that the constructed error still carries each sentinel
	// through whatever wrapping the producing site adds — the
	// sentinel-error standard's half of the contract. Without it a band
	// assertion could pass for the wrong reason: a wrap that dropped %w
	// would still exit 1, and 1 is the default.
	is []error
	// as is a pointer to a typed-error pointer, new(*T): the error
	// must still unwrap to the type that owns its band.
	as any
}

// exitTable builds every row. Sites are named by function so the table
// reads as a map of where each exit comes from.
//
// It takes the test because some rows are produced by asking the code
// that owns them, and a producer that unexpectedly succeeded must fail
// loudly rather than contribute a nil error to a table about errors.
func exitTable(t *testing.T) []exitRow {
	t.Helper()
	ctx := context.Background()
	const branch = "dockhand/jq-1.8"
	const sha = "0123456789abcdef0123456789abcdef01234567"
	// The moment a hold went on, pinned so the rows read the same twice.
	held := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	const tip = "0123456789ab"
	testos := []platform.Release{{Name: "Testos", Darwin: 99}}

	// Errors produced by the code paths themselves, wherever the
	// producer runs without a machine behind it.
	_, unknownRelease := platform.Parse("cheetah")
	_, noBases := resolveReleaseSet(nil, nil, true)
	_, noBaseFor := resolveReleaseSet([]string{"sequoia"}, testos, true)
	_, badOn := resolveReleaseSet([]string{"cheetah"}, testos, true)
	_, noVerifier := (&runstate.Context{}).VerifyProvider(ctx)
	_, noGh := (&runstate.Context{}).RunGH(ctx, "api", "user")
	_, writeFailed := fmt.Fprint(failWriter{}, "capabilities:\n")
	tooManyArgs := exactArgs(1)(&cobra.Command{Use: "bump"}, []string{"a", "b"})
	extraArg := noArgs(&cobra.Command{Use: "doctor"}, []string{"extra"})
	var notJSON map[string]any
	parseErr := json.Unmarshal([]byte("{not json"), &notJSON)

	capacity := &verify.CapacityError{Busy: 2, Cap: 2}
	// The same refusal with somebody standing there: the gate stamps
	// Synchronous, because nothing is queued for an ask that leaves.
	waiting := &verify.CapacityError{Busy: 2, Cap: 2, Synchronous: true}
	// submit's queue: the branch stands, the
	// cause rides along typed so status's pump can tell a full machine
	// from a missing capability.
	later := func(cause error) error {
		return &engine.VerifyDeferredError{Branch: branch, Reason: cause.Error(), Cause: cause}
	}
	noEnv := func(format string, a ...any) error {
		return fmt.Errorf("%w: "+format, append([]any{verify.ErrNoEnvironment}, a...)...)
	}
	// The other half of the provider split: no tart at all, whose
	// remedy is installing one rather than provisioning a base. Built
	// through verify.NoProvider, the way the composition root builds it,
	// so the row carries the bytes a user is shown rather than a second
	// spelling of them.
	noProvider := verify.NoProvider
	// The note refusals are asked of the codec rather than restated as
	// literals: the sentence is record's to word, the identity is what a
	// caller branches on, and a row that spelled either by hand would
	// agree with nothing. Each argument is the bytes that refusal
	// actually meets on disk.
	refuseNote := func(body string) error {
		_, err := record.Decode([]byte(body), sha)
		require.Error(t, err, "these bytes must be refused: %s", body)
		return err
	}
	noteErr := refuseNote("{not json")
	// What the finder hands back for a tool it did not resolve: the
	// sentinel mid-sentence, which is the shape every gh call wraps.
	ghMissing := fmt.Errorf("%s %w on PATH", tool.Gh, tool.ErrNotFound)

	var rows []exitRow
	add := func(want int, r ...exitRow) {
		for i := range r {
			r[i].want = want
		}
		rows = append(rows, r...)
	}

	// Band 0. --help, version, and a parent command with no RunE all
	// arrive here as a nil error.
	add(exitcode.OK, exitRow{name: "nil", err: nil})

	// Band 2: the invocation. Every site wraps into *UsageError; the
	// only exit in this band that never becomes an error value is the
	// unknown-subcommand pre-flight, pinned by its own test below.
	add(exitcode.Usage,
		exitRow{name: "*cmd.UsageError (usagef)", err: usagef("bad invocation"), as: new(*UsageError)},
		exitRow{name: "*cmd.UsageError wrapped", err: fmt.Errorf("outer: %w", usagef("inner")), as: new(*UsageError)},
		exitRow{name: "*cmd.UsageError over a cobra flag error (FlagErrorFunc)",
			err: &UsageError{Err: errors.New("unknown flag: --no-such-flag")}, as: new(*UsageError)},
		exitRow{name: "*cmd.UsageError over cobra.ExactArgs (exactArgs)", err: tooManyArgs, as: new(*UsageError)},
		exitRow{name: "*cmd.UsageError over cobra.NoArgs (noArgs)", err: extraArg, as: new(*UsageError)},
		exitRow{name: "platform.ErrUnknownRelease in *cmd.UsageError (resolveReleaseSet --on)",
			err: badOn, is: []error{platform.ErrUnknownRelease}, as: new(*UsageError)},
		exitRow{name: "platform.ErrUnknownRelease in *cmd.UsageError (provision --macos)",
			err: &UsageError{Err: unknownRelease}, is: []error{platform.ErrUnknownRelease}, as: new(*UsageError)},
		// bumprevision.ErrNoReason exists, but the command pre-empts it
		// with a usage error of its own before the planner runs.
		exitRow{name: "bump-revision without --reason (usagef pre-empts bumprevision.ErrNoReason)",
			err: usagef("a revision bump needs --reason: it says why users must rebuild"), as: new(*UsageError)},
		exitRow{name: "log/shell: several environments and no --on (usagef)",
			err: usagef("%s has environments on %s; pick one with --on", branch, "Sequoia, Sonoma"), as: new(*UsageError)},
		exitRow{name: "verify: --trace across several releases (usagef)",
			err: usagef("--trace follows one build; name one release with --on"), as: new(*UsageError)},
		// ExitCode consults the Coder before the sentinel table, so a
		// typed error wrapping a tree sentinel keeps the typed band.
		exitRow{name: "precedence: *cmd.UsageError wrapping tree.ErrNotPortsTree",
			err: &UsageError{Err: fmt.Errorf("a ports tree is needed: %w", tree.ErrNotPortsTree)},
			is:  []error{tree.ErrNotPortsTree}, as: new(*UsageError)},
	)

	// Band 30-36: the machine. Every code here has an installation or a
	// provisioning remedy, which is what separates them from the tree
	// band below — and what makes them worth telling apart at all: a
	// script that reads 3x knows the answer is "fix this machine", and
	// the code says which part of it.
	add(exitcode.NoMacPorts,
		exitRow{name: "prefix.ErrNotInstalled (prefix.Find)",
			err: fmt.Errorf("%w (no port-tclsh on PATH or under /opt/local)", prefix.ErrNotInstalled),
			is:  []error{prefix.ErrNotInstalled}},
	)
	add(exitcode.EvalStartup,
		exitRow{name: "eval.ErrStartup over shim.ErrNoShims (session.Start)",
			err: fmt.Errorf("%w: %w", eval.ErrStartup, shim.ErrNoShims), is: []error{eval.ErrStartup, shim.ErrNoShims}},
		exitRow{name: "eval.ErrStartup (session.Start: shim initialization)",
			err: fmt.Errorf("%w: initializing shim: %w", eval.ErrStartup, errors.New("broken pipe")),
			is:  []error{eval.ErrStartup}},
		exitRow{name: "session.ErrStartup over shell.Start (session.Start)",
			err: fmt.Errorf("%w: %w", session.ErrStartup, errors.New("fork/exec /nowhere/bin/port-tclsh: no such file or directory")),
			is:  []error{session.ErrStartup, eval.ErrStartup}},
		exitRow{name: "session.ErrStartup (portfetch.New over session.Start: shim initialization)",
			err: fmt.Errorf("%w: initializing shim: %w", session.ErrStartup, errors.New("broken pipe")),
			is:  []error{session.ErrStartup}},
		// The witness wrapper must not take an evaluator that will not
		// start: a Coder outranks a sentinel, so wrapping this would
		// relabel a broken tclsh "upstream unreachable" and send the
		// user off to look at a website. Unreachable returns it bare,
		// and this row is what holds it to that.
		exitRow{name: "precedence: upstream.Unreachable declines to band eval.ErrStartup",
			err: upstream.Unreachable("livecheck", fmt.Errorf("%w: %w", eval.ErrStartup, errors.New("broken pipe"))),
			is:  []error{eval.ErrStartup}},
	)
	add(exitcode.RootRefused,
		exitRow{name: "eval.ErrRootRefused", err: eval.ErrRootRefused, is: []error{eval.ErrRootRefused}},
		exitRow{name: "portfetch.ErrRootRefused", err: portfetch.ErrRootRefused, is: []error{portfetch.ErrRootRefused}},
		// The session owns the bootstrap eval and portfetch share, and
		// with it the sentinels; theirs alias it, which the is lists
		// prove — a re-declared sentinel would keep its own band and
		// silently drop the other package's.
		exitRow{name: "session.ErrRootRefused (session.Start; eval and portfetch alias it)",
			err: session.ErrRootRefused, is: []error{session.ErrRootRefused, eval.ErrRootRefused, portfetch.ErrRootRefused}},
	)
	// ErrNoProvider reaches here only from the verbs that were ASKED to
	// verify. The implicit submit inside a write intent intercepts the
	// same sentinel, says the branch is unverified and exits zero — the
	// contract narrowing rather than failing — which the intent test
	// below holds in place.
	add(exitcode.ToolMissing,
		exitRow{name: "verify.ErrNoProvider", err: verify.ErrNoProvider, is: []error{verify.ErrNoProvider}},
		exitRow{name: "verify.ErrNoProvider (realVMProvider: tart missing)",
			err: noProvider("tart is not installed (`port install tart`); --no-verify skips verification"),
			is:  []error{verify.ErrNoProvider}},
	)
	// Every one of these is raised with somebody standing there, before
	// anything is minted: nothing was queued and nobody will come back
	// for it. The asynchronous mirror — a submit that defers instead —
	// is VerifyAwaitingSlot, in the pending band.
	add(exitcode.NoVerifyEnv,
		exitRow{name: "verify.ErrNoEnvironment", err: verify.ErrNoEnvironment, is: []error{verify.ErrNoEnvironment}},
		exitRow{name: "verify.ErrNoEnvironment (realVMProvider: no bases)",
			err: noEnv("no base images; run `dockhand provision tart --macos <release>` first"),
			is:  []error{verify.ErrNoEnvironment}},
		exitRow{name: "verify.ErrNoEnvironment (resolveReleaseSet: no base images)",
			err: noBases, is: []error{verify.ErrNoEnvironment}},
		exitRow{name: "verify.ErrNoEnvironment (resolveReleaseSet: --on release without a base)",
			err: noBaseFor, is: []error{verify.ErrNoEnvironment}},
		exitRow{name: "verify.ErrNoEnvironment (provision: base missing after provisioning)",
			err: noEnv("base image %s is not present after provisioning", "dockhand-base-sequoia"),
			is:  []error{verify.ErrNoEnvironment}},
		// The tart tree wraps the child's error with %w in six places,
		// and an *exec.ExitError answers ExitCode. The sentinel is what
		// classifies this, and it stays that way because Coder asks for
		// DockhandExit — a name os/exec cannot answer by accident. The
		// row that proves it against a real child is below.
		exitRow{name: "verify.ErrNoEnvironment over an *exec.ExitError (tart stage: preparing the overlay)",
			err: fmt.Errorf("%w: preparing the overlay: %w", verify.ErrNoEnvironment, childExit(66)),
			is:  []error{verify.ErrNoEnvironment}},
	)
	add(exitcode.ProvisionFailed,
		exitRow{name: "*cmd.provisionFailedError (provisionAll)",
			err: &provisionFailedError{Releases: []string{"Sequoia", "Sonoma"}}, as: new(*provisionFailedError)},
	)
	// The full machine met by someone waiting. Same fact as
	// VerifyQueued, different answer, and the difference is only that
	// nothing was queued — which is why the flag is stamped by the
	// caller standing there and never by the provider counting slots.
	// Four callers stamp it: the gate and `verify <portdir>` through
	// RunVerification, and `exec` and `provision` through
	// waitingRefusal, whose own test is below.
	add(exitcode.VerifierBusy,
		exitRow{name: "*verify.CapacityError synchronous (the --verify gate, verify <portdir>, exec, provision)",
			err: waiting, as: new(*verify.CapacityError)},
	)

	// Band 40-44: the tree. Dockhand was pointed at the wrong place;
	// the remedy is a different path, branch or flag, never an install.
	add(exitcode.NotPortsTree,
		exitRow{name: "tree.ErrNotPortsTree", err: tree.ErrNotPortsTree, is: []error{tree.ErrNotPortsTree}},
		exitRow{name: "tree.ErrNotPortsTree wrapped (tree.Open)",
			err: fmt.Errorf("%s: %w", "/nowhere", tree.ErrNotPortsTree), is: []error{tree.ErrNotPortsTree}},
	)
	add(exitcode.PortNotFound,
		exitRow{name: "tree.ErrPortNotFound (tree.Resolve)",
			err: fmt.Errorf("%q: %w", "someport", tree.ErrPortNotFound), is: []error{tree.ErrPortNotFound}},
		// tree.Resolve names the missing PortIndex in prose; the
		// portindex sentinel itself is not wrapped in, so the band is
		// the tree's and portindex.ErrNoIndex stays a plain failure
		// wherever it escapes raw (see band 1).
		exitRow{name: "tree.ErrPortNotFound (tree.Resolve: no PortIndex)",
			err: fmt.Errorf("%q: %w (the tree has no PortIndex; run portindex to enable name lookup)", "someport", tree.ErrPortNotFound),
			is:  []error{tree.ErrPortNotFound}},
	)
	add(exitcode.NotARepo,
		exitRow{name: "git.ErrNotARepo (git.Open)",
			err: fmt.Errorf("%w: %s", git.ErrNotARepo, "/nowhere"), is: []error{git.ErrNotARepo}},
		exitRow{name: "git.ErrNotARepo (planOnBase)",
			err: fmt.Errorf("%w — the branch workflow needs a git checkout; --in-place edits the tree directly",
				fmt.Errorf("%w: %s", git.ErrNotARepo, "/nowhere")),
			is: []error{git.ErrNotARepo}},
	)
	// Drift is the tree having moved out from under a plan: nothing
	// failed and nothing is missing, what was planned against is not
	// what is there. Its sibling ErrMismatch stays in the failure band
	// on purpose — a delta that differs from the prediction is the
	// operation going wrong, not the tree changing — and its rows are
	// below.
	add(exitcode.Drift,
		exitRow{name: "plan.ErrDrift (verifyPlan)",
			err: fmt.Errorf("%w: %s", plan.ErrDrift, "/tree/sysutils/jq"), is: []error{plan.ErrDrift}},
		exitRow{name: "plan.ErrDrift (planOnBase)",
			err: fmt.Errorf("%w: the Portfile on %s is not the one planned against — commit your work there first, or use --in-place", plan.ErrDrift, "main"),
			is:  []error{plan.ErrDrift}},
		exitRow{name: "plan.ErrDrift raw (Materialize)", err: plan.ErrDrift, is: []error{plan.ErrDrift}},
	)
	add(exitcode.BranchNotFound,
		exitRow{name: "*engine.BranchNotFoundError (ResolveBranch)",
			err: &engine.BranchNotFoundError{Target: "jq"}, as: new(*engine.BranchNotFoundError)},
	)

	// Band 50-53: somebody else's. Nothing local is wrong and the same
	// invocation may work in an hour.
	add(exitcode.FetchFailed,
		exitRow{name: "distfile.ErrUnavailable (Fetch: no url served the file)",
			err: distfile.ErrUnavailable, is: []error{distfile.ErrUnavailable}},
	)
	add(exitcode.WitnessUnreachable,
		exitRow{name: "*upstream.WitnessError over upstream.ErrNoGit (Tags)",
			err: &upstream.WitnessError{Witness: "git", Err: upstream.ErrNoGit},
			is:  []error{upstream.ErrNoGit}, as: new(*upstream.WitnessError)},
		// The ls-remote refusal is deliberately NOT wrapped: an
		// *exec.ExitError carries an ExitCode of its own, and %w here
		// would hand git's child status straight to dockhand's. The
		// wrapper supplies the band and the child's words stay text.
		exitRow{name: "*upstream.WitnessError over an ls-remote refusal (Tags)",
			err: &upstream.WitnessError{Witness: "ls-remote",
				Err: fmt.Errorf("upstream: ls-remote %s: %s", "https://example/x.git", "exit status 128")},
			as: new(*upstream.WitnessError)},
		exitRow{name: "*upstream.WitnessError over a livecheck that could not run (Check)",
			err: upstream.Unreachable("livecheck", fmt.Errorf("portfetch: livecheck of %s: %s", "sysutils/jq", "dial tcp: connection refused")),
			as:  new(*upstream.WitnessError)},
	)
	// The forge answered badly or not at all, on the road where an
	// unanswered question must stop the work: a person is warned and
	// carries on, and the unattended pass waits for the next one.
	add(exitcode.WitnessAPI,
		exitRow{name: "*engine.ForgeLookupError (the machine road's own-PR lookup)",
			err: &engine.ForgeLookupError{Branch: branch, What: "this branch's own pull request",
				Err: errors.New("HTTP 403: API rate limit exceeded")},
			as: new(*engine.ForgeLookupError)},
	)
	// The witnesses ran and left no version anyone may act on. It is
	// the same *plan.Decline a judgment produces — the words are the
	// planner's either way — banded apart by the verdict underneath,
	// which does not survive being formatted into Detail.
	add(exitcode.LatestUnresolved,
		exitRow{name: "*upstream.UnresolvedError over *plan.Decline LatestUnresolved (NoSignal)",
			err: upstream.Unresolved(upstream.NoSignal, &plan.Decline{Type: plan.LatestUnresolved,
				Detail: fmt.Sprintf("%s (%s)", upstream.NoSignal, "livecheck found nothing and the forge has no tags")}),
			as: new(*upstream.UnresolvedError)},
		exitRow{name: "*upstream.UnresolvedError (LivecheckRot)",
			err: upstream.Unresolved(upstream.LivecheckRot, &plan.Decline{Type: plan.LatestUnresolved,
				Detail: fmt.Sprintf("%s (%s)", upstream.LivecheckRot, "forge has 1.9")}),
			as: new(*upstream.UnresolvedError)},
		exitRow{name: "*upstream.UnresolvedError (LivecheckBehind)",
			err: upstream.Unresolved(upstream.LivecheckBehind, &plan.Decline{Type: plan.LatestUnresolved,
				Detail: fmt.Sprintf("%s (%s)", upstream.LivecheckBehind, "livecheck 1.7, newest stable-looking tag 1.9")}),
			as: new(*upstream.UnresolvedError)},
		exitRow{name: "*upstream.UnresolvedError (LivecheckAhead, uncorroborated)",
			err: upstream.Unresolved(upstream.LivecheckAhead, &plan.Decline{Type: plan.LatestUnresolved,
				Detail: fmt.Sprintf("%s (%s)", upstream.LivecheckAhead, "no forge tag matches either")}),
			as: new(*upstream.UnresolvedError)},
		// The forge ran, answered, and said nothing about the version
		// livecheck named: one witness spoke to the value, so the band
		// is upstream's. It used to wear the Agreement label and resolve.
		exitRow{name: "*upstream.UnresolvedError (LivecheckUncorroborated)",
			err: upstream.Unresolved(upstream.LivecheckUncorroborated, &plan.Decline{Type: plan.LatestUnresolved,
				Detail: fmt.Sprintf("%s (%s)", upstream.LivecheckUncorroborated,
					"livecheck 1.2.0 stands alone: the forge's tags are all prerelease-style (newest 2.0.0-beta) and none of them is that version")}),
			as: new(*upstream.UnresolvedError)},
	)

	// Band 60-62: nobody's problem yet. Nothing failed and nothing
	// finished; the remedy is to ask again later, which is why these
	// can never share a band with a refusal.
	add(exitcode.VerifyQueued,
		exitRow{name: "*verify.CapacityError (tart.Admit)", err: capacity, as: new(*verify.CapacityError)},
		exitRow{name: "*engine.VerifyDeferredError over *verify.CapacityError (submit)",
			err: later(capacity), as: new(*verify.CapacityError)},
		// The summary carries no cause: every release it counts was
		// recorded deferred, which is what queued means.
		exitRow{name: "*engine.VerifyDeferredError (verifyBranch summary, no cause)",
			err: &engine.VerifyDeferredError{Branch: branch,
				Reason: fmt.Sprintf("%d release(s) deferred — each line above names its remedy; `dockhand cycle` retries them as remedies are met", 1)},
			as: new(*engine.VerifyDeferredError)},
		// A followed run found deferred is queued work, not a verdict.
		// Reachable only through a racing writer — what the follow
		// watched was Running — and it is banded here because lumping
		// queued work in with "the environment could not answer" is the
		// confusion the pending band exists to end.
		exitRow{name: "*verdict.QueuedError (Follow: the settle found the run deferred)",
			err: &verdict.QueuedError{Port: "jq", Platform: "Sequoia", Detail: "all 2 verification slots are busy"},
			as:  new(*verdict.QueuedError)},
	)
	// A deferral over a missing environment is queued, not refused: the
	// run is on the note and `cycle` starts it once a base exists. The
	// synchronous ask with the same obstacle is NoVerifyEnv above, and
	// the pair reads exactly like VerifyQueued and VerifierBusy do.
	// The publish slot's two ways of not being finished: a change whose
	// verification is still going, and a pass that has spent its own cap
	// or is holding itself to its pacing. Both mean ask again later,
	// which is the whole of what this band says.
	add(exitcode.PromotionPending,
		exitRow{name: "*verdict.PromotionPendingError (the machine road meets a run still going)",
			err: &verdict.PromotionPendingError{Branch: branch, Platforms: []string{"Sequoia"}},
			as:  new(*verdict.PromotionPendingError)},
		exitRow{name: "*engine.PassLimitError (the per-pass PR cap)",
			err: &engine.PassLimitError{Branch: branch,
				Why: "this pass has already opened its 1 pull request(s)"},
			as: new(*engine.PassLimitError)},
	)
	add(exitcode.VerifyAwaitingSlot,
		exitRow{name: "*engine.VerifyDeferredError over verify.ErrNoEnvironment (submit)",
			err: later(noEnv("no base images; run `dockhand provision tart --macos <release>` first")),
			is:  []error{verify.ErrNoEnvironment}, as: new(*engine.VerifyDeferredError)},
	)

	// Band 10-13: the PLAN's problem. dockhand understood the request,
	// could have carried it out, and judged that it should not. The
	// sweep test below insists each DeclineType has a row.
	add(exitcode.PlanDeclined,
		exitRow{name: "*plan.Decline AlreadyCurrent (bump.Plan: --to is the current version)",
			err: &plan.Decline{Type: plan.AlreadyCurrent, Detail: "jq is already at 1.8"}, as: new(*plan.Decline)},
		exitRow{name: "*plan.Decline AlreadyCurrent without detail (refresh: sums already match)",
			err: &plan.Decline{Type: plan.AlreadyCurrent}, as: new(*plan.Decline)},
		exitRow{name: "*plan.Decline TransformedStyle (bumprevision.Plan)",
			err: &plan.Decline{Type: plan.TransformedStyle, Detail: "perl5 writes its version transformed"}, as: new(*plan.Decline)},
		exitRow{name: "*plan.Decline FetchNotDriven (bump.Plan)",
			err: &plan.Decline{Type: plan.FetchNotDriven, Detail: "the fetch is pinned to a ref"}, as: new(*plan.Decline)},
		// bump's checksum edits fold checksums.ErrUnresolved into the
		// decline as text: the sentinel identity ends here, and the
		// decline's band is what the user sees.
		exitRow{name: "*plan.Decline ChecksumsNotLocated over checksums.ErrUnresolved (bump checksumEdits)",
			err: &plan.Decline{Type: plan.ChecksumsNotLocated, Detail: checksums.ErrUnresolved.Error()}, as: new(*plan.Decline)},
		exitRow{name: "*plan.Decline ChecksumsNotLocated (bump checksumEdits: value not literal)",
			err: &plan.Decline{Type: plan.ChecksumsNotLocated,
				Detail: fmt.Sprintf("recorded value %q not found as a literal (%s)", "deadbeef", "sha256")}, as: new(*plan.Decline)},
		exitRow{name: "*plan.Decline SubportsChanged (bump.Plan)",
			err: &plan.Decline{Type: plan.SubportsChanged, Detail: "demo2 would disappear"}, as: new(*plan.Decline)},
		exitRow{name: "*plan.Decline TargetNotReached (bump.Plan)",
			err: &plan.Decline{Type: plan.TargetNotReached, Detail: "version stayed at 1.7"}, as: new(*plan.Decline)},
		exitRow{name: "*plan.Decline UnexpectedChange (bump.Plan)",
			err: &plan.Decline{Type: plan.UnexpectedChange, Detail: "revision moved with the version"}, as: new(*plan.Decline)},
		// PrereleaseNewest is the one unresolved verdict that stays here:
		// the witnesses spoke and what they offered is a beta, so the
		// refusal is dockhand's own. Every other unresolved verdict is
		// upstream's and is banded 53 above. The livecheck-disabled port
		// whose forge has nothing but prerelease tags lands on this row
		// too: its one witness answered soundly, and declining what it
		// answered is the same judgment.
		exitRow{name: "*plan.Decline LatestUnresolved (bump.ResolveLatest: PrereleaseNewest is a judgment)",
			err: upstream.Unresolved(upstream.PrereleaseNewest, &plan.Decline{Type: plan.LatestUnresolved,
				Detail: fmt.Sprintf("%s (%s)", upstream.PrereleaseNewest, "newest tag 2.0-beta1 is prerelease-style")}),
			as: new(*plan.Decline)},
		// `unhold` on a branch nothing is holding: the verb was asked to
		// release something and there was nothing to release. Declined
		// rather than refused — nothing is wrong and nothing was written —
		// and typed rather than silent, so a script cannot read "the hold
		// is lifted" out of a hold that was never there.
		exitRow{name: "*engine.NotHeldError (unhold on an unheld branch)",
			err: &engine.NotHeldError{Branch: branch}, as: new(*engine.NotHeldError)},
		exitRow{name: "*plan.Decline VendoredBlock (refresh: go.vendors)",
			err: &plan.Decline{Type: plan.VendoredBlock, Detail: "go.vendors"}, as: new(*plan.Decline)},
		// The insertion's refusal, and it belongs in this band rather
		// than with the location declines below it: the field WAS
		// located — there is no revision line, which is a complete
		// answer — and what dockhand declined is writing one where the
		// file does not say it goes. That is a judgment about a plan.
		exitRow{name: "*plan.Decline RevisionShapeAmbiguous (bumprevision: no revision line, subports)",
			err: &plan.Decline{Type: plan.RevisionShapeAmbiguous,
				Detail: "the Portfile defines 3 evaluation contexts, and one inserted revision line would move all of them"},
			as: new(*plan.Decline)},
		// A patch the bump could not carry across: the plan was a version
		// away from complete, and what stopped it is a judgment about the
		// one move dockhand makes for a hunk — verbatim, once, elsewhere —
		// not a failure of anything. The remedy is a person's edit of the
		// patch, which is why it is a decline and not a tree error.
		exitRow{name: "*plan.Decline PatchWontRelocate (bump: a hunk's lines are not in the new source)",
			err: &plan.Decline{Type: plan.PatchWontRelocate,
				Detail:     "patch-no-fink.diff: configure.ac hunk 2: its lines do not occur in the new source",
				Determined: plan.ByNetwork},
			as: new(*plan.Decline)},
		exitRow{name: "*plan.Decline wrapped",
			err: fmt.Errorf("bump: %w", &plan.Decline{Type: plan.AlreadyCurrent}), as: new(*plan.Decline)},
		exitRow{name: "*portstyle.Decline FieldUnsupported (portstyle.Locate)",
			err: &portstyle.Decline{Type: portstyle.FieldUnsupported, Field: info.FieldDescription}, as: new(*portstyle.Decline)},
		exitRow{name: "*portstyle.Decline UnknownStyle (portstyle.Locate)",
			err: &portstyle.Decline{Type: portstyle.UnknownStyle, Field: info.FieldVersion}, as: new(*portstyle.Decline)},
		exitRow{name: "*portstyle.Decline NotLiteral with candidates (portstyle.Locate)",
			err: &portstyle.Decline{Type: portstyle.NotLiteral, Field: info.FieldVersion,
				Candidates: []portstyle.Candidate{{Style: portstyle.SetVariable, Literal: false}}}, as: new(*portstyle.Decline)},
		// A Coder anywhere in the chain wins, even under a sentinel.
		exitRow{name: "precedence: tree.ErrNotPortsTree wrapping *plan.Decline",
			err: fmt.Errorf("%w: %w", tree.ErrNotPortsTree, &plan.Decline{Type: plan.AlreadyCurrent}),
			is:  []error{tree.ErrNotPortsTree}, as: new(*plan.Decline)},
	)

	// The decline that withheld something on the way past. Nothing
	// populates Withheld yet — the riders arrive with the sweep — and
	// the code lands now, with this row, so the contract cannot quietly
	// change when they do.
	add(exitcode.AlreadyCurrent,
		exitRow{name: "*plan.Decline AlreadyCurrent with withheld riders (a sweep's held-back changes)",
			err: &plan.Decline{Type: plan.AlreadyCurrent, Detail: "jq is already at 1.8",
				Withheld: []string{"revision reset"}}, as: new(*plan.Decline)},
	)
	add(exitcode.BranchInFlight,
		exitRow{name: "*engine.BranchInFlightError (mint translating git.ErrBranchExists)",
			err: &engine.BranchInFlightError{Branch: branch}, as: new(*engine.BranchInFlightError)},
		exitRow{name: "*engine.BranchInFlightError with Reason (replaceInFlight: --replace refused)",
			err: &engine.BranchInFlightError{Branch: branch, Reason: fmt.Sprintf(
				"%s carries %d commit(s) beyond the mint — --replace replaces only what dockhand placed; `dockhand discard %s` first if you mean to drop your own work",
				branch, 1, branch)}, as: new(*engine.BranchInFlightError)},
	)
	// Ambiguity is one code whether the target names several branches
	// or the branch names several contexts: the request did not say
	// enough, and naming the one settles it either way.
	add(exitcode.Ambiguous,
		exitRow{name: "*verdict.AmbiguousTargetError (ResolveBranch)",
			err: &verdict.AmbiguousTargetError{Target: "jq",
				Matches: []string{"dockhand/jq-1.8", "dockhand/jq-1.9"}}, as: new(*verdict.AmbiguousTargetError)},
		exitRow{name: "*engine.AmbiguousContextError (SubjectOf: the branch changes several contexts)",
			err: &engine.AmbiguousContextError{Contexts: []string{"demo", "demo2"}}, as: new(*engine.AmbiguousContextError)},
	)

	// Band 20-24: the DESTINATION's problem. The change is fine; the
	// place it would go will not take it, and the remedy is about the
	// branch or the pull request rather than the edit.
	add(exitcode.DuplicatePR,
		exitRow{name: "*verdict.DuplicatePRError (promote)",
			err: &verdict.DuplicatePRError{Title: "jq: update to 1.8", URL: "https://github.com/macports/macports-ports/pull/1"},
			as:  new(*verdict.DuplicatePRError)},
	)
	add(exitcode.PRMerged,
		exitRow{name: "*verdict.PRMergedError (promote: MergedDeadEnd)",
			err: &verdict.PRMergedError{Number: 7, Branch: branch, URL: "https://github.com/macports/macports-ports/pull/7"},
			as:  new(*verdict.PRMergedError)},
	)
	// Work a newer sibling replaced. The code was reserved for exactly
	// this and a followed run that the branch moved out from under is
	// the first thing to produce it: nothing failed, and the answer the
	// run was about to give is about bytes that are no longer the tip.
	add(exitcode.Superseded,
		exitRow{name: "*verdict.SupersededError (Follow: the branch moved while the build ran)",
			err: &verdict.SupersededError{Port: "jq", Platform: "Sequoia"},
			as:  new(*verdict.SupersededError)},
	)
	// The machine gate's other half, and the one that never reaches the
	// engine: promote publishes on a person's authority, so a run that
	// declared itself unattended is turned away at the verb rather than
	// gated at the push.
	// The machine gate proper: the build-time refusal every unattended
	// publication meets on this build, and the two judgments underneath
	// it. All three say the same thing about whose problem it is — the
	// change is fine, the road refused it, and a person asking for the
	// same thing would be allowed it.
	add(exitcode.MachineGate,
		exitRow{name: "*cmd.PromoteIsHumanError (promote in auto mode)",
			err: &PromoteIsHumanError{}, as: new(*PromoteIsHumanError)},
		exitRow{name: "*engine.MachineDisabledError (GateRing3: the build-time constant is false)",
			err: &engine.MachineDisabledError{}, as: new(*engine.MachineDisabledError)},
		exitRow{name: "*verdict.UnprovenError (the machine road's positive-evidence rule)",
			err: &verdict.UnprovenError{Branch: branch, Tip: "abc1234"},
			as:  new(*verdict.UnprovenError)},
	)
	// A branch held back. One code for every road, because a hold means
	// the same thing on all of them — the publication, the verification
	// the pump would start, the deletion a merged pull request would earn
	// — and a caller branching on $? wants one answer, not four.
	add(exitcode.Held,
		exitRow{name: "*engine.HeldError (the publish gate obeying a hold)",
			err: &engine.HeldError{Branch: branch, Withheld: "the publication",
				Hold: &record.Hold{Reason: "waiting on upstream", At: held}},
			as: new(*engine.HeldError)},
		exitRow{name: "*engine.HeldError (hold on an already-held branch)",
			err: &engine.HeldError{Branch: branch, Withheld: "a second hold",
				Hold: &record.Hold{At: held}}, as: new(*engine.HeldError)},
		exitRow{name: "*engine.StealthHeldError (the publish slot's re-witness)",
			err: &engine.StealthHeldError{Branch: branch, Mismatch: []engine.ChecksumMismatch{
				{File: "jq-1.8.tar.gz", Type: "sha256", Recorded: "aaa", Served: "bbb"}}},
			as: new(*engine.StealthHeldError)},
	)

	// Band 70-73: verification ANSWERED, and not with a pass. The band
	// is about what the run concluded, which is why it holds the port's
	// failure alongside the three ways a run ends concluding nothing —
	// the distinction the single old "environment" answer destroyed by
	// telling a user whose neighbour was broken to provision something.
	add(exitcode.VerifyFailed,
		exitRow{name: "*engine.VerifyFailedError (Follow --trace)",
			err: &engine.VerifyFailedError{Port: "jq"}, as: new(*engine.VerifyFailedError)},
		exitRow{name: "*engine.VerifyFailedError with environment kept (RunVerification)",
			err: &engine.VerifyFailedError{Port: "jq", Handle: "dockhand-jq-1"}, as: new(*engine.VerifyFailedError)},
		// promote refusing over a failed run exits with the run: the
		// refusal is that verdict being enforced, not a way promote can
		// break.
		exitRow{name: "*verdict.FailedVerificationError (promote's gate)",
			err: &verdict.FailedVerificationError{Branch: branch, Tip: tip}, as: new(*verdict.FailedVerificationError)},
	)
	add(exitcode.VerifyBlocked,
		exitRow{name: "*verdict.BlockedError (Follow: the settle recorded blocked)",
			err: &verdict.BlockedError{Port: "jq", Platform: "Sequoia", Detail: "oniguruma failed to build"},
			as:  new(*verdict.BlockedError)},
	)
	add(exitcode.VerifyUnsupported,
		exitRow{name: "*verdict.UnsupportedError (the provider cannot run the request)",
			err: &verdict.UnsupportedError{Port: "jq", Platform: "Sequoia", Detail: "no base for Sequoia"},
			as:  new(*verdict.UnsupportedError)},
		exitRow{name: "verify.ErrUnsupported", err: verify.ErrUnsupported, is: []error{verify.ErrUnsupported}},
		exitRow{name: "verify.ErrUnsupported (tart.Provider: unserved release)",
			err: fmt.Errorf("%w: no base for %s", verify.ErrUnsupported, "Sequoia"), is: []error{verify.ErrUnsupported}},
		// Nothing frees a capability refusal, so the deferral over one
		// is not pending: the provider has said it cannot run what was
		// asked for, and that is a verdict about the request.
		exitRow{name: "*engine.VerifyDeferredError over verify.ErrUnsupported (submit)",
			err: later(fmt.Errorf("%w: %s is not a <category>/<port> directory", verify.ErrUnsupported, "stage-jq")),
			is:  []error{verify.ErrUnsupported}, as: new(*engine.VerifyDeferredError)},
	)
	add(exitcode.VerifyErrored,
		exitRow{name: "*verdict.ErroredError (RunVerification: the Errored verdict)",
			err: &verdict.ErroredError{Port: "jq", Platform: "Sequoia", Detail: "the guest agent timed out"},
			as:  new(*verdict.ErroredError)},
		exitRow{name: "*verdict.ErroredError (Follow: a run in a state this build does not know)",
			err: &verdict.ErroredError{Port: "jq", Platform: "Sequoia", Detail: "reticulating"},
			as:  new(*verdict.ErroredError)},
		// A cancel shares the code and not the sentence. None of the
		// ruled numbers names a person stopping a run, so the band is
		// the one that says the verification ended without a verdict —
		// but "could not answer: canceled" contradicts itself, and the
		// reason is what tells the two apart.
		exitRow{name: "*verdict.CanceledError (Follow: `dockhand cancel` from another terminal)",
			err: &verdict.CanceledError{Port: "jq", Platform: "Sequoia", Detail: "canceled by the user"},
			as:  new(*verdict.CanceledError)},
	)

	// Band 80-83: the operation did HALF ITS WORK, and the half it did
	// stands. Re-running is neither free nor always safe, so a script
	// has to be able to tell these from "nothing happened".
	add(exitcode.MintedSubmitErrored,
		exitRow{name: "*engine.VerifyDeferredError over an untyped submit failure (submit)",
			err: later(errors.New("the agent never answered")), as: new(*engine.VerifyDeferredError)},
	)
	add(exitcode.PushedPRFailed,
		exitRow{name: "*engine.PushedPRError (promote: PR create failed after push)",
			err: &engine.PushedPRError{Branch: branch, Remote: "fork",
				Err: fmt.Errorf("gh %s: %s", "pr", "HTTP 422")}, as: new(*engine.PushedPRError)},
	)
	add(exitcode.PRRefreshFailed,
		exitRow{name: "*engine.PRRefreshError (promote: PR edit failed after push)",
			err: &engine.PRRefreshError{Branch: branch, Remote: "fork", Number: 7,
				Err: fmt.Errorf("gh %s: %s", "pr", "HTTP 422")}, as: new(*engine.PRRefreshError)},
	)
	// The two ways a selector-scale write verb ends badly, and both are
	// half-done work rather than nothing: the ports before the bad one
	// have branches. The first is the census's own exit — rows that
	// were not declines — and the second is the dispatch loop losing
	// its evaluators with targets still queued.
	add(exitcode.SweepHardErrors,
		exitRow{name: "*cmd.SweepFailedError (a sweep whose census counted hard rows)",
			err: &SweepFailedError{Hard: 2, Total: 40, First: "jq"}, as: new(*SweepFailedError)},
		exitRow{name: "*sweep.AbandonedError (the pool died with targets still queued)",
			err: &sweep.AbandonedError{Targets: []tree.Target{{Portdir: "/tree/sysutils/jq"}},
				Cause: errors.New("no evaluator would start")},
			is: []error{sweep.ErrAbandoned}, as: new(*sweep.AbandonedError)},
	)

	// Band 1: everything else. The untyped refusals are built here
	// exactly as their sites build them — fmt.Errorf with no sentinel
	// and no type — which is why they land in the default band. If one
	// of them ever earns a sentinel or a Coder, its row moves.
	add(exitcode.Failure,
		exitRow{name: "untyped errors.New", err: errors.New("boom")},
		// Typed, but with no Coder: these four carry a type for their
		// own callers' sake (the census counts a parse failure apart
		// from an unreadable file; a session survives a handler error)
		// and reach ExitCode unclassified. A Coder added to any of them
		// moves its row instead of silently moving $?.
		exitRow{name: "*port.ParseError (port.Source: Portfile does not parse; typed, no Coder)",
			err: &port.ParseError{Path: "/tree/sysutils/jq/Portfile", Detail: "3:1: unterminated brace"}, as: new(*port.ParseError)},
		exitRow{name: "text.EditError (text.Apply: refused edit list; typed, no Coder)",
			err: text.EditError{Type: text.Overlap, Edit: text.Edit{Span: text.Span{Start: 3, End: 5}, New: []byte("1.8")}}, as: new(text.EditError)},
		exitRow{name: "syntax.Error (syntax.Parse; typed, no Coder)",
			err: syntax.Error{Type: syntax.UntermBrace, Span: text.Span{Start: 0, End: 4}}, as: new(syntax.Error)},
		exitRow{name: "rpc.CallError (Session.Call: the handler errored; typed, no Coder)",
			err: rpc.CallError{Msg: `invalid command name "nope"`}, as: new(rpc.CallError)},
		exitRow{name: "rpc.CallError wrapped (eval: a snapshot's Tcl error)",
			err: fmt.Errorf("evaluating %s: %w", "sysutils/jq", rpc.CallError{Msg: "can't read \"x\": no such variable"}), as: new(rpc.CallError)},
		// ErrMismatch sits one line from ErrDrift in apply.go and reads
		// like its twin; it is not. Drift is the tree having moved, and
		// has the tree's band. A delta that differs from the prediction
		// is the operation going wrong, which is this one.
		exitRow{name: "plan.ErrMismatch (Apply)", err: plan.ErrMismatch, is: []error{plan.ErrMismatch}},
		exitRow{name: "plan.ErrMismatch (Apply: edited Portfile does not evaluate)",
			err: fmt.Errorf("%w: edited Portfile failed to evaluate: %w", plan.ErrMismatch, errors.New("invalid command name")),
			is:  []error{plan.ErrMismatch}},
		exitRow{name: "plan.ErrMismatch joined with a restore failure (Apply)",
			err: errors.Join(fmt.Errorf("%w (and restore failed)", plan.ErrMismatch), errors.New("write Portfile: permission denied")),
			is:  []error{plan.ErrMismatch}},
		// Raw git.ErrBranchExists never reaches ExitCode — MintFromPlan
		// translates it into the BranchInFlightError above — so this
		// row says what would happen if it escaped.
		exitRow{name: "git.ErrBranchExists raw (MintFromPlan translates it; raw would be 1)",
			err: fmt.Errorf("%w: %s", git.ErrBranchExists, branch), is: []error{git.ErrBranchExists}},
		exitRow{name: "git.ErrNoNote (NoteRead)",
			err: fmt.Errorf("%w: %s", git.ErrNoNote, sha), is: []error{git.ErrNoNote}},
		exitRow{name: "verify.ErrUnknownJob (Poll)",
			err: fmt.Errorf("%w: %s", verify.ErrUnknownJob, "fake-1"), is: []error{verify.ErrUnknownJob}},
		exitRow{name: "platform.ErrUnknownRelease raw (every cmd site wraps it in *UsageError)",
			err: unknownRelease, is: []error{platform.ErrUnknownRelease}},
		exitRow{name: "note validation: does not parse (record.Decode)",
			err: noteErr, is: []error{record.ErrMalformed}},
		exitRow{name: "note validation: newer schema (record.Decode)",
			err: refuseNote(fmt.Sprintf(`{"schema": 99, "sha": %q}`, sha)),
			is:  []error{record.ErrSchemaTooNew}},
		// Every note on disk before this build arrives here, which is
		// what the clean break costs and what it says out loud: the old
		// evidence cannot be carried over, so the refusal names both
		// halves of the remedy — discard the note, then re-earn it. It
		// is band 1 like its siblings: a note this build cannot read is
		// a thing gone wrong, not a destination refusing.
		exitRow{name: "note validation: schema 2, from before the break (record.Decode)",
			err: refuseNote(fmt.Sprintf(`{"schema": 2, "sha": %q, "port": "jq", "runs": {}}`, sha)),
			is:  []error{record.ErrSchemaTooOld}},
		exitRow{name: "note validation: sha mismatch (record.Decode)",
			err: refuseNote(`{"schema": 3, "sha": "ffff"}`),
			is:  []error{record.ErrShaMismatch}},
		exitRow{name: "submit-and-record compensation: release failed too (submit)",
			err: fmt.Errorf("recording the run failed (%w) and releasing %s failed too: %w — `dockhand cycle --reclaim-orphans` frees the slot",
				noteErr, "fake-1", errors.New("tart delete: no such vm"))},
		exitRow{name: "submit-and-record compensation: worker released (submit)",
			err: fmt.Errorf("recording the run failed; the worker was released: %w", noteErr)},
		// "one at a time for now" is gone: several portdirs are a cohort
		// now, and the two refusals that replace it are about a branch
		// that changes nothing and a branch whose record and whose diff
		// disagree. Both are band 1 — the same band the retired sentence
		// landed in — because neither is a destination refusing.
		exitRow{name: "verify: branch changes no portdir (ChangedPortdirs)",
			err: fmt.Errorf("verify: %s changes no portdir against %s; there is nothing to verify", branch, tip)},
		exitRow{name: "verify: the record and the diff disagree (ChangedPortdirs)",
			err: fmt.Errorf("verify: %s changes %s against %s, but its record names %s; the two disagree, so nothing is staged — `dockhand discard %s` and re-mint, or verify the portdirs by hand",
				branch, "sysutils/jq, textproc/oniguruma", tip, "sysutils/jq", branch)},
		// The `lifecycle:` prefix outlived the package that gave it: the
		// code moved to the engine and the sentence did not, because the
		// words are what a user reads and a move does not get to reword
		// them. Both rows carry the literal text for that reason — the
		// name says where the error is built, the literal says what is
		// printed, and only a deliberate change moves the second.
		exitRow{name: "engine.SubjectOf: evaluation failed",
			err: fmt.Errorf("lifecycle: evaluating %s at %s: %w", "sysutils/jq", tip, errors.New("Portfile: invalid command name"))},
		exitRow{name: "verify: job ended in state (RunVerification)",
			err: fmt.Errorf("verify: job ended in state %s", "running")},
		// Not the sweep's band: SweepHardErrors is a write verb over a
		// selector finishing with rows that were not declines, and it
		// has its own two rows above. exec is a probe, and a probe
		// reporting how many
		// releases ran its command and did not like the answer is an
		// ordinary failure. The one refusal that must NOT be counted
		// into this summary is a full machine — the command never ran
		// — and exec returns that one instead, banded 36 above.
		exitRow{name: "exec: the command failed on N of M releases",
			err: fmt.Errorf("exec: the command failed on %d of %d releases", 1, 2)},
		exitRow{name: "log/shell: environment no longer exists (debugTarget)",
			err: fmt.Errorf("environment %s no longer exists", "dockhand-jq-1")},
		exitRow{name: "log/shell: no verification record (debugTarget over git.ErrNoNote)",
			err: fmt.Errorf("%s has no verification record; `dockhand verify %s` starts one", branch, branch)},
		exitRow{name: "log/shell: no reachable environment on release (debugTarget)",
			err: fmt.Errorf("%s has no reachable environment on %s (%s)", branch, "Sequoia", "passed on Testos")},
		exitRow{name: "log/shell: no environment to reach (debugTarget)",
			err: fmt.Errorf("%s: no environment to reach (%s); `dockhand verify %s` starts one", branch, "unverified", branch)},
		exitRow{name: "log/shell: environment vanished (debugTarget over verify.ErrUnknownJob)",
			err: fmt.Errorf("%s: environment %s no longer exists", branch, "dockhand-jq-1")},
		exitRow{name: "log: reading the guest log failed",
			err: fmt.Errorf("reading %s from %s: %w", "main.log", "dockhand-jq-1", errors.New("ssh: connection reset"))},
		exitRow{name: "shell: provider takes no interactive shell",
			err: errors.New("this provider's environments do not take an interactive shell; `dockhand exec` runs a command in one")},
		exitRow{name: "runstate: no verify provider wired (VerifyProvider)", err: noVerifier},
		exitRow{name: "runstate: no gh runner wired (RunGH)", err: noGh},
		exitRow{name: "gh.UpstreamRepo: unreadable remote",
			err: fmt.Errorf("cannot read owner/repo from remote %q (%s)", "origin", "nonsense")},
		exitRow{name: "gh.RealGhOut: gh failed",
			err: fmt.Errorf("gh %s: %s", "api", "HTTP 401")},
		// Built through the sentinel, the way RealGhOut builds it: the
		// row used to be a bare errors.New with the same words, which
		// would have kept asserting this band after tool.ErrNotFound
		// moved and passed for the wrong reason. A row that does not
		// carry what the site carries proves nothing.
		exitRow{name: "gh.RealGhOut: gh missing (tool.ErrNotFound)",
			err: fmt.Errorf("%w (`port install gh`)", ghMissing), is: []error{tool.ErrNotFound}},
		exitRow{name: "gh.LookupPR: unreadable tracked remote",
			err: fmt.Errorf("cannot read an owner from remote %q", "herby")},
		exitRow{name: "gh.QueryPR: unreadable JSON",
			err: fmt.Errorf("reading PR lookup: %w", parseErr)},
		exitRow{name: "gh.OpenPortPRs: unreadable JSON",
			err: fmt.Errorf("reading open PRs: %w", parseErr)},
		exitRow{name: "gh.OpenPortPRs: the walk ran past its bound",
			err: fmt.Errorf("listing open PRs on %s: more than %d pages", "macports/macports-ports", 100)},
		exitRow{name: "gh.ForkRemote: no such remote",
			err: fmt.Errorf("no remote %q", "nope")},
		exitRow{name: "gh.ForkRemote: unreadable override remote",
			err: fmt.Errorf("remote %q: cannot read an owner from %s", "nope", "nonsense")},
		exitRow{name: "gh.ForkRemote: gh needed (tool.ErrNotFound)",
			err: fmt.Errorf("finding your fork needs gh: %w (or pass --remote)",
				fmt.Errorf("%w (`port install gh`)", ghMissing)), is: []error{tool.ErrNotFound}},
		exitRow{name: "gh.ForkRemote: several fork remotes",
			err: fmt.Errorf("%d remotes belong to %s; pass --remote", 2, "herbygillot")},
		exitRow{name: "git plumbing failure (execGit)",
			err: fmt.Errorf("git %s: %s", "push", "rejected: stale info")},
		exitRow{name: "git: no primary branch",
			err: errors.New("git: no primary branch: origin/HEAD unset, no main or master, HEAD detached")},
		exitRow{name: "git: path outside the repository (RelPath)",
			err: fmt.Errorf("git: %s is outside the repository at %s", "/elsewhere", "/repo")},
		exitRow{name: "lockfile: flock failed",
			err: fmt.Errorf("locking %s: %w", "/repo/.git/dockhand-notes.lock", syscall.EACCES), is: []error{syscall.EACCES}},
		exitRow{name: "lockfile.ErrHeld (Acquire: deadline passed)",
			err: fmt.Errorf("%w past its deadline: %s — check for a running or hung dockhand; a crashed one releases the lock by itself", lockfile.ErrHeld, "/repo/.git/dockhand-notes.lock"),
			is:  []error{lockfile.ErrHeld}},
		exitRow{name: "lockfile.ErrHeld (submitRelease: a peer holds the submit lock)",
			err: fmt.Errorf("%w: a verification is being submitted in this repository; `dockhand status` shows what it started, then `dockhand verify %s` again", lockfile.ErrHeld, branch),
			is:  []error{lockfile.ErrHeld}},
		exitRow{name: "context.Canceled (SIGINT)", err: context.Canceled, is: []error{context.Canceled}},
		exitRow{name: "context.DeadlineExceeded", err: context.DeadlineExceeded, is: []error{context.DeadlineExceeded}},
		exitRow{name: "output write failure (doctor: Fprint to a failing stream)", err: writeFailed},
	)

	// Sentinels no table entry names: they are branchable state for
	// their own callers and reach ExitCode only by escaping, where the
	// default band takes them. The ones that are re-wrapped on the way
	// up (checksums into plan.Decline, shim.ErrNoShims into
	// eval.ErrStartup, portindex.ErrNoIndex into tree.ErrPortNotFound)
	// have their wrapped rows in the bands above; raw, they are 1.
	//
	// The sweep is what proves a sentinel that gained a band elsewhere
	// did not silently gain one here — which is why every sentinel the
	// contract DOES name is out of it, in a row of its own:
	// distfile.ErrUnavailable, plan.ErrDrift, upstream.ErrNoGit and the
	// verify and tree families all sit above.
	for _, s := range []error{
		checksums.ErrUnresolved,
		checksums.ErrMalformed,
		distfile.ErrMemberMissing,
		distfile.ErrMemberAmbiguous,
		vendored.ErrNoBlock,
		vendored.ErrMultipleBlocks,
		vendored.ErrMalformed,
		vendored.ErrUnaccounted,
		vendored.ErrNoGenerator,
		vendored.ErrEmptyBlock,
		portindex.ErrMalformed,
		portindex.ErrNotIndexed,
		portindex.ErrNoIndex,
		build.ErrNotAPortdir,
		build.ErrNoTally,
		shell.ErrClaimed,
		rpc.ErrHandshake,
		rpc.ErrBroken,
		info.ErrMalformedSelection,
		upstreamforge.ErrUnbound,
		bumprevision.ErrNoReason,
		tree.ErrNoTreeAbove,
		tree.ErrNoPortdir,
		shim.ErrNoShims,
		lockfile.ErrHeld,
	} {
		add(exitcode.Failure, exitRow{name: "raw sentinel " + s.Error(), err: s, is: []error{s}})
	}
	return rows
}

// childExit runs a process that exits with the given status and returns
// the *exec.ExitError it produced. The real type, not a stand-in: what
// the rows using it are about is a structural accident of os/exec's own
// method set, which a fake cannot reproduce and cannot disprove.
func childExit(code int) error {
	return exec.Command("/bin/sh", "-c", fmt.Sprintf("exit %d", code)).Run()
}

// A child process's exit status is not a dockhand band, and cannot
// become one by being wrapped.
//
// *exec.ExitError answers ExitCode(), so an interface asking for that
// method is satisfied by every child process dockhand ever runs — and
// the typed half of the mapping is consulted FIRST, so a chain that
// wrapped a raw exec error would hand the child's status to $?, past
// the sentinel that knows better. The tart tree wraps exactly that way
// in six places. Coder therefore asks for DockhandExit, a name nothing
// outside this repository writes, and these are the numbers that used
// to come out: a guest exiting 66 made `case $?/10` conclude "nobody's
// problem yet, ask again later" about a verification environment that
// had failed.
func TestAChildsExitStatusCannotOccupyABand(t *testing.T) {
	for _, code := range []int{1, 3, 5, 66, 128} {
		t.Run(fmt.Sprintf("child %d", code), func(t *testing.T) {
			child := childExit(code)
			var ee *exec.ExitError
			require.ErrorAs(t, child, &ee)
			require.Equal(t, code, ee.ExitCode(), "the child really does carry this status")

			var coder exitcode.Coder
			assert.NotErrorAs(t, child, &coder, "and it is not a dockhand band")

			// Wrapped the way internal/verify/tart/tart.go writes it.
			err := fmt.Errorf("%w: preparing the overlay: %w", verify.ErrNoEnvironment, child)
			assert.Equal(t, exitcode.NoVerifyEnv, ExitCode(err),
				"the sentinel classifies it; the child's status is not consulted")
			assert.Equal(t, "environment", exitcode.Family(ExitCode(err)))
		})
	}
}

// Every ask that waits for its answer says so, so that a full machine
// answers the code for "nothing was queued" rather than the one for
// "come back later".
//
// Two callers stamp it in the engine, where RunVerification serves the
// --verify gate and `verify <portdir>`; the other two are exec and
// provision, which go through this helper. Before it, `exec` counted a
// refusal it never got to run as a failed command and exited 1, and
// `provision tart` told the user `dockhand status` would start work
// that nothing had recorded.
func TestWaitingRefusalStampsTheAsksThatLeave(t *testing.T) {
	full := &verify.CapacityError{Busy: 2, Cap: 2}
	require.Equal(t, exitcode.VerifyQueued, ExitCode(full), "unstamped, it is a deferred run")

	assert.True(t, waitingRefusal(full))
	assert.True(t, full.Synchronous)
	assert.Equal(t, exitcode.VerifierBusy, ExitCode(full), "somebody is standing there")
	assert.Equal(t, "verifier-busy", TwinOf(full).Reason)

	// Through the wrapping each site adds, because neither caller gets
	// the refusal bare.
	wrapped := fmt.Errorf("provisioning %s: %w", "Sequoia", &verify.CapacityError{Busy: 2, Cap: 2})
	assert.True(t, waitingRefusal(wrapped))
	assert.Equal(t, exitcode.VerifierBusy, ExitCode(wrapped))

	// And nothing else is touched: a probe whose command really did run
	// and fail is still an ordinary failure.
	assert.False(t, waitingRefusal(fmt.Errorf("exec: the command failed on %d of %d releases", 1, 2)))
	assert.False(t, waitingRefusal(nil))
}

// The twin's reason is a key: one reason, one code. Several reasons may
// share a code — two producers of the same outcome name themselves the
// same way — but a reason that spanned two codes would be the coarser
// of the two fields, which is backwards. The band says which KIND of
// problem this is; the reason says WHICH, and a consumer filtering on
// it must not have to read the code to learn what it filtered.
//
// Two collisions were found this way and both were fixed by naming:
// upstream's unresolved wrapper stopped borrowing the decline's
// "latest-unresolved" (the two are 53 and 10, and the reason was the
// only thing that could have told them apart), and a decline that
// withheld riders names itself apart from one that withheld nothing.
func TestEveryTwinReasonNamesOneCode(t *testing.T) {
	codes := map[string]map[int]string{}
	for _, row := range exitTable(t) {
		twin := TwinOf(row.err)
		if twin.Reason == "" {
			continue
		}
		if codes[twin.Reason] == nil {
			codes[twin.Reason] = map[int]string{}
		}
		codes[twin.Reason][twin.Code] = row.name
	}
	for reason, rows := range codes {
		assert.Len(t, rows, 1, "reason %q spans several codes: %v", reason, rows)
	}
}

// The sentinel half of the mapping names itself too. Without this the
// twin could say nothing at all for a third of the contract's codes —
// harmless while the only documents are the plan, the status report and
// the decline, and wrong the first time a verb publishes one on a
// machine that has no MacPorts.
func TestSentinelOutcomesCarryAReason(t *testing.T) {
	for _, tc := range []struct {
		err    error
		code   int
		reason string
	}{
		{prefix.ErrNotInstalled, exitcode.NoMacPorts, "no-macports"},
		{eval.ErrStartup, exitcode.EvalStartup, "eval-startup"},
		{eval.ErrRootRefused, exitcode.RootRefused, "root-refused"},
		{verify.ErrNoProvider, exitcode.ToolMissing, "no-verify-provider"},
		{verify.ErrNoEnvironment, exitcode.NoVerifyEnv, "no-verify-environment"},
		{tree.ErrNotPortsTree, exitcode.NotPortsTree, "not-ports-tree"},
		{tree.ErrPortNotFound, exitcode.PortNotFound, "port-not-found"},
		{git.ErrNotARepo, exitcode.NotARepo, "not-a-repo"},
		{plan.ErrDrift, exitcode.Drift, "drift"},
		{distfile.ErrUnavailable, exitcode.FetchFailed, "fetch-failed"},
		{verify.ErrUnsupported, exitcode.VerifyUnsupported, "verification-unsupported"},
	} {
		t.Run(tc.reason, func(t *testing.T) {
			// Wrapped, because no site returns a sentinel bare.
			err := fmt.Errorf("dockhand: %w: %s", tc.err, "context the site adds")
			assert.Equal(t, exitcode.Of(tc.code, tc.reason), TwinOf(err))
		})
	}

	// An error the mapping does not recognize carries no reason rather
	// than an invented one, and a typed error's own name wins over the
	// table's: the type says more about the same error.
	assert.Equal(t, exitcode.Of(exitcode.Failure, ""), TwinOf(errors.New("boom")))
	assert.Equal(t, "verification-unsupported",
		TwinOf(&verdict.UnsupportedError{Port: "jq"}).Reason)
}

func TestExitTableParity(t *testing.T) {
	seen := map[string]bool{}
	for _, tc := range exitTable(t) {
		require.False(t, seen[tc.name], "duplicate row %q", tc.name)
		seen[tc.name] = true
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, ExitCode(tc.err), "error: %v", tc.err)
			for _, s := range tc.is {
				require.ErrorIs(t, tc.err, s)
			}
			if tc.as != nil {
				require.ErrorAs(t, tc.err, tc.as)
			}
		})
	}
}

// Every DeclineType has a row, so a type added later cannot ship
// without its band pinned. Both taxonomies end where String stops
// naming members, which the first assertions hold in place.
func TestExitTableCoversEveryDeclineType(t *testing.T) {
	const past = "unknown decline"
	require.Equal(t, past, plan.DeclineType(1000).String())
	require.Equal(t, past, portstyle.DeclineType(1000).String())

	planCovered := map[plan.DeclineType]bool{}
	styleCovered := map[portstyle.DeclineType]bool{}
	for _, row := range exitTable(t) {
		var d *plan.Decline
		if errors.As(row.err, &d) {
			planCovered[d.Type] = true
		}
		var s *portstyle.Decline
		if errors.As(row.err, &s) {
			styleCovered[s.Type] = true
		}
	}
	for dt := plan.AlreadyCurrent; dt.String() != past; dt++ {
		assert.True(t, planCovered[dt], "plan.Decline %q (%d) has no row", dt.String(), dt)
	}
	for dt := portstyle.FieldUnsupported; dt.String() != past; dt++ {
		assert.True(t, styleCovered[dt], "portstyle.Decline %q (%d) has no row", dt.String(), dt)
	}
}

// Unknown subcommands never reach ExitCode: execute pre-flights
// root.Find and answers the usage band itself, cobra's message under
// dockhand's prefix. It is the one exit the table cannot express as an
// error value, so it is pinned by running it.
func TestExitTableUnknownCommandIsPreflighted(t *testing.T) {
	t.Setenv("DOCKHAND_TREE", "")
	var out, errb bytes.Buffer
	got := execute(context.Background(), "test", []string{"nonsense"}, &out, &errb)
	assert.Equal(t, exitcode.Usage, got)
	assert.Empty(t, out.String())
	assert.Contains(t, errb.String(), "dockhand: ")
	assert.Contains(t, errb.String(), "Run 'dockhand --help' for usage.")
}
