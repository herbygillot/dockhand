# Platforms worded as macOS releases

Asked for on 2026-09-17 after the status table said "building on darwin 25" where the summary said "macOS 26". dockhand already knew the mapping between Darwin majors, macOS product versions, and release names (`macos.Release`, used by `setup`); `macos.Describe` now words a platform from it, "macOS 26 (Tahoe) arm64", chosen over the name-first form so the version is never ambiguous. An unknown Darwin major or a non-Darwin platform keeps its raw fields.

Every user-facing platform mention goes through it: the action summary's verdict lines, the status table's "building on" state, the `-v` status record's attempts, `setup`'s image line, and the publish dry run. Recorded data is unchanged; only wording moved.
