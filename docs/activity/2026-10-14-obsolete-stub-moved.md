# An obsolete stub the edit moved isn't updated again

Field testing at a3c43ef7: `bump terraform-1.16 --with-obsolete` made the whole
edit, patchNumber 5 to 6, both architectures' checksums, and terraform's
`version 1.16.5` to `1.16.6` inside `if {${subport} eq ${name}}`, which moves
with terraform-1.16's shared release. Then it exited 1: "its obsolete
terraform couldn't: portfile: unsupported source edit: no version input can
select 1.16.6", and "Kept: dockhand/terraform-1.16-1.16.6, with nothing
changed", though the worktree held the edit. Field testing submitted it by
hand as macports-ports #35502.

- `engine.Update` reads the obsolete stub's version after the edit as well
  (`familyAfter`): one the edit moved to the new version is said as moved,
  and the follow-up update, which found no version input left to move, isn't
  run. TestAnObsoleteStubTheEditMovedIsntUpdatedAgain.
- An update that fails after its edit says the edit it kept, "with the edit to
  <files> made and not committed", rather than "with nothing changed".
- The stub line names an obsolete stub as one: "terraform is an obsolete stub,
  replaced by terraform-1.16; editing terraform-1.16 and its sibling subports
  as one release".

Not touched: check counting all 18 terraform subports as changed, which is
layer 1's (the evaluation-model thread).
