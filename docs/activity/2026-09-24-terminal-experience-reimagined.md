# 2026-09-24: the terminal experience, reimagined around changesets

The person asked for a complete reimagining of dockhand's command-line experience, with today's commands as optional input. It is built around the **changeset**: a branch whose commits together touch one or more ports and become one pull request. The request also named two capabilities:

- a server mode that works through a queue of changesets to build and test;
- help for new contributors in squashing stacked commits into the one-commit-per-change shape that MacPorts asks for.

The proposal is [`docs/reimagining/terminal-experience.md`](../reimagining/terminal-experience.md). It is design only; no code changed.

## What the proposal says

- **Four stages.** Author (`update`, `revbump`, `checksums`, `create`, `edit`, `save`), shape (`check`, `tidy`, `undo`), test (`test`, `logs`, `watch`), and submit (`submit`, `sync`).
- **Authoring stops early.** It stops after the local checks, and `--submit` restores today's one-line bump-to-PR.
- **One commit per logical change, by construction.** `save` folds edits into their port's commit, so the stack a newcomer usually builds can't form. `tidy` repairs a branch that already has one: it splits by port, folds follow-ups, orders by dependency, and never changes the final tree. `undo` reverts it.
- **Sparse worktrees.** Each changeset dockhand makes gets one, so several can be open at once without touching the main checkout. Branches the person made stay where they are.
- **One driver.** An on-demand server drives the queue, and every other command is a client, so detaching is always safe. The server can run at login, on remote Macs over SSH, and optionally follows PRs, retests changesets, and tests others' PRs on your ports.
- **`--on` names where builds run.** It replaces `--provider`, `--image`, and `--os`, and `--else` gives an explicit fallback.
- **The multi-port journey.** A library update, `revbump --dependents-of`, builds in dependency order, and a baseline build to tell a new failure from a pre-existing one.
- **Reference sections.** Selectors, the flag vocabulary, the rules `check` applies, the subjects dockhand writes, the PR body under the template's own headings, output conventions, configuration, the promises, the engine changes each part needs (mapped to the contracts review's sections), a table from today's commands, and six open questions.

## What informed it

- **The UX lessons already recorded.** These are the deno exercise, the messaging scan, `output.md`, and the activity notes on naming, levels, refusals, and the console. A read-only pass summarized them.
  - Kept: identifiers only at `-v`, one phrasebook, the JSON envelope and exit codes, refusals that name the way out, kept PR bodies, and the reason as a rebuild's subject.
  - Addressed:
    - branch names carrying job IDs;
    - `--all` meaning three things;
    - mixed JSON key casing;
    - the console driving work as soon as it opens;
    - the amend staging question;
    - having to switch away from a branch to rebase it;
    - summaries repeating the milestones.
- **MacPorts' own sources, read from GitHub.** The egress proxy refused guide.macports.org and trac.macports.org, so the guide's DocBook source, the PR template, the CI workflow, mpbb, the mpbot PR webhook, and MacPorts Base's ChangeLog and man pages were read from GitHub instead. Findings the proposal relies on:
  - The guide asks for commits minimized, "ideally with one commit per logical change", and for review follow-ups to be squashed. It does not require one commit per dependent's rebuild. The contracts review's §3.1 says the guide "asks for" separate commits there, which overstates it; the proposal accepts either shape.
  - The template's headings and checklist, verbatim. The bot detects "update" from the title and "submission" from a new Portfile, and `[skip notification]` silences maintainer mentions. Today's PR body omits the template's Type(s) section.
  - MacPorts CI runs `port lint` without `--nitpick`, builds default variants with no trace mode on macOS 14, 15, and 26, and runs only the built-in test phase.
  - `port bump` has refreshed checksums and reset the revision after a hand edit since MacPorts 2.6.0.
  - Trace mode on arm64 macOS 15 and later is fixed only in the unreleased 2.13.0.
  - `dist_subdir ${name}/${version}_1` is the guide's recipe for a stealth update.
  - Maintained ports have a 72-hour window before others may merge.
- **Name plausibility.** The mock sessions use real port names, with versions, pull request numbers, and durations marked as illustrative. The poppler dependents were limited to ports that link it.

## Not done

- The roadmap is unchanged; whether and how to adopt any of this is the person's decision.
- The open questions in §11 of the proposal are for the person: how far the server reaches, publishing from a remote, worktrees or branches, whether `update` stops early, `check --comment`, and "changeset" as the on-screen word.

## Decisions, later the same day

The person answered four of the open questions, and the proposal was revised to match. Its §11 now records the decisions.

- **The server stays on the person's own Mac.** Dockhand is for an individual maintainer working through the ports they maintain.
  - Removed: remote build servers (`remote add`, `--on mini:…`, `remote trust --publish`, the SSH protocol row in the engine table).
  - `--on` now names only this Mac's images and `github`.
  - The server section says the server serves one person and opens no network port.
  - Testing others' PRs on your ports stays opt-in.
- **Sparse worktrees, placed where people can reach them.** The default moved from `~/.dockhand/changesets` to a visible `~/src/macports-changesets` beside the clone, which `init` offers. The proposal says the worktrees are meant to be opened and edited directly.
- **`update` stops after the local checks, with a standing way through.**
  - `update.then = stop | test | submit` sets how far a passing changeset goes by itself; `--stop`, `--test`, and `--submit` override it for one run.
  - `server.updates = list | draft | submit` sets what the daily release check does. At `submit`, the server opens at most `submit.daily-limit` PRs a day, so a release wave doesn't swamp reviewers.
  - Journey 5.12 was rewritten as "Working through your ports": one batch, the passing changesets going through to PRs, and only the failures left on the list.
- **Reviewing others' PRs is allowed.**
  - `check --comment` posts the findings as a comment.
  - `check --review` posts them as a GitHub review, with inline findings and the fix given in both dockhand and plain Git terms.
  - Requesting changes is offered when the account has write or triage access to macports/macports-ports.
  - A server rule may post reviews on PRs to your ports, at most one per head, and never requesting changes by itself.

Left open: whether "passes" includes the local build (assumed yes; `then = "submit-untested"` would be the other reading), and whether "changeset" is the on-screen word.
