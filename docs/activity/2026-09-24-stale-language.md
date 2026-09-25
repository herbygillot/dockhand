# 2026-09-24: design documents brought up to what the code does

The contracts review of 2026-09-23 found prose contracts the code no
longer keeps and left the rest for a later pass. This is that pass: each
sentence below was checked against the code before it was changed. No
behavior changed.

## Phases that already happened

`architecture.md`, `cli-design.md`, `components.md`, and `state.md` still
described PR monitoring, `outdated`, `rebase`, `amend`, reassociation, and
dependent verification as phase-two or later work. All are implemented:
repository-wide cycles, the ones `serve` runs, follow open pull requests
whose next look is due and settle owed cleanups (`workflow/cycle.go`,
`observePullRequests`), and `sync` does so once; `outdated`, `rebase`,
`amend`, and `reassociate` are commands; `--dependents` verifies a root's
direct dependents. The phase headings became plain ones ("Extension
boundaries", "Discovery and follow-up workflows"), and what is still not
implemented is said so: scheduled discovery, retained discovery
observations, the identity-only notes namespace, interactive conflict
resolution, automated review responses and merging. `cli-design.md` also
claimed some command handlers "still return explicit not-implemented
errors"; none do. The CLI's `errNotImplemented` survives only in a test
asserting a command does not return it, a leftover to remove with the
next CLI change.

## Retention the architecture misdescribed

`architecture.md` said the first cycle retains failed environments
indefinitely and that ordinary cycles apply no age policy. The code
releases failures unless `--keep-failed` was recorded
(`workflow/verification_record.go`), and cycles prune up to eight
released diagnostic directories once both job and release are seven days
old (`workflow/retention_cycle.go`), as `operations.md` already said.

## A dry run and the database

`resolution-design.md` still named `--dry-run --adopt` as the one dry run
that creates the database. That was fixed on 2026-09-22: the adopt dry run
builds for reading (`cli/adopt.go`, `app.BuildForReading`) and the CLI
tests assert no database appears
([note](2026-09-22-smaller-items-triaged.md)). The rule now reads without
exception.

## One open contribution per port

`resolution-design.md` stated "one open contribution per port" flatly,
where `target-workflow.md` says duplicates must stay representable with
ambiguity surfaced. Both are true at different layers, and the sentence
now says which: dockhand creates at most one (a bump continues it, `adopt`
refuses a second branch); the store does not enforce it; selection names
duplicates and asks for `--change` or `--branch`
(`workflow/contribution_select.go`).

## The driver prohibitions are decisions

`principles.md` and `architecture.md` wrote "commands do not spawn
background drivers", "no Unix socket or separate local request transport",
and "no process registry, singleton residency lock, child driver, socket,
or operating-system driver service" in the voice of principles. They are
decisions of the first implementation, as the review argued (its 3.4), so
each now says so, gives its reason, and points to the
[coordination note](../instance-coordination.md) for what would reopen it.
The rules themselves are unchanged.
