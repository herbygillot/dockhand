# Managed contribution corrections

Implemented `amend`, `rebase`, and `reassociate` through the existing workflow lifecycle. No new package, dependency, schema migration, or separate progression loop was needed.

## Boundaries and behavior

- `git/changeset` constructs a single replacement contribution commit; `git` provides detached replay and guarded ref replacement. Rebase conflicts retain a disposable worktree with its path in the error. Successful temporary worktrees are removed.
- Workflow binds the target and evaluates the candidate outside state transactions. The accepted preparation intent records the original branch head, change/revision preconditions, and candidate source. Existing preparation claims/checkpoints integrate that candidate and append an immutable revision to the same change.
- An original head permits conditional adoption, the candidate head permits recovery, and any third head requires attention. Concurrent managed corrections, reassociation, and revision adoption are fenced while a correction is pending. Active jobs must settle before starting a correction.
- A checked-out amendment requires the intended contents already staged and matching the captured working tree. Dockhand locks and reads the index without rewriting it, and conditionally updates the branch. An occupied linked worktree is refused. Rebase should target a branch switched out of all worktrees. This first implementation deliberately does not reset working files or automatically stage tracked edits.
- `--diff` reads existing SQLite state without initializing a database, creates only private Git candidates, and neither moves branches nor accepts jobs. Rebase preview still fetches the intended upstream base.
- Matching Tart evidence can apply after same-tree amendments. Changed trees require another build. GitHub evidence remains tied to an exact commit and branch. Managed GitHub corrections carry an explicit expected prior remote commit; replacement uses the existing conditional push primitive.
- Reassociation validates a single contribution commit, scope, target, ownership, and current revision. It changes the local locator while preserving PR identity. Different source creates a revision rather than claiming existing evidence applies.
- Publication now records local and remote branch names separately. Existing PRs are observed by their recorded identity, preserve human body edits, reject unexpected remote heads, and keep the original remote head branch after local reassociation. A successful head update with unchanged metadata needs no extra PR API write.

## Validation

Real Git and SQLite regressions cover staged/unstaged amendments, linked worktrees, clean replay and retained conflicts, original/candidate/third-head recovery, pending correction fencing, changed-tree rebuild versus same-tree reuse, CLI amendment, and missing-state previews. Publication regressions cover existing PR identity/body preservation, remote divergence, and reassociation. GitHub provider regressions check that an amended push requires its exact recorded remote head. Existing stale-claim, lost-response, concurrent metadata, and complete-cohort publication tests remain in place.

Validation passed: `go test ./... -count=1`, focused race-enabled correction/Git/CLI/provider tests, `go vet ./...`, and `gmake build`. Live GitHub PR mutation was not needed for this pass: conditional pushes and PR behavior were exercised against isolated Git remotes and fixture forge responses.

All new code and comments were authored for this implementation; no v1 comments or tests were copied.
