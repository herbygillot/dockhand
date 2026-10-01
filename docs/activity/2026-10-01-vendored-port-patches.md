# 2026-10-01: a vendored port's patches, read where they are

The dogfood session's hand update of rust 1.99.0 and cargo 0.100.0 found that `submit --plan` compared neither rust's nor rust-src's archives: "archives not compared: portfile: unsupported source edit: local patch directory is unavailable", though `lang/rust/files` holds nine patches, three of which the update needed to know still applied.

## What changed

- **A Cargo or Go port's patches are read in the tree's `files/`.** Batch 21 had a revision's assessment plan a vendored port's own archives by evaluating its Portfile with the crates or modules set aside, in an overlay (`withoutVendored`), which it removes as it returns. That evaluation's `filespath` names the overlay's `files/`, and the fetch policy (`archives.CheckPolicy`) reads the port's patches there after it's gone. So every Cargo or Go port that declares its dependencies and has a patch of its own went uncompared, which holds an unattended submission. The observation it returns now keeps the tree's `filespath`, where the port as it is says its patches are.
- **`assess.Policy` is 8.** An assessment that ended "not compared" for want of a patch directory isn't one another try may meet, so it stood for the same files under policy 7, and the fix didn't reach rust-0hxv, assessed before it. Raising the policy keeps an assessment made before from standing for one made now, as the assessment design has it. A recorded assessment that stands is now said with `-v`, "rust: the assessment recorded for these files under policy 8 stands", since nothing said whether one was made again or reused.
- The dogfood session took it for the sparse worktree not holding `files/`; the workspace brings a port's whole directory, `files/` included, and the overlay's lifetime was the cause.

## Verification

- `TestAVendoredPortIsComparedByItsOwnArchives` gains a Cargo port with a patch of its own; without the change it fails with the same "local patch directory is unavailable".
- The full suite with `DOCKHAND_TEST_MACPORTS_TCLSH`, vet, fmt-check, vendor-check, deadcode, and lint.
