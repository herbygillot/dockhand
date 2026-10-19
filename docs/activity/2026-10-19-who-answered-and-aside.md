# Who answered, said once, and one aside name

Three more lines of the roadmap's v0.5.0 part 1 item 4, "One owner for
each rule", from the architecture review of 5897a82.

## R3-9's engine half: `ArchiveBranch` asks `AppliesUnasked`

"May this plan apply unasked" was spelled `HeldForLook`, `AppliesUnasked`,
and, in `ArchiveBranch`, a bare `!plan.Unambiguous()`.

- `ArchiveBranch --commit` asks `plan.AppliesUnasked(model.AnsweredNobody,
  false)`. A plan that something blocks now leaves the worktree with
  `NeedsWords`, as an ambiguous one did, where `ApplyTidy` refused it.
- The command's half of R3-9 is part 4.1's "one gate", as the roadmap
  places it.
- Grep for the old shape: `Unambiguous()` outside tidy.go finds none.

## R3-36: serve says who answered

`prepareOne` invented `ShipOptions.Answered` from `Origin`, nobody for
serve's.

- `PrepareOptions.Answered` is the caller's; serve sets
  `model.AnsweredNobody`, and `prepareOne` passes it on.
- Call sites switched: serve's `PrepareOptions` and the tests that build
  one.
- Test: `prepareOne` with a person's origin and nobody answering holds the
  major version, in `TestAMajorVersionHoldsAnUnattendedUpdate`.

## R3-13: one aside name

`RenameArchived` (`nameFree`, outside a transaction) and `renameAside`
(`tx.BranchNamed` with `Repo.Branch`, inside one) each chose
`<name>-archived`, `-2`, and on, and wrote their own rename event.

- `asideName(ctx, tx, old)` chooses it, within the transaction: free of a
  record, a Git branch, and a worktree directory. `recordAside` records the
  rename and why.
- `RenameArchived` now chooses, renames the Git branch, and records inside
  one `store.Recorded`.
- Grep for the old shape: `archivedName(old)` is read by `asideName` only.
- Test: `TestARecordSetAsideTakesTheFirstFreeName`.
