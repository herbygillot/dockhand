# Dockhand reimagined: a terminal workspace for MacPorts contributions

2026-09-24. Independent product proposal, not an accepted contract or an
implementation plan. Prepared after reading the contracts review, its direction
record, the changeset roadmap review, and the earlier new-user exercise.

The requested audience is occasional contributors and experienced maintainers,
served through progressive disclosure. Managed working directories and existing
Git checkouts are equally supported. All commands and output below are proposed.
Versions, run identifiers, results, and dependency examples are illustrative.

## 1. The experience I would build

**Make a contribution, work on it freely, check it as often as useful, and shape
it for review when ready.**

The primary object is a named changeset: an ordinary branch, its intended base,
and its relationship to a pull request. Its ports come from the diff. It can
contain one new port, an update, a patch fix, a PortGroup change, or a coordinated
change across many ports. The branch's commit series expresses the contributor's
intent; the file tree determines what is built.

Uncommitted edits are work toward that changeset. Dockhand can capture them for a
check without creating a public commit. A passing check is attached to what was
captured, so subsequent editing does not turn an old result into a current pass.

The visible rhythm is:

```text
start → edit → check → tidy → submit
          ↑       │       │       │
          └───────┴───────┴───────┘
```

These are activities, not mandatory lifecycle stages. Someone can adopt an
existing branch, check before committing, keep an already good commit series,
submit a draft, or return from review to editing. A contributor should not need
to manufacture a commit just to find out whether their unfinished Portfile
builds.

I would make authoring commands stop after authoring. `update` changes a port;
`check` builds and tests; `submit` opens or updates a PR. Common combinations are
available explicitly. Their meanings stay the same at every level of experience.

## 2. Context should be obvious

### Starting in a managed directory

```console
$ cd ~/Source/macports-ports
$ dockhand init
Using this ports repository.
Upstream: macports/macports-ports
Authoring is ready. Run dockhand setup when you want to configure builds.

$ dockhand start jq-update
Created changeset jq-update
Branch: dockhand/jq-update
Base: macports/master, fetched just now
Directory: ~/.dockhand/worktrees/macports-ports/jq-update

Next: cd "$(dockhand path jq-update)"

$ cd "$(dockhand path jq-update)"
```

`init` registers an existing ports checkout, checks that it really is one, and
explains what is available. It should not require GitHub login, a fork, or a
provisioned builder to permit local work. With no checkout, `dockhand init
~/Source/macports-ports` offers to clone the official tree. Authentication and
fork creation are introduced when publication needs them.

Managed directories are normal Git worktrees. They are useful for many concurrent
changesets and for people who want Dockhand to handle branch creation. The printed
directory is real and can be opened in any editor. `path` prints only the path;
Dockhand does not pretend it can change its parent shell's directory.

### Starting in an existing checkout

```sh
# Create a contribution branch in this checkout.
dockhand start jq-update --here

# Or keep the branch, commits, and edits already here.
git switch my-existing-branch
dockhand adopt
```

Adoption associates an existing branch with Dockhand; it does not require moving
the work or rewriting it. Both modes get identical editing, checking, tidying,
and submission commands. `start --here` checks for work that would be displaced
and explains how to preserve it; it never silently stashes or resets it.

### Selection rules

1. An explicit `--change <name-or-id>` selects a changeset.
2. Otherwise, the current worktree and its checked-out branch supply context.
3. Without that context, a mutation asks for a selection in an interactive
   terminal, or returns an actionable error in a script.

The first line of every mutation names the selected changeset and directory.
There is no persistent global "current changeset" shared between terminals.
Checking out a different branch changes the context. A branch rename can retain
the changeset identity after reconciliation.

A port name is an editing or filtering target, never an implicit choice of
changeset. Two changesets may touch `jq`; neither silently captures the other's
next edit. `dockhand status --port jq` finds them. From anywhere in the repository,
`dockhand update jq --change jq-update` chooses one deliberately.

