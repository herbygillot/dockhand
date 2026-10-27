# Codex's language review, applied

2026-10-27, for rc6 at the person's word ("I want you to implement those
changes into rc6"): docs/reviews/2026-10-26-language-review-032836b8.md,
Codex's review of the text dockhand writes, at 032836b8. Its sections
§1–§7 and §7a are applied as wording; no behavior, JSON field, enum,
reason code, exit code, or parser marker changes.

- **Help (§1).** The root, setup tart, shell, explain, ship, submit,
  check, outdated, serve, and review help, and the short lines of serve,
  status, tidy, rebase, understand, run, queue, and clean, name the
  action first. The "Needs you" heading is "Needs attention", and a held
  ship or author says "needs review, so nothing was submitted/changed",
  then "After review: <command>".
- **Setup and authoring (§2), checks and status (§3), submission and
  tidy (§4), serve (§5), GitHub text (§6), destructive paths (§7).** The
  rows' proposed wording, in internal/command, internal/branchstate,
  internal/engine, internal/command/view, and internal/macports'
  pull request description, adjusted where the code said otherwise
  (below). A failed check's advice is "Fix the failure, submit a draft
  with --draft, or run dockhand check --baseline to see whether master
  fails in the same way"; someone else's push to the fork names
  `git merge`, not a reset.
- **README (§7a).** The release line names v0.6.0, the Intel line, the
  Local row, the limitations' introduction and bullets, and the explain
  line.

Where the review's wording didn't match the code, the code's fact won:

- review.go's behind-base note: the review called the commit reviewed
  from one that "predates the PR's base"; it is the commit before the pull
  request's own commits, and the text says so.
- branchflags' start line says "Created %s at this commit": StartAtHead
  creates the branch (SwitchNew), so "Created" is what happened.

Acceptance rows whose expected text changed: D-I7 and D-S6 ("running
here", was "runs here"), B2 ("After review: dockhand submit", was "Once
it's fine"), and G4 (view.ChangeWords' "stopped before it could restore",
as docs/local-provider.md now says it). Unit tests asserting the old
wording were updated; a few assert through cmdtest.Flat where the new
wording wraps.
