# Terraform's replay: two fixes for main

These are field testing's terraform replay notes at f6d5281c, which the
roadmap placed as fixes for main (b2365034).

## A plan says which subports a branch changes, and its exclusions a line per reason

Notes 1 and 3. Branch terraform-1.16-1.16.7's diff is inside
`subport terraform-1.16 {` alone. Its plan said "Changed 19 ports" and
printed 25 Excluded lines.

- **`Engine.PlanSiblings`** (engine/changes.go) gives a plan's changed
  targets that their directory's change record finds unchanged. Its pure
  half, `planSiblings`, is tested on the multi-subport fixture. A
  directory with no record, or one that keeps its text scope, has none.
- **`view.WritePlan`** takes the siblings. The Changed line names the
  changed subports. The rest follow on a line of their own: "Siblings
  terraform_select, terraform-1.0 … and N more, built as MacPorts' CI
  builds every subport of a changed Portfile; --only narrows". The check
  still builds every subport, as decided, and the JSON's roles stay.
  check's and ship's previews pass the engine's siblings; a baseline's
  plan passes none.
- **`model.Plan.GroupedExclusions`** groups by reason and by the
  environments that say it, across ports (`GroupedExclusion.Targets`),
  where it grouped by port. The Excluded line names its ports as the
  Changed line names them.
- **Each reason reads for one port or many**:
  - "minmax builds each port on its oldest and newest releases, …"
  - "platforms … exclude this release"
  - "known_fail here, as the Portfile says"
  - "check-N didn't fail there" and "check-N didn't build there"

## A subport's result stands across an edit inside another subport's block

Note 2. Any edit to terraform's Portfile changed the directory's tree for
every subport, so each patch bump of terraform-1.16 built its 18 siblings
again, though none runs a line that changed: MacPorts runs a `subport X
{ … }` block's body only when evaluating X.

- **`portfile.BlockChanges(before, after)`** gives the subports whose
  literal blocks hold every changed line, with `UnseenChanges`'
  attribution applied to every change. A block nested in an `if` counts.
  It returns all where a change is outside every block or a version
  doesn't parse.
- **`reuse.Current`** takes a `reuse.Blocks` reading. Where a directory's
  tree differs from the one the build read, the result stands only if the
  difference is in other subports' blocks than the target's. An active
  port's directory is held to the same rule, by its name. `_resources`
  still compares by tree. A nil reading compares trees alone, as before.
- **The engine's reading** (`driver.blocks`, `subportEdits`) reads both
  directory trees. Every entry but the Portfile must be the same object,
  and the Portfile is read from each tree, so nothing is recorded or
  migrated. Anything unreadable reuses nothing. Each pair of trees is
  read once per environment's choice.

The replay field testing offered is what shows it done: master's
terraform checked, then the branch, building terraform-1.16 alone and
reading its siblings as reused.

## The replay's rerun on 1f445dc7

Field testing replayed it on a DB copy: commit e083c13 changed only
`revision 0→1` inside `subport terraform-1.16 {`, checked as check-553
with `--build minmax`. 17 siblings reused check-549, and the exclusions
were 3 lines where they had been 25. It found two things.

- **The Siblings line didn't show** on `check --plan` of e083c13.
  `PlanSiblings` read only the recorded change records, and a new
  commit's files have none until a check or a submission makes them. It
  makes them now (`revisionChanges` collecting, as submit's does).
  `TestAPlanOfNewFilesNamesItsSiblings` plans a fresh capture, and it
  fails with the records only read.
- **terraform_select built on both releases.** That's the reuse rule
  working, not this change missing it. terraform-1.16, which built, has
  `depends_run port:terraform_select`. A reused target that a building
  one needs is installed from its kept archive, or else built
  (reuse.Choose, `Needs` and `available`). check-549 kept no archive of
  it, so it built beside terraform-1.16: the log's "building
  terraform_select (1 of 2)". Its block is `subport terraform_select {}`,
  and BlockChanges reads the edit as terraform-1.16's alone. A check
  whose earlier build kept select's archive would install it instead.
