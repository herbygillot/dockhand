# Checking declared patches before a build

The Codex 0.155.0-alpha.15 verification spent twelve minutes provisioning a guest and fetching thirteen hundred crates before MacPorts stopped in the `patch` phase: the port's `patch-package-managed-daemon.diff` rejected four of five hunks against the new source. Preparation already had the candidate archive in hand a minute in, so the answer was available before any job existed.

## Design

`archive` is a new leaf that walks tar (plain, gzip, bzip2) and zip members without extracting onto the host; the dependency manifest reader now uses it instead of its private copy. `macports/patchcheck` reads each declared patch (decompressing gzip and bzip2), collects the paths the diff headers name after the port's `-p` strip level, extracts only those members from the downloaded archives into a temporary directory, mapped through `worksrcdir`, `extract.rename`, and the new evaluated `patch.dir`, and runs the system patch command in check mode (`-C` for Apple's BSD-derived patch, `--dry-run` for GNU). Each patch reports `Checked`, `Applies`, and a detail taken from patch's own diagnostics, such as "4 out of 5 hunks failed while patching 'app-server-daemon/src/lib.rs'" or "No file to patch". Patches the check cannot model (xz or compress files, `patch.pre_args` beyond the recognized set, a `patch.dir` outside the source) are reported as unchecked rather than failed.

Preparation runs the check for version bumps whenever the port declares patch files, keeping archive bytes in a temporary store that it removes afterwards; the dependency-regeneration path already kept them. The result carries every patch's verdict, the progress stream summarizes it, and `bump --diff` prints one line per patch. The prepared candidate records the rejected patches, and branch integration then finishes the job as needs-attention naming them instead of moving to verification. The branch and change exist, so the correction path can refresh the patch and `verify` can run afterwards. Nothing refuses the version.

## Validation

- `archive` and `patchcheck` unit tests cover tar, gzip, and zip walks, member path hygiene, applying, stale, and missing-file patches, gzip-compressed patches, strip levels, `extract.rename`, `patch.dir`, unmodeled arguments, and the summary line.
- A native portedit test bumps a port with one applying and one stale patch: preparation succeeds, records both verdicts, and leaves no temporary directory behind.
- A workflow test injects a rejected patch and checks that the job creates its branch and change, then finishes as needs-attention with the patch named, in the preparation phase.
- Live: `bump codex 0.155.0-alpha.15 --diff` now prints "Patch patch-use-system-gstreamer-runtime.diff: applies" and "Patch patch-package-managed-daemon.diff: 4 out of 5 hunks failed while patching 'app-server-daemon/src/lib.rs'" in the preparation minute, the same finding the guest produced after twelve.
