# Guest log corpus

Tart-shaped verification logs, one per file, each with a sidecar stating
the judgment the settle-time readers must reach from it. The readers
live in `settle.go` — `LintSummary`, `failureSummary`, `portDeclined`,
`dependencyFailure` over `failedPortRE` — and `settle_table_test.go`
sweeps this directory: every `<name>.log` is read, its `<name>.expect`
is required, and the readers run over it singly and then composed
through `SettleRuns`, with the `verifytest` fake standing in for the
guest. A log dropped here is picked up with no code change; a log
without a sidecar fails the sweep by name.

A guest log is `/tmp/dockhand-verify/log` in the worker: the combined
stdout and stderr of `port lint <port>`, then `port -d -N -k test
<port>` when the run asked for tests, then `port -d -N [-s] install
<port> [variants]`, appended in that order and stopped at the first
command that fails (`internal/verify/tart/tart.go`, `runner`). The
`-d` is why real captures carry `DEBUG:` lines, and it is deliberate:
the environment is disposable and the log is the artifact.

## Dropping in a real capture

    dockhand log <branch> > internal/engine/testdata/logs/<name>.log

Then write `<name>.expect` by hand from what you saw in the field —
the point of a corpus is that a person states the truth and the code
is measured against it — and run `go test ./internal/engine/`. Name
the file for the port and the shape (`gomuks-olm-dependency`), and
replace a reconstruction below with the real thing whenever one lands:
keep the name, rewrite the sidecar's provenance comment.

## The sidecar

`key: value` lines. `#` comments and blank lines are ignored; a value
runs from the first colon to the end of its line, so a detail may
itself contain colons. Keys other than these fail the sweep.

| Key | Meaning |
|---|---|
| `port` | The port under test, as the note names it: a subport's own name, never the portdir's base. Required. |
| `outcome` | What the guest's state file said: `passed` or `failed`. Required. |
| `state` | The state the note settles to: `passed`, `failed`, `blocked` or `unsupported`. Required. |
| `blamed` | The port `dependencyFailure` names, set exactly when the state is `blocked`. |
| `detail` | The detail the note settles to: empty for a pass; the first substantive `Error:` line for a failure; `dependency <blamed> fails to build; the change itself is untested` when blocked; `the port declines to build on this platform` when unsupported. |
| `lint` | What the log's lint line says, whatever the outcome: `clean`, `1 warning`, `N warnings`, or empty when lint never reported. The note records it only on a pass. |

The blocked detail is never annotated `(nomaintainer)` here. That
annotation is a lookup in the tree the settle runs against, exercised
by `TestBlockedDetailAnnotatesNomaintainer` against a fixture tree; the
sweep settles against a throwaway repo holding no dependency's
Portfile.

## Provenance

Every file here is a RECONSTRUCTION, not a capture. The lines that
carry the judgment are verbatim from the shapes the existing tests and
`docs/decisions.md` quote; the progress and `DEBUG:` lines around them
are what `port -d` prints, written from memory of MacPorts output
rather than copied from a guest. They are faithful to the shape the
readers key on and approximate everywhere else — which is exactly why
real captures should replace them.

| File | Shape | Reconstructed from |
|---|---|---|
| `gomuks-olm-dependency` | The dependency `olm` breaks before the change is reached; the verdict blamed the bump. Settles blocked, `olm` blamed, worker released. | `TestSettleRunsDependencyFailureIsBlocked`, `docs/decisions.md` (blame refinements) |
| `pcre2-built-as-pcre` | The branch changed the subport `pcre2` in `devel/pcre`; verify submitted the portdir's base name and the guest built the untouched `pcre` 8.45 from its archive, cleanly. No log reader can see the mismatch — `ChangedPort` exists for that — so this must settle as a plain pass. | `changedport.go` (macports-ports-46), `TestStatusPumpSubmitsTheNotesSubport` |
| `jq-build-failure` | The port under test fails in its own build phase. Settles failed with the phase line as the detail, worker kept. | `TestSettleRunsRecordsTheFailureDiagnosis` |
| `jq-known-to-fail` | The port refuses the platform in words the readers recognize. Settles unsupported, worker released. | `TestSettleRunsReadsARefusalAsUnsupported` |
| `php-xcode-refusal` | A refusal in the port's own words, no marker: the php PortGroup's Xcode 12 guard. Dockhand's pre-flight records `known_fail` ports unsupported before a VM boots; when a refusal reaches the guest anyway, the conservative reading keeps it failed with the port's own complaint as the detail. | `internal/macports/testdata/portgroups/php-1.1.tcl` |
| `lint-errors` | `port lint` finds errors, so the runner stops there. Settles failed with lint's first `Error:` line as the detail; the lint summary still reads its warning count. | `LintArgs` doc in `internal/macports/build/build.go` |
