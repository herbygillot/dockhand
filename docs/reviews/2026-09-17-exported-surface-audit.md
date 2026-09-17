# Exported-surface audit

The roadmap's maintenance item asked for Tcl and upstream exports to be audited against real callers and protocol use. This pass covered every package under `internal`, right after the topic constants were consolidated so each fact had one definition.

## Method

Two tools, because each is blind to something the other sees.

- A small `go/types` index (kept in the session scratchpad, not the repository) listed every exported identifier and counted references from other packages, from tests, and from inside its own package, and flagged types that appear in exported signatures or fields. It cannot see methods reached only by promotion through an embedded struct, and with `Tests: true` it undercounts uses from external test packages of packages that also have internal tests.
- `golang.org/x/tools/cmd/deadcode`, run with and without `-test`, gives whole-program reachability for functions and methods. It cannot judge constants, variables, or types, and it says nothing about exported-but-internal.

The compiler arbitrated every disagreement: each change was applied, and `go vet ./...` compiling all test packages decided whether an identifier was really reachable. Two of the index's "delete" verdicts were wrong for exactly the reasons above, the host machine methods promoted into the Tart provider's interface and the provider's `BuildConfig` reached through an unexported interface in `app`, and both were restored by the build.

## Policy

- Delete what nothing reaches, tests included.
- Unexport what only its own package uses.
- Keep, and do not count as speculative: complete value vocabularies of an exported type (`record` states, `releasever.Stability`, `dependency.GitPolicy`, assessment codes), error types that exported functions return (`text.EditError`, `syntax.Error`), interfaces embedded in exported interfaces (`state.ImageCapabilityCache`), values set by the linker (`app.DefaultGitHubOAuthClientID`), values that arrive through a protocol (failure kinds and attributions decoded from guest results), and names the roadmap reserves (`ReviewAccept`, `ReviewDismiss`).
- Exports that exist only for tests stay exported when the test is in an external package; they are listed below rather than hidden.

## Changes

Deleted, unreachable even from tests: `Group.Key` in `distfiles`, `Dependent.BuildOnly` in `portindex`, `Service.Discover` in `upstream` (a wrapper the bound discovery no longer used), `shell.WithEnv`, `verify.ErrNotImplemented`, `git.CommandError` (an alias for `subprocess.Error`), the leftover `boundedBuffer` in `dependency`, and the `ErrNotFound` alias in `workflow`, whose uses now name `state.ErrNotFound`.

Moved into test files, since only tests reached them: `observationProfiles` and `contextBoundaries` in `portedit`, and `Provider.settings` and `describeEnvironment` in `verify/tart`.

Unexported, used only inside their package: `archive.errScanLimit`; the CLI's job-outcome errors; `fetch.errTooLarge`; `git.errSymbolicRef` and `Repository.objectTypes`; `macports.errNotPortsTree`; `dependency.generated` and `GitReference.declarable`; `fidelity.version`; `portedit.errProbeInconclusive`; `portindex.dependencyName`, `errMalformed`, `errNoIndex`; `publish.validateObservation`; `subprocess.errOutputLimit`; the default limits in `tcl/rpc` and `tcl/shell`; `Braced.scriptLens` and `listValue` in `tcl/syntax`; `upstream.errAutomaticUnsupported`; `verify.planSingleWithConfig`; `github.buildConfig`; `tart.errClosed`; and in `workflow`, `Engine.contributionBuild`, `Engine.verificationProvider`, `errNoState`, `errNotImplemented`, and `errUnsupportedAction`.

## Kept for tests

Reachable only from tests after this pass: `app.Status`, `cli.NewRoot`, `github.TokenSourceFunc.Token`, the option constructors of `tcl/rpc` and `tcl/shell`, `syntax.SpanOf` and `SegmentSpan`, `upstream.PatternFromCurrent`, and `verify.PlanSingle`. They are legitimate test seams; the option constructors are the package's configuration API even though production uses the defaults. `workflow.ErrClaimLost`, `ErrInvalidScope`, `ErrNoPendingJobs`, `ErrStaleRevision`, and `ErrRequestConflict`, `cli.ActionResult`, and `portindex.Index.Each` stay exported because external test packages assert on them.

## Result

`deadcode -test ./...` reports nothing. The full suite passes. `make deadcode` repeats the reachability check. The empty `internal/upstream/github` and `internal/upstream/gitlab` directories are untracked leftovers on this checkout and are removed here.
