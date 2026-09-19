# Review: message coherency, correctness, and level

Date: 2026-09-19. Reviewed the clean working tree at `7600d3b`, after the
structural pass landed. The question was whether what Dockhand prints matches
what it does, and whether each message sits at the right level. Scope: the whole
`dockhand help` surface, all 89 `progress` reports, the action summaries, the
JSON envelope, and the error lines. Messages were checked by reading the code
path that produces them and, where a command could run without a ports tree, by
running it. These observations are proposals for triage, not accepted
implementation work.

The level discipline is in better shape than the correctness. Of 89 reports,
identifiers are almost entirely confined to verbose, debug holds sub-operations
and provider calls, and several sites discriminate level by outcome rather than
by call site — `portedit/patches.go` reports at info only when a patch was
rejected and at verbose when everything applied, and
`workflow/contribution_lifecycle.go` reports at info only when a contribution's
disposition actually changed. That is the contract in
[output.md](../output.md) being applied deliberately, not by habit.

Three findings are real defects. The rest are consistency drift.

## 1. "Using GitHub verification." is printed before the fallback is known to work

[`app/build.go`](../../internal/app/build.go) reports at **info**, then attempts
the fallback:

```go
progress.Report(ctx, "No suitable prepared Tart image is available. Run %s to verify locally. Using GitHub verification.", setup)
...
resolved, githubErr := githubBuild()
if githubErr != nil && preserve && ctx.Err() == nil {
    return workflow.BuildResolution{Problem: "GitHub verification could not be configured: " + githubErr.Error()}, nil
}
```

`githubBuild` reaches
[`Services.githubBuild`](../../internal/app/github_verification.go), which fails
when there is no GitHub credential, no resolvable fork destination, no
repository info, or a remote that is not the person's own fork of
macports-ports. The message has already claimed the outcome.

This is the first-run path. A person who has installed Dockhand and run neither
`dockhand setup` nor `dockhand auth login` types `dockhand bump jq` and sees:

```
No suitable prepared Tart image is available. Run dockhand setup to verify locally. Using GitHub verification.
Error: <the GitHub authentication failure>
```

and on the `preserve` path, the same command emits "Using GitHub verification."
and then "GitHub verification could not be configured: …" — two contradictory
sentences about one decision. The second message, `"Tart is not available; using
GitHub verification."`, has the same shape.

Scope is the bump family only. [`cli/build.go`](../../internal/cli/build.go)
pins the provider to Tart whenever `--image`, `--capacity`, `--from-source`,
`--variant`, `--test-timeout`, or a non-workflow `--tests` is given, and
`verify` defaults to `tart` rather than `auto`, so `auto` only survives to this
code for `bump`, `bump-revision`, `refresh-checksums`, `amend`, and `rebase`
with no build flags — the default invocation.

Report the fallback as an attempt before calling `githubBuild`, or report the
choice after it succeeds. The message should not name an outcome the next
statement can contradict.

## 2. `gc` prints "No eligible cleanup." when nothing was examined

[`cli/maintenance.go`](../../internal/cli/maintenance.go) renders the result
before checking the call's error:

```go
if len(result.Items) == 0 {
    fmt.Fprintln(cmd.OutOrStdout(), "No eligible cleanup.")
}
...
if callErr != nil {
    return databaseReadError(callErr)
}
```

Observed directly, outside a ports tree:

```
$ dockhand gc --dry-run
not a MacPorts ports tree: … (stderr)
No eligible cleanup.            (stdout, exit 1)
```

An empty item list because collection never ran is reported as the affirmative
fact that there was nothing to clean. Anything reading stdout sees a clean
sweep. Printing partial results before a late failure is right for
`--all-repositories`, where some registrations can succeed; it is the
zero-items sentence specifically that needs `callErr == nil`.

## 3. `gc --json` returns a result object on failure

[output.md](../output.md) states that `result` is "typed per command and null on
failure". Every command observed honors this except `gc`:

```
$ dockhand bump jq --diff --json
{"command":"bump","exit_code":1,"error":"not a MacPorts ports tree: …","result":null}

$ dockhand gc --json --dry-run
{"command":"gc","exit_code":1,"error":"not a MacPorts ports tree: …","result":{"Before":"…","DryRun":true,"Items":[]}}
```

Same root cause as item 2. A consumer that branches on `result != null` treats
this failure as a successful empty sweep.

## 4. Identifiers at the info level, including where output.md names the command

"Identifiers never appear at the info level" is the stated rule, and for
`setup`, `auth`, and `gc` output.md specifically says "their existing
summaries, trimmed of identifiers". Four sites print them at info:

- [`app/preparation.go`](../../internal/app/preparation.go): `"Continuing
  contribution %s from recorded source %s"` prints a `ChangeID` and a commit SHA.
- [`cli/maintenance.go`](../../internal/cli/maintenance.go): `gc` prints
  `Registration <RepositoryID>: <path>`, and each cleanup line's target is a
  `ResourceID` or `AttemptID` unless a path is available.
- [`macports/portindex/cache.go`](../../internal/macports/portindex/cache.go)
  and [`index.go`](../../internal/macports/portindex/index.go): both print a
  12-character source tree hash. The "this may take several minutes" half of
  the index message is properly info — it changes what a person should expect —
  but the hash identifies nothing a person can act on.

One tension is worth deciding rather than fixing.
[`view.PortSelector`](../../internal/workflow/view/contribution.go) returns
`--job <ID>` for a job with no targets, and `pendingGuidance` uses it at info to
build a runnable `dockhand wait …`. There is no way to tell someone how to
resume standalone work without naming the job, so either the rule admits an
exception for a command a person is meant to run, or that guidance is not
printable at info. `view.PortLabel`'s bare job ID as a *label* is weaker — it
reads as if the port were named after a UUID.