For convenience, an authoring helper accepts `--new <name>`. For example,
`dockhand update jq --new jq-update` means create a managed changeset and update
the port there. It prints the directory and has the same behavior as the two
separate commands. It does not search for an existing contribution to continue.

## 3. The ordinary update

```console
$ dockhand update jq 1.8.1
jq-update · ~/.dockhand/worktrees/macports-ports/jq-update

jq: 1.8.0 → 1.8.1
Source: upstream tag jq-1.8.1
Updated version and checksums; revision reset to 0.

Changed: textproc/jq/Portfile
Next: dockhand diff

$ dockhand diff
jq-update · 1 changed port
... ordinary unified diff ...

$ dockhand check
jq-update · captured working files as snapshot 3
Environment: macOS 26, arm64, Command Line Tools
Coverage: jq, default variants

  jq    lint ✓   fetch/checksum ✓   build/install ✓   tests ✓

Passed for snapshot 3 on this environment.
Next: dockhand tidy

$ dockhand tidy
Proposed commit:
  jq: update to 1.8.1

Includes: textproc/jq/Portfile
Review diff [d] · Edit message [e] · Apply [a] · Cancel [q]
> a

Created 1 commit. Recovery checkpoint: tidy-12
File contents are unchanged. Check results still apply.

$ dockhand submit
jq-update · ready to submit

Title: jq: update to 1.8.1
From: your-fork:dockhand/jq-update
To: macports/macports-ports:master
Commits: 1
Checks: passed on macOS 26 arm64; other environments not checked

Preview pull request [p] · Edit description [e] · Submit [s] · Cancel [q]
> s

Opened pull request #<number>
```

Omitting the version asks release discovery to resolve one. The output names
the source it consulted and the chosen release or tag. A tag without downloadable
assets is described as such. If the editor cannot safely change a Portfile, it
explains the unsupported expression and opens a path to manual editing; the
changeset remains useful.

An expert who wants the combined operation can write:

```sh
dockhand update jq 1.8.1 --new jq-update
dockhand submit --change jq-update --tidy --check
```

`--tidy` reviews and applies a commit plan first. `--check` obtains missing
coverage next. The final submission preview shows the exact commits and results.
A plain `submit` does neither implicitly. A script supplies explicit commit
grouping, messages, and approval flags; `--yes` cannot resolve an ambiguous plan.

This costs more visible steps than a one-command bump by default. That is a
deliberate tradeoff: a routine update and a complex contribution have the same
understandable operations. Shortcuts compose those operations.

## 4. The other authoring tasks

### A new port

```sh
dockhand start seaglass
cd "$(dockhand path seaglass)"
dockhand new seaglass --category devel
dockhand edit seaglass
git add devel/seaglass
dockhand check
dockhand tidy
dockhand submit
```

`new` asks for the source release, description, license, and build system where
they are unknown. It can suggest a PortGroup or use an available generator, but
shows the resulting Portfile and unresolved fields. It never presents guessed
license or maintainer information as fact. Advanced users provide the same inputs
as flags and skip the questions.

The scaffold can remain incomplete while being edited. Checks explain what is
missing. New files must be deliberately included with `git add` or the explicit
snapshot inclusion option described below. The scaffold command prints that next
step. Build support should not depend on an automatic scaffolder understanding
the port: a handwritten new Portfile is a first-class path.

### Checksums, revisions, and other edits

```sh
dockhand checksums jq
dockhand revbump harbor-cli harbor-viewer --reason "rebuild for libharbor 2"
dockhand edit jq
```

These operate within the selected changeset. `checksums` refreshes the selected
source and displays the archive URL, old/new digests, and affected files. If the
source changed without a version change, it calls that out and invites source
inspection; it does not silently decide that a revision bump is unnecessary.
`revbump` changes revision declarations and records the supplied reason for later
commit-message suggestions. When a shared declaration affects sibling subports,
the preview names them.

`edit` is a convenience for opening the relevant files in `$EDITOR`. Any other
editor or Git command works equally well. There is no privileged "Dockhand edit"
that must be used for the tool to notice changes.

