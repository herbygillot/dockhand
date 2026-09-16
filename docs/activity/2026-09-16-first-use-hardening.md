# First-use path hardening

Roadmap item 3 after the [new-user deno exercise](../reviews/2026-09-16-new-user-deno-exercise.md): the small fixes a new user hits in the first hour, each without new design.

## Changes

- **Ports tree validation.** `macports.ValidatePortsTree` requires at least one `<category>/<port>/Portfile` and runs on the materialized committed tree before the first index staging, in metadata selection (`assess`, `outdated`) and name lookup (bumps, verification). Commands that only read workflow state, such as `status`, do not need a ports tree. Running from an unrelated Git repository now fails in 30ms with `not a MacPorts ports tree: ... pass --tree /path/to/macports-ports or set MACPORTS_TREE` instead of spending minutes on a full index and then reporting an incomplete index. Read-only `status` is unchanged.
- **Distfile errors carry context.** `fetch.Open` returns a typed `StatusError` whose message names the status and redacted URL. `portedit` adds, for a 404, that no archive is published at that location yet and that a release tag alone does not publish its assets. The deno 2.9.7 case now reads `fetch: HTTP 404 for https://github.com/denoland/deno/releases/download/v2.9.7/deno-aarch64-apple-darwin.zip; no archive is published at that location yet, ...`.
- **`assess --version` echoes the resolved release.** The human port line reads `deno: candidate-checked; current 2.9.6; candidate 2.9.7 (tag v2.9.7)`. JSON already carried the release.
- **`outdated` names its catalog.** Discovery details now say `latest eligible version among published releases is 2.9.6` or `... among tags ...`, and an update reads `Selected v2.9.7 from published releases`, so a maintainer who has seen an unreleased tag understands why it was not selected.
- **One index staging per command.** The selection reader memoizes staging per materialized root and tree, so repeated name lookups reuse the installed generation without re-entering the cache.
- **Distinct help prose.** `bump`, `bump-revision`, and `refresh-checksums` share only the preparation paragraph; each describes its own edit and has an Examples section.
- **`--version`.** Cobra's version flag reports the module version or VCS revision from the embedded build information, with a modified marker for dirty trees.

## Checks

- `go test ./... -count=1` and `go vet ./...`: passed.
- New test for the tree check; existing fixtures with `devel/fixture/Portfile` satisfy it.
- Real tree: `--version`, the fast failure from the Go repository, the three help pages, the assess candidate line, the outdated wording, and the 404 hint were exercised with the rebuilt binary. The bump preview also showed a moved master deriving from the retained seed in a four-second incremental pass.
