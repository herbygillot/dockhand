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
