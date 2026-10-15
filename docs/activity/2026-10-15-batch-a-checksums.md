# Batch a, second half: checksums on just about anything

The person's word (2026-10-09): "`checksums` should be able to update or
verify checksums on just about anything, and shouldn't be subject to most hold
conditions." Built in the shape layer 1's step 2 keeps:

- **No branch for a plan.** `checksums --plan` with no branch open started one,
  "this starts dockhand/jq-checksums for it", for a plan that changes nothing
  (eight were started in the person's checkout surveying this batch, and
  archived). It now plans on master, as `update --plan` does
  (`unbranchedPlan`, `updateSource`). TestUpdateWithoutABranchStartsOneOrUsesTheOne.
- **This Mac's context as it stands.** cpi, a qmake5 port whose PortGroup reads
  the host's Qt paths, was refused, "modeled context depends on host state".
  On this Mac the evaluation's host is the host, so its reading stands
  (`bindArchivesOn`); a context modeled on another Mac that reads the host is
  left as it was and said. `checksums cpi --plan` now reads its archive.
- **A typed list of what's left** (`portedit.ChecksumLeft`, kinds context,
  declaration, dependencies), carried to `engine.Update.ChecksumsLeft`: a
  declaration no readable context fetches is left as written and said, where
  it refused the whole port. "Left as it was: <what>, <why>", a line each.
- **`--plan` is the check.** It says "all 3 distfiles match their checksums",
  or "1 of 3 distfiles don't match their checksums: <names>" and exits 3
  (`portedit.Result.Differing`, `engine.Update.Compared`). The help says so.

Left for later in the item, and still on the roadmap: fnox, fetched with Git
and with submodules, still needs a dedicated preparer; regenerating a Rust or
Go port's crates or modules after a hand edit with overrides named, as
certstrap's and meli's. Tests: TestAnArchiveDiffersByAnyValue,
TestALeftContextSaysWhy.