All authoring helpers support `--plan` to show intended edits before applying
them. Fetching, evaluation, and temporary downloads needed for a plan are stated;
the promise is no changes to the user's branch, working files, or forge. Expensive
or executable preparation is identified before it begins. Successfully computing
checksums does not imply that the source has passed a build.

## 5. A coordinated changeset across ports

The following library and consumers are fictional examples.

```sh
dockhand start libharbor-2
cd "$(dockhand path libharbor-2)"
dockhand update libharbor 2.0
dockhand impact libharbor
dockhand revbump harbor-cli harbor-viewer --reason "rebuild for libharbor 2"
dockhand check --also harbor-tools
dockhand tidy
dockhand submit
```

`impact` explains potential effects: changed directories, expanded subports,
consumers found through dependency metadata, and affected shared files. Dependency
lists are candidates for inspection, not proof of an ABI break or an instruction
to revision-bump everyone. An optional `revbump --dependents-of libharbor` shows
the expanded list before editing; scripts can select an explicit list.

```console
$ dockhand status
libharbor-2 · 3 changed ports · 5 working commits

Changed
  libharbor       1.9 → 2.0
  harbor-cli      revision 2 → 3
  harbor-viewer   revision 0 → 1

Checks · snapshot 8 · macOS 26 arm64
  libharbor       passed
  harbor-cli      passed, using the changed libharbor
  harbor-viewer   failed during build
  harbor-tools    passed · additional coverage, no source edits

Submission: needs attention
Next: dockhand logs check-42 --port harbor-viewer
```

There is no primary port with secondary ports attached to it. Revision-bumped
ports are changed ports. Additional check targets remain additional coverage.
A `--only harbor-cli` check automatically includes the changed `libharbor` as a
prerequisite, displays that expansion, and cannot substitute an old archive.

PortGroup-only changes are also changesets. When Dockhand cannot establish useful
coverage automatically, `check` asks for explicit representative ports through
`--also`. The report retains the unknown impact; one successful representative
build does not establish that every consumer works. Renames, deleted ports, and
changes to support files remain visible even if they have no directly buildable
target. Removal checks can inspect references and selected consumers.

## 6. Tidy the contribution, not every work-in-progress commit

