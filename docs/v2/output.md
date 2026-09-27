# Output and interface design

Agreed on 2026-09-17 as the contract for the interface pass. It covers what each command prints, at which level, in which format, and how `status` becomes a live view. The [CLI design](cli-design.md) still defines what commands do; this page defines what a person sees.

## Levels

Every operation reports progress through `progress.Report`. Each report carries a level, and the CLI filters:

| Level | Shown by | Content |
| --- | --- | --- |
| info | default | the minimum a person needs to follow the command (see below) |
| verbose | `-v` | identifiers (job, change, attempt, request, commit), index and image work, capacity waits, evidence reuse decisions |
| debug | `-vv` or `--debug` | every sub-operation as it starts and finishes, provider calls, guest transfer, MacPorts `DEBUG:` lines |

`--trace` means debug plus the guest's own log stream; it is no longer a separate mode. Reports always go to stderr and results always to stdout, at every level, so `--json` output stays parseable while reports scroll.

No logging library is adopted. Reports are user-facing sentences, not log lines; the structured form of a run is its JSON result.

`--timestamps`, a global output flag, puts the wall clock on every line the command writes to stderr, reports and the command's own lines alike, `22:38:20 fixture: branch … prepared`; in JSON mode the reports carry it as a `time` field instead, so their lines stay pure. It says when a line happened. How long a phase took is the record's to say, since a reattached `wait` cannot know what happened before it attached: a finished job's narration ends with one line, `took 34m12s: preparation 2m14s, verification 31m40s, publication 12s`, the branch and verdict lines carry their own `in 2m14s`, and `status -v` shows the same durations under `took:`. The phases come from timestamps the record holds, acceptance, the branch integrated, an attempt's creation and its verdict, the publication confirmed, and the finish, so the numbers are the same on every reading (2026-09-22).

Who is reporting decides the level, not only what is reported (decided 2026-09-19, after the default-to-publish bump made every change command its own driver). A command attached to its job narrates that job in the status table's words, one line per milestone: the change, the branch, the build's platform and verdict, the pull request, and any outcome a person must act on. It drives the workflow under a quiet context that lowers the engine's and providers' info reports to verbose, since to it they are the work behind the scenes. `serve` and the live status table drive under the plain context and are the audience for those reports at info. `-v` restores the full interleaving on any command.

## The JSON envelope

Every command supports `--json`, and every JSON result has the same envelope:

```json
{"command": "bump", "exit_code": 0, "error": "", "result": {}}
```

`command` is the verb; `exit_code` repeats the process exit code so a captured file needs no process to ask; `error` is empty on success and the one-line failure otherwise; `result` is typed per command and null on failure. The exit codes are unchanged: 0 for the requested milestone, 1 for other errors, 2 for failed work, 3 for needs-attention or superseded work, 130 for interruption or canceled work. In JSON mode, progress reports are line-delimited events on stderr, one JSON object per line with `level`, `scope`, and `message`, and the envelope is the last line on stdout.

## The minimum per command

The info level prints only these; everything else moves to verbose or debug.

| Command | Info output |
| --- | --- |
| `bump`, `bump-revision`, `checksums` | port, old version to new version (or the revision), the branch name, then the verdict line, then the PR URL and whether it was created or updated; `--detach` stops after the branch name |
| `--dry-run` variants | the same header lines, then the diff on stdout |
| `verify` | port, platform, verdict; on failure the phase that failed and where the log is |
| `publish` | PR URL, created or updated, the head commit |
| `sync` | PR state and the one-line status (mergeable, review, checks); what was retired or cleaned up |
| `abandon` | what was preserved |
| `assess`, `outdated` | the headline line per port as today, with plain words for the verdict (see below) |
| `status` | the contribution table (below); `--json` gives the projection |
| `wait`, `cancel` | the verdict or confirmation line |
| `setup`, `auth`, `gc` | their existing summaries, trimmed of identifiers |

Identifiers never appear at the info level. A person reaches for an ID to file a bug or to pass `--change`, and both are verbose-tier actions. Warnings that change what a person should expect, such as a bump that takes a port out of stable or a patch that no longer applies, are info.

Headline verdict words: `assess` says "ready" instead of "input-found" and "candidate ready" instead of "candidate-checked"; the codes stay in JSON.

## The contribution projection

`status` is contribution-centric. The engine gains one projection over its existing reader, used by the plain output, the JSON result, and the table alike, so all three say the same thing:

- **port** and **version move**: old to new, or the revision bump.
- **phase**: preparation, verification, publication, done.
- **state**: the phase's current word: preparing, building on macOS 26, waiting for capacity, verified, publishing, published, published unverified, merged, closed, abandoned, needs attention.
- **retry**: the verb that re-runs stopped work without redoing it, which the table's key runs: the contribution's own preparing action, which adopts the branch already built and keeps its publication, or `verify` for a branch dockhand did not prepare. Absent when nothing is stopped.
- **next**: what happens or is needed next: "verification pending", "PR open, 3 checks pending", "merged; branches cleaned", "merged; fork branch author/ports:dockhand/bump/jq cleanup pending: kept: push failed", "needs attention: patch rejects 4 hunks", "waiting for a Tart slot".
- **active job**: at most one; its last progress message.
- **history**: earlier jobs and attempts, shown only when expanded.

Rows are per port (decided 2026-09-17 after seeing the table on real data): the port's newest open contribution leads, and its earlier contributions and standalone verifications fold underneath. A preparation that stops before creating a branch retires its contribution rather than leaving an empty open one.

The "next" derivation moves out of the CLI's progress formatting and beside this projection. The action results of `bump`, `verify`, and `publish` read a job's port label, change, and state words from the same projection (done 2026-09-19), so a summary line and the table never disagree about a job; the summary owns only its layout and the identifier-level detail the projection leaves out.

## The status table

`console` renders the projection as a live table with Bubble Tea, polling the snapshot every second or two; `status` keeps the plain snapshot. Rows are contributions, one per open contribution plus recently retired ones; the columns are port, versions, phase, state, next. Selecting a row expands its history and identifiers.

Keys map onto existing verbs and add no authority: wait, cancel, verify, publish, refresh, abandon, open the PR in the browser, show the log. Verify, publish, cancel, and abandon confirm before acting, since they cost minutes, push, or discard.

A message strip at the bottom shows progress reports for every running job, prefixed by port, at the selected level; each row also shows its contribution's latest message inline.

## Order of work

1. Levels and the envelope, since they change every command's plumbing once.
2. The per-command minimum, which is mostly deletion, plus the verdict words.
3. The contribution projection and its use by plain `status` and `--json`.
4. The table.

Each step lands with the friction list items it closes named in its activity report.