## 5. `-v` changes what `status` does, not just what it shows

[`cli/status.go`](../../internal/cli/status.go) orders the branches so that the
verbose check returns first:

```go
if r.level(cmd) >= progress.Verbose {
    return renderStatus(cmd.OutOrStdout(), status)
}
if !printOnly && isTerminal(…) {
    return r.liveStatus(cmd, filter, all)
}
```

Plain `dockhand status` on a terminal opens the live table and, through
`Options.Processor`, advances the repository's pending work for as long as it is
open. `dockhand status -v` — or `--debug` — silently does neither. The help text
documents `--print` as the flag that "processes nothing" and says of `-v` only
that it "prints the full record with identifiers".

Levels are supposed to select what is shown. A person adding `-v` to see more
detail is unlikely to expect that they also stopped the driver work that the
same command was doing a moment ago. Either say so in the help, or let `-v`
select the detailed rendering without taking the processing path away.

## 6. `--trace` does more than its flag help says

The flag reads "Follow build logs on stderr through completion". In
[`cli/progress.go`](../../internal/cli/progress.go) it also forces the level to
`progress.Debug`, which is what output.md describes ("`--trace` means debug plus
the guest's own log stream"). The behavior is right and the design doc agrees
with it; only the flag's own help understates it, so a person reaching for build
logs gets every sub-operation as well without being told.

## 7. JSON result payloads are cased two different ways

The envelope is consistent everywhere: `command`, `exit_code`, `error`,
`result`. The payloads inside `result` are not.

```
auth status → {"host":"github.com","account":"herbygillot","source":"…","authenticated":true}
gc          → {"Before":"…","DryRun":true,"Items":[]}
db check    → {"Valid":true}
```

`app/auth.go` and `workflow/view` carry JSON tags; `outdated`, `assess`,
`workflow/retention.go`, and `app/maintenance.go` carry none, and
`cli/maintenance.go` emits anonymous `struct{ Valid bool }` and
`struct{ Current bool }` literals. output.md says only that `result` is "typed
per command", so this violates no stated rule — but `--json` is a documented
machine-readable interface, and a script reading it gets `account` from one
command and `Valid` from another.

## 8. Smaller consistency drift

**Error prefixes leak unevenly.** `dockhand db check` outside a database prints
`state: database does not exist`, while sibling failures print
`not a MacPorts ports tree: …` and `branch must name a literal local branch`.
The package prefix is ordinary Go error-wrapping convention and there are ~450
such strings; nothing strips them at the CLI boundary, so whether a person sees
one depends on which layer produced the error. Worth one decision either way.

**Six of 51 progress messages start lowercase** — `git.branch pins a commit…`,
`branch cleanup skipped…`, `host access not explained…`, `evaluating without a
shared session…`, `go.toolchain_min %s already covers…` — against 45 that begin
with a capital. output.md calls these "user-facing sentences, not log lines".

**`review` is undifferentiated in the root help.** `dockhand review accept
--help` says plainly "This command's workflow is not implemented yet", which is
honest, but the root help lists `review` under "Additional Commands" beside
`help` and `completion` with the description "Record a review decision" and no
hint of its status. Every implemented command has an explicit group.

**`--test-timeout` hardcodes its own default.** The flag's default value is
`config.Tart.TestTimeout`, which is zero unless set, so Cobra prints nothing and
the usage string carries the literal text "default 30m". The real default is
`verify/tart.DefaultTestTimeout`, resolved later in `Config.testTimeout()`. The
help is accurate today and silently wrong the day that constant changes.

## Checked and correct

These were suspected and hold, and are recorded so the next pass does not
re-litigate them.

- **`--skip-verify` PR disclosure.** The PR body states "The author asked
  dockhand to publish this change without verification (`--skip-verify`)" — an
  intent claim on a public pull request. It is reachable only through
  `input.SkipVerify`: [`publication_bind.go`](../../internal/workflow/publication_bind.go)
  refuses with "verify the committed contribution before publishing" when
  verification is required and no candidate exists, so a zero evidence attempt
  cannot arrive any other way.
- **"The explicit version is honored"** in the `--diff` prerelease warning. This
  prints whenever `LeavesStable` is set, including when no version argument was
  given, so it would misattribute an automatic bump. It cannot: `admits()`
  rejects a prerelease candidate unless the port is already on one, and it is
  applied on the releases path, the tags path (where the forge's `Prerelease`
  flag is always false and would otherwise filter nothing), and the HTTP listing
  path.
- **`--diff` "without … opening the state database"** — the preview path calls
  `app.PreviewPreparation`, which opens the ports tree and the preparation
  service only.
- **"Ctrl-C detaches without canceling accepted work"** — `signal.NotifyContext`
  cancels the context, the attach loop records `Interrupted`, and submission was
  already committed.
- **`status`'s "--json and output that is not a terminal imply --print"** — both
  branches confirmed.
- **The provider/tests default asymmetry** between `verify` (`tart`,
  `--tests declared`) and the bump family (`auto`, no `--tests` default) is
  deliberate in `cli/build.go` and each command's help states its own default
  correctly.
- **The JSON envelope** itself is correct and uniform across every command
  observed, including on failure, except for `gc`'s result field.

## Suggested order

Items 1 through 3 are defects and are each a small, local change. Item 4 is the
stated contract being applied unevenly and is mostly moving calls to
`VerboseReport`, once the `PortSelector` question is decided. Item 5 is a
behavior surprise worth settling before more is built on the live table. Items 6
through 8 are wording and convention.
