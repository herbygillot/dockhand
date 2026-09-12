# Behavioral tests — 2026-09-11

## Selection

Filtered the current packages for those without `_test.go` files. At the start of this work (`613b464`), none of the packages had permanent tests, so all remained eligible.

Selected packages by the amount and interaction of implemented behavior. A Go AST inventory of production code counted conditionals, loops, switch cases, select clauses, and short-circuit Boolean operators as a supporting measure. These counts are decision points, not formal cyclomatic complexity scores.

| Package | Decision points before this change | Why it was selected |
| --- | ---: | --- |
| `internal/workflow` | 298 | Durable intake, multiple state transitions, provider admission, reconciliation, cancellation, claim expiry, and independent cleanup. |
| `internal/tcl/syntax` | 193 | Recursive Tcl syntax, quoting and substitution boundaries, source positions, lists, diagnostics, and selective traversal. |
| `internal/ledger` | 113 | Immutable snapshot reads, process-level writer locking, transaction deadlines, guarded Git refs, source retention, and uncertain commit outcomes. |

`internal/git` was the closest remaining candidate at 112 decision points. The ledger has additional coordination and persistence guarantees, and its integration tests also exercise the real Git adapter. CLI and Tcl RPC followed at 46 and 36 decision points respectively. Unimplemented packages and passive record definitions were not preferred over substantial existing behavior.

## Tests added

Added 34 top-level behavioral tests and two fuzz targets across the three selected packages. Tables and subtests cover additional cases. All tests use public package boundaries from external test packages.

### Workflow

- Canonical, concurrent, and conflicting submission retries; caller-owned inputs are frozen on acceptance. Retries return the original receipt after the change advances or closes.
- Invalid requests cannot publish partial intent. Verification can target older revisions while modifying actions reject stale revisions.
- Cancel controls are idempotent, use the shared request namespace, and apply only to the selected jobs.
- Status is an independent, read-only snapshot projection with correct scope and resource visibility.
- Capacity delays admission and retries without changing submission identity. Unknown submission outcomes reconcile before another submission. Closed identities are retired before retry, and partial provisioning is cleaned up.
- Observations must identify the correct run and carry consistent, current evidence. Dependency failures outside the edited cohort remain visible and retain resources for diagnosis.
- Cancel acknowledgement does not settle a build; observed completion wins a race with cancellation. Cancel errors do not starve observation.
- Live claims exclude competing cycles; expired attempt and cleanup claims reject late results. A provider that closes a submission identity prevents a paused submitter from creating a run after cancellation.
- Job completion and resource release have separate lifetimes. Uncertain cleanup survives reopening the ledger, and completed work is not repeated.

Tests use real temporary Git ledgers, a scripted provider, an atomic fake clock, and channel barriers. Every provider operation attempts a ledger transaction to check that workflow calls it outside the writer lock. The provider is a test adapter; these tests do not establish Tart's admission or fencing behavior.

### Ledger

- Strict schema decoding and round trips, unknown fields, trailing data, missing collections, mismatched IDs, and broken request indexes.
- Snapshot reads during an open writer transaction, caller-owned snapshot mutation, reopen persistence, no-op updates, and linear state history.
- Atomic adoption of state, source pins, and a prepared branch in both SHA-1 and SHA-256 repositories.
- Callback errors, invalid state, nested transactions, ref conflicts, cancellation, expired deadlines, and panic recovery leave committed state intact. Rejected ref transactions publish neither partial branches nor pins.
- Bounded advisory locking, initialization and reads while the lock is held, and concurrent stores preserving all accepted updates.
- Source object validation, corrupt-pin rejection, missing-pin repair without another state commit, and historical source retention through Git garbage collection.
- Corrupt ledger refs are distinguished from an absent ledger and cannot be overwritten by an update callback.
- A Git wrapper commits a ref transaction and then loses its acknowledgement through process termination. The store reports `ErrCommitUncertain`, invokes the callback once, and allows the committed result to be read back.
- Lockfile creation uses the configured path and preserves an existing file.

Git fixtures disable user/system configuration. Direct fixture commands discard inherited Git environment overrides. Lockfiles, object databases, and the fault-injection executable all live in test temporary directories.

### Tcl syntax

- An authored Portfile fixture includes nested `string map`, `format`, and `expr` substitutions, quoted interpolation, expansion, and conditional bodies. Tests assert syntax structure and exact byte spans without evaluating the source.
- Escaped delimiters, namespaces, braced variable names, array indexes, comments, continuations, and command boundaries.
- Windowed parsing preserves offsets into the original source; malformed syntax produces diagnostics and recovers subsequent commands where possible.
- Braced script/list lenses, selective command traversal, and iterator early termination.
- List quoting, escapes, dictionary duplicate keys, malformed lists, and odd dictionary cardinality.
- Fuzz targets check parser progress, ordered spans, containment, and valid source windows for arbitrary bounded byte inputs. They exercise syntax structure, not interpreter equivalence.

## Bugs found and fixed

The new list tests initially failed on backslash continuations and escaped values:

- `SplitList` treated backslash-newline as a separator, losing leading whitespace from an element or rejecting a valid element spanning lines. Continuations now remain within the unquoted element, including following spaces and tabs.
- `ListValue` stripped backslashes without decoding named or numeric escapes. It now handles named controls, newline folding, and octal/hexadecimal/Unicode escapes. Braced list elements preserve their contents, and malformed adjacency after a closing brace or quote still produces an error.

Checked the continuation behavior against the local Tcl interpreter and consulted the official [Tcl syntax](https://www.tcl-lang.org/man/tcl8.6/TclCmd/Tcl.htm) and [list parsing](https://www.tcl-lang.org/man/tcl8.6/TclLib/SplitList.htm) documentation for the regression expectations. This changes list helpers only; the source parser and workflow/ledger production code are unchanged. The tests do not require a Tcl interpreter.

## Validation and provenance

- Targeted package tests passed.
- `go test -race -coverprofile=... ./...` passed. Statement coverage: workflow **80.7%**, ledger **85.0%**, Tcl syntax **95.7%**. Coverage is measured per package's own suite, not across dependency calls; it is not branch coverage or proof of complete runtime behavior.
- Bounded fuzzing passed with two workers per target: **1,333,093** parser inputs and **1,093,970** list inputs over approximately 20 seconds each. No failing corpus entries were produced.
- The final race-enabled suite also passed from `~/Source/dockhand2` after installation. `go vet ./...`, `gofmt` checks, and `git diff --check` passed. Changes are left uncommitted.

All test code, fixtures, helper code, and list-decoding fixes were authored for this v2 pass. No v1 tests or comments were copied. No dependencies or new production packages were introduced. README and component status now describe the permanent suite; earlier activity reports remain historical records of temporary validation.