The MacPorts guide recommends minimizing commits around logical changes, while
allowing multiple commits for work that needs them. It also asks contributors to
fold review corrections into their changes. That is a semantic goal, not a
one-directory arithmetic rule. [MacPorts contribution workflow](https://guide.macports.org/chunked/project.github.html)

`tidy` is the main teaching feature. It answers: **What should a reviewer see as
the individual changes in this branch?**

```console
$ dockhand tidy
libharbor-2 · 7 commits and working edits

Suggested series
  1  libharbor: update to 2.0
       combines the update and its two corrections
  2  harbor-cli: rebuild for libharbor 2
       combines the revision bump and its correction
  3  harbor-viewer: rebuild for libharbor 2
       combines the revision bump and its correction

Review patches [d] · Change groups [g] · Edit messages [e]
Apply [a] · Keep existing history [k] · Cancel [q]
```

A single-port update with six corrections would normally become one commit. A
new port and a prerequisite port would normally keep two. A bug fix and an
unrelated maintainer change in the same Portfile can remain two. One coordinated
change spanning a PortGroup and several ports can be one commit if that is the
author's intended review unit. Grouping by port is a useful suggestion, never a
required partition.

Authoring helpers contribute hints about intent. Commit messages and file paths
contribute other hints. None proves that two edits are the same logical change.
Mixed commits or overlapping hunks require a reviewed plan; Dockhand should admit
uncertainty rather than invent a polished but wrong series. Existing well-shaped
history should pass through unchanged.

The simple explicit escape hatch is:

```sh
dockhand tidy --squash --message "jq: update to 1.8.1"
```

This means squash the entire contribution. It must say which commits and edits
are included. It must not be the automatic response to every multi-port branch.

For advanced or scripted work, `tidy --plan --out <plan-file>` saves a reviewable
plan and `tidy --apply <plan-file>` applies it. The plan binds to the base, branch tip, and
captured working state. If any changed, it is stale and needs regeneration.

Required behavior:

- Preview the resulting commits and patches, including the final combined diff.
- Preserve the final tree exactly for a history-only operation. Build evidence
  remains reusable only when its other inputs still match. History-sensitive
  checks and forge statuses retain their actual identities.
- Preserve human authorship and existing relevant trailers. A combined commit
  with multiple human authors needs an explicit attribution choice.
- Preserve the old refs and any captured working/index state in a named recovery
  checkpoint before applying. `dockhand restore tidy-12` restores locally only
  after checking for newer work; it cannot silently overwrite intervening edits.
- Never rewrite a merge topology merely to run a check. If tidying a branch with
  merges would flatten it, require a specific reviewed choice and preserve a
  checkpoint; checking the final tree does not need that choice.

After a PR exists, local tidying is still local. `submit` explains the necessary
remote history replacement and checks the remote head against the last observed
commit. Someone else's intervening push causes a stop with a useful comparison,
not an unconditional force push.

## 7. Check the files the contributor means to check

Within the active changeset worktree, `check` captures the final working-file
contents of tracked files, including staged additions and tracked deletions.
The staged-versus-unstaged boundary does not select only half an edit by accident.
`--staged` checks the index instead; `--head` checks the committed branch tip.
These options also apply to `tidy` when selecting material to include. With
`tidy --staged`, the index supplies the intended committed tree and unstaged
working edits remain outside the resulting commits.

Untracked files are listed and excluded by default. `--include <path>` explicitly
adds a file or directory to the capture without staging it. An untracked patch
referenced by a changed Portfile must trigger an actionable missing-input error,
not a misleading successful capture. Ignored generated files remain excluded
unless explicitly selected and shown in the inclusion list.

For `--change X` outside X's active worktree, the default is its committed head.
If its worktree has edits, Dockhand requires `--head` or `--working-tree` instead
of quietly choosing. Unresolved merge conflicts prevent capture. Capture must
detect concurrent changes and retry or stop instead of producing a mixture of
two saves. Every accepted run pins its source, base, targets, and configuration.

```console
$ dockhand check --plan
Source: working files in libharbor-2
Changed ports: libharbor, harbor-cli, harbor-viewer
Additional coverage: none
Environment: macOS 26 arm64, default variants
Order: libharbor → harbor-cli, harbor-viewer
Excluded subport: harbor-viewer-legacy; unsupported on macOS 26
...
```

`check` encompasses lint, source fetch/checksum, build/install, and declared
upstream tests. `check --lint` is an inexpensive partial check. A test suite not
provided, skipped, failed, or passed has its own explicit result. An advisory
test failure is displayed as "build passed; tests failed (advisory)". It is not
compressed to an unqualified pass. `--tests required` makes that failure decisive
for the selected profile.

Different questions can be asked without altering the source:

```sh
dockhand check --only harbor-cli
dockhand check --also harbor-tools
dockhand check --on macos-26-arm64 --on macos-15-arm64
dockhand check --only libharbor --variants +universal
```

Environment names resolve to configured builders with explicit OS, architecture,
toolchain, and MacPorts versions. They are not promises that every provider can
run every configuration. Unsupported requests fail during planning with a
specific capability explanation. `--variants` in this example applies to the
one selected target; mixed per-port variants require a saved coverage profile.

The report distinguishes changed scope, selected coverage, prerequisites,
exclusions, additional coverage, and unresolved evaluation. "Could not evaluate"
never becomes "not applicable". A focused `--only` run does not silently shrink
the changeset's submission requirements.

On failure, independent targets continue. Targets that need a failed changed
dependency become blocked. Interrupted work, infrastructure trouble, and a failed
port build are distinct outcomes. A retry reuses only applicable complete results
and the exact available dependency artifacts they require.

```sh
dockhand logs check-42 --port harbor-viewer
dockhand retry check-42
dockhand check --baseline --only harbor-viewer
```

`retry` repeats the pinned request; it does not test newer edits. `check` captures
newer edits. A baseline compares the relevant target against the recorded base
under the same configuration. It reports what happened in each run and does not
automatically claim that matching failure phases establish the same cause.

## 8. Server mode should make execution predictable

```sh
# In a dedicated terminal, or started by an explicitly installed user service.
dockhand serve --jobs 2

# In authoring terminals.
dockhand check --change libharbor-2 --enqueue
dockhand check --change jq-update --enqueue
dockhand queue
dockhand wait check-42
```

```text
RUN        CHANGE        SOURCE       STATE       DETAIL
check-42   libharbor-2    snapshot 8   running     harbor-viewer: build
check-43   jq-update      snapshot 3   queued      waiting for a builder

Worker: running · 1 of 2 execution slots available
jq-update needs an environment currently occupied by check-42.
```

The queue holds immutable check requests, displayed by changeset. Editing the
branch after enqueueing cannot change what an accepted request means. A completed
older run says "passed for snapshot 8; current work has changed". Cancellation
targets a run; it does not abandon the changeset or close its PR.

The process rules are part of the user experience:

| Situation | Behavior |
| --- | --- |
| `check`, service available | Submit a request and follow it in this terminal. |
| `check`, no service | Execute this request in the foreground using the same runner. |
| `check --enqueue`, no worker | Save the request; say "queued, no worker running" and print `dockhand serve`. |
| `wait <run>` | Observe that exact run. Attaching does not start a worker. |
| Ctrl-C while following service work | Detach; the service keeps running. |
| Ctrl-C during foreground-owned work | Explain the stop, interrupt safely, retain results, and mark unfinished work resumable. |
| Worker restart or lost connection | Reconcile active provider work before retrying; retain completed target results. |

An external provider may continue after a local interruption. Status says so,
and recovery reconnects instead of submitting a duplicate. Durability is never
presented as evidence that a process is still executing.

`dockhand serve --drain` processes the finite set queued at invocation and exits
after those requests settle. `dockhand service install` is an explicit opt-in to
automatic service startup. Neither is required to use the foreground workflow.

`queue pause` stops new admissions while running checks finish; `queue resume`
re-enables admission; `cancel <run>` stops a specific request. A missing builder
for one request should not prevent compatible work from progressing. Scheduling
is fair across changesets and respects actual resource limits.

The first server feature should execute checks and record results. If unattended
submission is added, it must be a separate explicit request binding the reviewed
source, remote destination, PR content, and completion conditions. A service
noticing green checks is insufficient authorization to publish later edits.

Remote workers are a natural later extension. The CLI contract can accommodate
them, but the first useful server does not need a distributed scheduler.

## 9. Submission and the review loop

`submit` opens or updates the one associated PR. It previews the commits, head
and base repositories, title, description, and coverage. A multi-port changeset
asks for a meaningful title when one has not been supplied. The title becomes
changeset metadata and is not silently replaced by whichever port was edited last.

The normal submission profile asks for the changed eligible targets on the
configured default environment. A changeset can deliberately choose a different
profile; its coverage is visible before checking and submitting. Focused runs
provide evidence toward that profile. A shared-resource change with unknown
scope needs an explicit coverage decision and an honest description of its limit.

There are three useful paths:

- `submit`: require applicable coverage for the chosen profile and committed
  source, then show the PR preview.
- `submit --draft`: allow unfinished checks and show the missing or failing
  results in a draft PR. The source must still be committed.
- `submit --allow-incomplete --reason "..."`: permit an informed ready-PR
  submission with listed failures or missing coverage. The preview requires
  acknowledgment of those exact results for that exact source and the description
  records them. New edits or newly discovered failures require a new decision.

This is a recommendation to make publication policy a visible contributor
decision. It departs from treating selected categories of failures as absolute
publication prohibitions. The tool's strongest obligation is an accurate report.
An inability to identify the source or destination is a different issue: it must
be resolved before any publication can occur.

If the worktree has edits, `submit` shows what would be omitted and asks for an
explicit `--head` or `--tidy`. It never takes passing working-tree evidence as
permission to push a different committed tree. A history-only tidy can reuse
applicable content evidence; changed build inputs require renewed evidence.

Draft publication can be useful before a builder is available. A provider that
needs a remote push for checking must preview that push when accepting the check
request, even if no PR will be opened. "Only checking" cannot conceal a write to
the contributor's fork.

For review corrections:

```sh
dockhand edit jq
dockhand check
dockhand tidy
dockhand submit
```

Dockhand recognizes the same contribution and updates its PR. The last command
shows whether it will append commits or replace remote history. Updates preserve
human-edited title and description text; refreshed generated coverage belongs in
an identifiable managed section and cannot overwrite a concurrent human edit.

Other useful entry points are `dockhand adopt --pr <url>` to work on an existing
PR and `dockhand rebase` to incorporate current upstream. Adoption alone permits
inspection and local work, not an assumption of permission to push to another
person's branch. Submission names that destination and checks actual access.
Rebase previews the new base, keeps a recovery checkpoint, and provides normal
continue/abort guidance when conflicts arise. Evidence is reassessed afterward.

`status --refresh` observes review, CI, and merge state without editing or
publishing. After merge, the changeset is recorded as merged and remains searchable.
`clean --merged` previews removal of eligible managed worktrees and branches;
dirty directories and work that advanced beyond the merged source are preserved.
`archive` hides inactive work without changing the PR. `cancel` stops a run.
Those three operations should never be synonyms.

## 10. The terminal should answer what to do next

In a changeset directory, bare `dockhand` prints its status. At a repository root,
it shows the changeset list and, on first use, a few getting-started examples.
`dockhand status --all` always shows the list.

```text
CHANGE          PORTS  WORK               CHECKS                 PR
jq-update           1  edits after check  passed for older work  #<n> changes requested
libharbor-2          3  5 commits          1 failed, 3 passed     —
seaglass            1  draft files        not checked            —
```

There is no single "done" or "verified" field trying to encode source state,
build activity, coverage, and review outcome. Each column answers one question.
Detailed status shows observation times so cached forge results do not look live.

`dockhand watch` is the optional live view of the same information. It can expose
keyboard actions for check, logs, tidy, and submit, each visibly using the same
command semantics. It observes work; the server executes it. Every operation is
available without the live view, and ordinary output remains useful over SSH,
in scrollback, and to screen readers.

Errors should name the requested outcome, the actual obstacle, the work retained,
and one concrete next action:

```text
harbor-viewer did not build against libharbor 2.0.
The failure occurred while compiling harbor-viewer; libharbor built successfully.

Your files and the other three results are retained.
Inspect: dockhand logs check-42 --port harbor-viewer
```

Normal output uses phase summaries and a short relevant failure excerpt. Full
tool output is available through `logs`; debug detail is opt-in. Source digests,
internal record IDs, and lease terminology belong in detailed output, not the
new contributor's next-step message. Run IDs appear where they are useful for
following or retrying work.

First-run costs must be visible before they are incurred: downloading an image,
building an index, discovering releases, or preparing tools. Setup reports
capabilities separately: authoring, evaluation, local checking, remote checking,
and publication. A machine that cannot run a requested builder can still do the
work it supports; cross-platform evaluation is offered only where its semantics
are actually implemented.

## 11. Small command families, progressively disclosed

Top-level help should teach the short loop first. It should not present every
maintenance control with equal prominence.

| Purpose | Main commands |
| --- | --- |
| Enter work | `init`, `start`, `adopt`, `path` |
| Author | `new`, `update`, `checksums`, `revbump`, `edit` |
| Understand | `status`, `diff`, `impact` |
| Check | `check`, `logs`, `retry` |
| Prepare review | `tidy`, `submit`, `rebase` |
| Keep work moving | `queue`, `wait`, `cancel`, `watch`, `serve` |
| Occasional administration | `setup`, `builders`, `auth`, `service`, `restore`, `archive`, `clean` |

Discovery such as `outdated --maintainer me` remains useful outside any
changeset. Its results can feed multiple explicit `update --new` requests. Several
unrelated updates can share a check queue without being combined into one branch
or PR. Batch authoring should preview that partition before creating work.

Automation uses the same commands, with consistent `--change`, `--plan`, and
structured output. No terminal means no prompts: missing required choices produce
an error that identifies the relevant arguments. `--json` returns one versioned
result envelope; progress goes to stderr. An explicitly requested event stream
uses JSON Lines. JSON output never implicitly approves a mutation.

Mutations recheck their expected source and destination before applying their
effects. Another terminal changing the branch, index, or affected working files
invalidates the relevant plan. An enqueue operation captures inputs before it
returns success; a worker never resolves "whatever the branch contains now."

Exit success for `--enqueue` means the request was saved, not that a check passed.
`wait` and foreground `check` return a documented outcome for success, check
failure, interruption, or attention needed. Result data also preserves individual
test failures and policy decisions; the exit code is not the whole report.

## 12. The deliberate departures

The most consequential differences from the existing direction are behavioral:

1. Changeset context comes from the branch/worktree or an explicit selection,
   rather than inferring continuation from the port and verb.
2. Authoring edits files; committing is an explicit Git or `tidy` operation.
   Helpers do not repeatedly choose which existing commit to amend.
3. Checking unfinished working files is a normal path. Clean contribution history
   is a review concern, not an admission requirement for useful checks.
4. Commit cleanup is guided by logical change, with per-port grouping only a
   suggestion. Multiple commits touching one directory can be correct.
5. Named coverage and evidence replace an undifferentiated "verified" promise.
6. The server has an explicit execution role, with honest foreground interruption
   and unstaffed-queue behavior.
7. Publication policy permits a documented contributor override and draft work;
   source identity and truthful coverage remain firm requirements.
8. Git remains usable without Dockhand, and both workspace styles have equal
   capabilities. Durable Dockhand records enrich branches rather than owning them.

The names are negotiable. These defaults are the design: they determine whether
Dockhand accommodates real contributions or asks contributors to arrange their
work around the tool.

## 13. How to judge the proposal before building it

Walk through these tasks with terminal prototypes and ask contributors to predict
what each command will do before running it:

- A first-time contributor creates a handwritten new port and checks it before
  its first commit.
- A maintainer performs a routine update using the explicit combined submission.
- Two terminals edit separate changesets touching the same port without ambiguity.
- A library update and two consumers share a branch and expose a partial failure.
- Six correction commits become one logical update, while two distinct changes
  to one Portfile remain separate commits.
- Work changes after enqueueing; the older result never appears current.
- A check is interrupted with and without a service, and the user can explain
  what is still running and how to resume.
- A maintainer pushes to the PR while its author is tidying; the author's next
  submission preserves that intervening work.
- A PortGroup-only contribution chooses representative coverage and reports its
  limits accurately.

The first implementation slice should prove an existing two-port branch can be
adopted, checked from a snapshot, reported clearly, and submitted with honest
coverage. A basic one-group tidy and recoverable queued checking follow. Sophisticated
commit regrouping and fine-grained build reuse can grow after those paths work.
Neither needs to be a prerequisite for making changesets useful.

## Reading that informed this proposal

- [Contracts review](2026-09-23-contracts-review.md): accidental product bounds.
- [Direction record](2026-09-23-contracts-direction.md), especially decisions
  22–27 and 44: changesets, continuation, commit ownership, and coverage.
- [Changeset roadmap review](2026-09-24-changeset-roadmap-review.md): source
  identity, target results, dependency artifacts, and failure attribution.
- [New-user exercise](2026-09-16-new-user-deno-exercise.md): context, startup cost,
  error messages, output, and remote selection.
- [MacPorts Base review](2026-09-24-macports-base-independent-review.md): limits
  of modeled environments and the importance of capability claims.
- [MacPorts contribution workflow](https://guide.macports.org/chunked/project.github.html):
  the distinction between minimal history and one commit per logical change.

Only this proposal document was added. No command described here was implemented
or executed against a ports checkout, build service, or forge.
