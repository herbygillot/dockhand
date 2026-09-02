package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"syscall"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/checksums"
	"github.com/herbygillot/dockhand/internal/distfile"
	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/intent/bumprevision"
	"github.com/herbygillot/dockhand/internal/lifecycle"
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
	"github.com/herbygillot/dockhand/internal/runstate"
	"github.com/herbygillot/dockhand/internal/tcl/rpc"
	"github.com/herbygillot/dockhand/internal/tcl/shell"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/text"
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
	assert.Equal(t, 3, exitcode.Environment)
	assert.Equal(t, 4, exitcode.Tree)
	assert.Equal(t, 5, exitcode.Declined)
	assert.Equal(t, 6, exitcode.Verify)
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
func exitTable() []exitRow {
	ctx := context.Background()
	const branch = "dockhand/jq-1.8"
	const sha = "0123456789abcdef0123456789abcdef01234567"
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
	// SubmitVerification's `later` closure: the branch stands, the
	// cause rides along typed so status's pump can tell a full machine
	// from a missing capability.
	later := func(cause error) error {
		return &lifecycle.VerifyDeferredError{Branch: branch, Reason: cause.Error(), Cause: cause}
	}
	noEnv := func(format string, a ...any) error {
		return fmt.Errorf("%w: "+format, append([]any{verify.ErrNoEnvironment}, a...)...)
	}
	noteErr := fmt.Errorf("note on %s does not parse: %w — `git notes --ref=%s remove %s` clears it",
		sha, parseErr, git.VerifyNotesRef, sha)

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

	// Band 3: the machine.
	add(exitcode.Environment,
		exitRow{name: "*verify.CapacityError (tart.Admit)", err: capacity, as: new(*verify.CapacityError)},
		exitRow{name: "*lifecycle.VerifyDeferredError (verifyBranch summary, no cause)",
			err: &lifecycle.VerifyDeferredError{Branch: branch,
				Reason: fmt.Sprintf("%d release(s) deferred — each line above names its remedy; `dockhand status` retries them as remedies are met", 1)},
			as: new(*lifecycle.VerifyDeferredError)},
		exitRow{name: "*lifecycle.VerifyDeferredError over *verify.CapacityError (SubmitVerification)",
			err: later(capacity), as: new(*verify.CapacityError)},
		exitRow{name: "*lifecycle.VerifyDeferredError over verify.ErrNoEnvironment (SubmitVerification)",
			err: later(noEnv("no base images; run `dockhand provision tart --macos <release>` first")),
			is:  []error{verify.ErrNoEnvironment}, as: new(*lifecycle.VerifyDeferredError)},
		exitRow{name: "*lifecycle.VerifyDeferredError over verify.ErrUnsupported (SubmitVerification)",
			err: later(fmt.Errorf("%w: %s is not a <category>/<port> directory", verify.ErrUnsupported, "stage-jq")),
			is:  []error{verify.ErrUnsupported}, as: new(*lifecycle.VerifyDeferredError)},
		exitRow{name: "*lifecycle.VerifyDeferredError over an untyped submit failure (SubmitVerification)",
			err: later(errors.New("the agent never answered")), as: new(*lifecycle.VerifyDeferredError)},
		exitRow{name: "verify.ErrNoEnvironment", err: verify.ErrNoEnvironment, is: []error{verify.ErrNoEnvironment}},
		exitRow{name: "verify.ErrNoEnvironment (RealVMProvider: tart missing)",
			err: noEnv("tart is not installed (`port install tart`); --no-verify skips verification"),
			is:  []error{verify.ErrNoEnvironment}},
		exitRow{name: "verify.ErrNoEnvironment (RealVMProvider: no bases)",
			err: noEnv("no base images; run `dockhand provision tart --macos <release>` first"),
			is:  []error{verify.ErrNoEnvironment}},
		exitRow{name: "verify.ErrNoEnvironment (runVerification: Errored verdict)",
			err: noEnv("%s", "the guest agent timed out"), is: []error{verify.ErrNoEnvironment}},
		exitRow{name: "verify.ErrNoEnvironment (resolveReleaseSet: no base images)",
			err: noBases, is: []error{verify.ErrNoEnvironment}},
		exitRow{name: "verify.ErrNoEnvironment (resolveReleaseSet: --on release without a base)",
			err: noBaseFor, is: []error{verify.ErrNoEnvironment}},
		exitRow{name: "verify.ErrNoEnvironment (provision: base missing after provisioning)",
			err: noEnv("base image %s is not present after provisioning", "dockhand-base-sequoia"),
			is:  []error{verify.ErrNoEnvironment}},
		exitRow{name: "verify.ErrUnsupported", err: verify.ErrUnsupported, is: []error{verify.ErrUnsupported}},
		exitRow{name: "verify.ErrUnsupported (tart.Provider: unserved release)",
			err: fmt.Errorf("%w: no base for %s", verify.ErrUnsupported, "Sequoia"), is: []error{verify.ErrUnsupported}},
		exitRow{name: "prefix.ErrNotInstalled (prefix.Find)",
			err: fmt.Errorf("%w (no port-tclsh on PATH or under /opt/local)", prefix.ErrNotInstalled),
			is:  []error{prefix.ErrNotInstalled}},
		exitRow{name: "eval.ErrStartup over shim.ErrNoShims (session.Start)",
			err: fmt.Errorf("%w: %w", eval.ErrStartup, shim.ErrNoShims), is: []error{eval.ErrStartup, shim.ErrNoShims}},
		exitRow{name: "eval.ErrStartup (session.Start: shim initialization)",
			err: fmt.Errorf("%w: initializing shim: %w", eval.ErrStartup, errors.New("broken pipe")),
			is:  []error{eval.ErrStartup}},
		exitRow{name: "eval.ErrRootRefused", err: eval.ErrRootRefused, is: []error{eval.ErrRootRefused}},
		exitRow{name: "portfetch.ErrRootRefused", err: portfetch.ErrRootRefused, is: []error{portfetch.ErrRootRefused}},
		// The session owns the bootstrap eval and portfetch share, and
		// with it the sentinels; theirs alias it, which the is lists
		// prove — a re-declared sentinel would keep its own band and
		// silently drop the other package's.
		exitRow{name: "session.ErrRootRefused (session.Start; eval and portfetch alias it)",
			err: session.ErrRootRefused, is: []error{session.ErrRootRefused, eval.ErrRootRefused, portfetch.ErrRootRefused}},
		exitRow{name: "session.ErrStartup over shell.Start (session.Start)",
			err: fmt.Errorf("%w: %w", session.ErrStartup, errors.New("fork/exec /nowhere/bin/port-tclsh: no such file or directory")),
			is:  []error{session.ErrStartup, eval.ErrStartup}},
		exitRow{name: "session.ErrStartup (portfetch.New over session.Start: shim initialization)",
			err: fmt.Errorf("%w: initializing shim: %w", session.ErrStartup, errors.New("broken pipe")),
			is:  []error{session.ErrStartup}},
	)

	// Band 4: the tree.
	add(exitcode.Tree,
		exitRow{name: "tree.ErrNotPortsTree", err: tree.ErrNotPortsTree, is: []error{tree.ErrNotPortsTree}},
		exitRow{name: "tree.ErrNotPortsTree wrapped (tree.Open)",
			err: fmt.Errorf("%s: %w", "/nowhere", tree.ErrNotPortsTree), is: []error{tree.ErrNotPortsTree}},
		exitRow{name: "tree.ErrPortNotFound (tree.Resolve)",
			err: fmt.Errorf("%q: %w", "someport", tree.ErrPortNotFound), is: []error{tree.ErrPortNotFound}},
		// tree.Resolve names the missing PortIndex in prose; the
		// portindex sentinel itself is not wrapped in, so the band is
		// the tree's and portindex.ErrNoIndex stays a plain failure
		// wherever it escapes raw (see band 1).
		exitRow{name: "tree.ErrPortNotFound (tree.Resolve: no PortIndex)",
			err: fmt.Errorf("%q: %w (the tree has no PortIndex; run portindex to enable name lookup)", "someport", tree.ErrPortNotFound),
			is:  []error{tree.ErrPortNotFound}},
		exitRow{name: "git.ErrNotARepo (git.Open)",
			err: fmt.Errorf("%w: %s", git.ErrNotARepo, "/nowhere"), is: []error{git.ErrNotARepo}},
		exitRow{name: "git.ErrNotARepo (planOnBase)",
			err: fmt.Errorf("%w — the branch workflow needs a git checkout; --in-place edits the tree directly",
				fmt.Errorf("%w: %s", git.ErrNotARepo, "/nowhere")),
			is: []error{git.ErrNotARepo}},
	)

	// Band 5: a judgment with a remedy. plan.Decline is every planner's
	// refusal; the sweep test below insists each DeclineType has a row.
	add(exitcode.Declined,
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
		exitRow{name: "*plan.Decline LatestUnresolved (bump.ResolveLatest)",
			err: &plan.Decline{Type: plan.LatestUnresolved,
				Detail: fmt.Sprintf("%s (%s)", "no signal", "livecheck found nothing and the forge has no tags")}, as: new(*plan.Decline)},
		exitRow{name: "*plan.Decline VendoredBlock (refresh: go.vendors)",
			err: &plan.Decline{Type: plan.VendoredBlock, Detail: "go.vendors"}, as: new(*plan.Decline)},
		exitRow{name: "*plan.Decline wrapped",
			err: fmt.Errorf("bump: %w", &plan.Decline{Type: plan.AlreadyCurrent}), as: new(*plan.Decline)},
		exitRow{name: "*portstyle.Decline FieldUnsupported (portstyle.Locate)",
			err: &portstyle.Decline{Type: portstyle.FieldUnsupported, Field: info.FieldDescription}, as: new(*portstyle.Decline)},
		exitRow{name: "*portstyle.Decline UnknownStyle (portstyle.Locate)",
			err: &portstyle.Decline{Type: portstyle.UnknownStyle, Field: info.FieldVersion}, as: new(*portstyle.Decline)},
		exitRow{name: "*portstyle.Decline NotLiteral with candidates (portstyle.Locate)",
			err: &portstyle.Decline{Type: portstyle.NotLiteral, Field: info.FieldVersion,
				Candidates: []portstyle.Candidate{{Style: portstyle.SetVariable, Literal: false}}}, as: new(*portstyle.Decline)},
		exitRow{name: "*lifecycle.BranchInFlightError (MintFromPlan translating git.ErrBranchExists)",
			err: &lifecycle.BranchInFlightError{Branch: branch}, as: new(*lifecycle.BranchInFlightError)},
		exitRow{name: "*lifecycle.BranchInFlightError with Reason (replaceInFlight: --force refused)",
			err: &lifecycle.BranchInFlightError{Branch: branch, Reason: fmt.Sprintf(
				"%s carries %d commit(s) beyond the mint — --force replaces only what dockhand placed; `dockhand discard %s` first if you mean to drop your own work",
				branch, 1, branch)}, as: new(*lifecycle.BranchInFlightError)},
		exitRow{name: "*verdict.DuplicatePRError (promote)",
			err: &verdict.DuplicatePRError{Title: "jq: update to 1.8", URL: "https://github.com/macports/macports-ports/pull/1"},
			as:  new(*verdict.DuplicatePRError)},
		// A Coder anywhere in the chain wins, even under a sentinel.
		exitRow{name: "precedence: tree.ErrNotPortsTree wrapping *plan.Decline",
			err: fmt.Errorf("%w: %w", tree.ErrNotPortsTree, &plan.Decline{Type: plan.AlreadyCurrent}),
			is:  []error{tree.ErrNotPortsTree}, as: new(*plan.Decline)},
	)

	// Band 6: the port does not build. Its own band because it is its
	// own kind of outcome — the tool worked, the machine worked.
	add(exitcode.Verify,
		exitRow{name: "*lifecycle.VerifyFailedError (FollowRun --trace)",
			err: &lifecycle.VerifyFailedError{Port: "jq"}, as: new(*lifecycle.VerifyFailedError)},
		exitRow{name: "*lifecycle.VerifyFailedError with environment kept (runVerification)",
			err: &lifecycle.VerifyFailedError{Port: "jq", Handle: "dockhand-jq-1"}, as: new(*lifecycle.VerifyFailedError)},
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
		exitRow{name: "promote: failed-verification refusal (untyped fmt.Errorf)",
			err: fmt.Errorf("%s: tip %s has a failed verification — fix it, `dockhand discard` it, or --no-verify to promote anyway", branch, tip)},
		exitRow{name: "promote: merged-PR dead end (untyped fmt.Errorf)",
			err: fmt.Errorf("PR #%d for %s already merged (%s) — `dockhand clean` retires the branch", 7, branch, "https://github.com/macports/macports-ports/pull/7")},
		exitRow{name: "promote: PR create failed after push",
			err: fmt.Errorf("the branch is pushed; opening the PR failed: %w", fmt.Errorf("gh %s: %s", "pr", "HTTP 422"))},
		exitRow{name: "promote: PR edit failed after push",
			err: fmt.Errorf("the branch is pushed; refreshing PR #%d failed: %w", 7, fmt.Errorf("gh %s: %s", "pr", "HTTP 422"))},
		exitRow{name: "lifecycle.ErrAmbiguousTarget (ResolveBranch)",
			err: fmt.Errorf("%w: %q names %d branches (%s); use the full branch name", lifecycle.ErrAmbiguousTarget, "jq", 2, "dockhand/jq-1.8, dockhand/jq-1.9"),
			is:  []error{lifecycle.ErrAmbiguousTarget}},
		exitRow{name: "ResolveBranch: no dockhand branch (untyped fmt.Errorf)",
			err: fmt.Errorf("no dockhand branch for %q; `dockhand status` lists what is in flight", "jq")},
		exitRow{name: "plan.ErrDrift (verifyPlan)",
			err: fmt.Errorf("%w: %s", plan.ErrDrift, "/tree/sysutils/jq"), is: []error{plan.ErrDrift}},
		exitRow{name: "plan.ErrDrift (planOnBase)",
			err: fmt.Errorf("%w: the Portfile on %s is not the one planned against — commit your work there first, or use --in-place", plan.ErrDrift, "main"),
			is:  []error{plan.ErrDrift}},
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
		exitRow{name: "note validation: does not parse (ReadNote)", err: noteErr},
		exitRow{name: "note validation: newer schema (ReadNote)",
			err: fmt.Errorf("note on %s was written by a newer dockhand (schema %d, this build speaks %d); upgrade dockhand", sha, 99, 2)},
		exitRow{name: "note validation: sha mismatch (ReadNote)",
			err: fmt.Errorf("note on %s claims to describe %s — corrupt; `git notes --ref=%s remove %s` clears it", sha, "ffff", git.VerifyNotesRef, sha)},
		exitRow{name: "submit-and-record compensation: release failed too (SubmitVerification)",
			err: fmt.Errorf("recording the run failed (%w) and releasing %s failed too: %w — `tart delete %s` frees the slot",
				noteErr, "fake-1", errors.New("tart delete: no such vm"), "fake-1")},
		exitRow{name: "submit-and-record compensation: worker released (SubmitVerification)",
			err: fmt.Errorf("recording the run failed; the worker was released: %w", noteErr)},
		exitRow{name: "verify: branch changes several portdirs (branchPortdir)",
			err: fmt.Errorf("verify: %s changes %d portdirs against %s; one at a time for now", branch, 2, tip)},
		exitRow{name: "lifecycle.ChangedPort: evaluation failed",
			err: fmt.Errorf("lifecycle: evaluating %s at %s: %w", "sysutils/jq", tip, errors.New("Portfile: invalid command name"))},
		exitRow{name: "lifecycle.ChangedPort: several contexts",
			err: fmt.Errorf("lifecycle: the branch changes %d contexts (%s); name the one to verify: `dockhand verify <subport>`", 2, "demo, demo2")},
		exitRow{name: "verify: job ended in state (runVerification)",
			err: fmt.Errorf("verify: job ended in state %s", "running")},
		exitRow{name: "exec: the command failed on N of M releases",
			err: fmt.Errorf("exec: the command failed on %d of %d releases", 1, 2)},
		exitRow{name: "provision: aggregate failure (provisionAll)",
			err: fmt.Errorf("provisioning failed for %s", "Sequoia, Sonoma")},
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
		exitRow{name: "forge.UpstreamRepo: unreadable remote",
			err: fmt.Errorf("cannot read owner/repo from remote %q (%s)", "origin", "nonsense")},
		exitRow{name: "forge.RealGhOut: gh failed",
			err: fmt.Errorf("gh %s: %s", "api", "HTTP 401")},
		exitRow{name: "forge.RealGhOut: gh missing",
			err: errors.New("gh not found on PATH (`port install gh`)")},
		exitRow{name: "forge.LookupPR: unreadable tracked remote",
			err: fmt.Errorf("cannot read an owner from remote %q", "herby")},
		exitRow{name: "forge.QueryPR: unreadable JSON",
			err: fmt.Errorf("reading PR lookup: %w", parseErr)},
		exitRow{name: "forge.OpenPortPRs: unreadable JSON",
			err: fmt.Errorf("reading PR search: %w", parseErr)},
		exitRow{name: "forge.ForkRemote: no such remote",
			err: fmt.Errorf("no remote %q", "nope")},
		exitRow{name: "forge.ForkRemote: unreadable override remote",
			err: fmt.Errorf("remote %q: cannot read an owner from %s", "nope", "nonsense")},
		exitRow{name: "forge.ForkRemote: gh needed",
			err: fmt.Errorf("finding your fork needs gh: %w (or pass --remote)", errors.New("gh not found on PATH (`port install gh`)"))},
		exitRow{name: "forge.ForkRemote: several fork remotes",
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
	for _, s := range []error{
		checksums.ErrUnresolved,
		checksums.ErrMalformed,
		distfile.ErrUnavailable,
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
		upstream.ErrNoGit,
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

func TestExitTableParity(t *testing.T) {
	seen := map[string]bool{}
	for _, tc := range exitTable() {
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
	for _, row := range exitTable() {
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
