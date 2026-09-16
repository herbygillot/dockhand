# Target workflow: integrated validation

The target-workflow milestone (stages 0–5) is implemented. Source-bound names, early contribution identity, frozen retries, target-based continuation, native HTTP regex discovery, and guarded fetch preparation share the existing workflow engine. No new external library was required. The new package is `macports/selection`; it composes native evaluation with exact-source PortIndex lookup.

## Automated checks

- `go test ./... -count=1 -timeout=10m`: passed.
- `go test -race ./... -count=1 -timeout=10m`: passed.
- `go vet ./...`: passed.
- Final selector/outcome regressions and `make build`: passed. The local `dockhand` executable was rebuilt.
- Tests cover migration, concurrent equivalent acceptance, request replay, failed-preparation retries, unchanged frozen source/release, no-update closure, dirty/missing branches, mismatched selectors, target-selected publication/evidence, and cancellation of a fixed job set.
- Native livecheck fixtures cover ordering, series filters, duplicate captures, prereleases, malformed/incomplete observations, Tcl extraction, request options, and persisted release provenance. Guard tests cover exact refusal-only shapes, preservation, rejected contexts, and zero archive/helper work after a local planning refusal.

A final exercise found that replaying an old branch-ready job could say verification had never been requested even after a later verification. Its outcome now explicitly says that this job did not request verification; target status still shows the later pass and reuse.

## Existing database migration rehearsal

A read-only `db backup` copied the user's schema-14 database. Only the disposable copy was migrated to schema 15, then checked with `db check`. All 35 jobs, 23 attempts/evidence records, 8 revisions, 5 PRs, and 9 publications were preserved, across three repositories. Six previously unassociated preparation jobs gained separate contribution identities. Existing row contents were compared, allowing only the intended job-to-contribution association and new migration columns/rows.

The original database was not upgraded or advanced. Unrelated historical attempts are not merged merely because they name the same target. If several open contributions match, explicit `--change` or `--branch` selection is required.

## Real-source survey controls

Repeated the 147-Portfile corpus at source `87ff2b89b11d1666d39a51929499c5787bebecdf`: 83 `input-found`, 53 `unsupported`, 11 `unknown`, identical to the baseline for every entry. This is a regression corpus, not a whole-tree success estimate.

Repeated the sixteen targeted controls: 10 `input-found`, 2 `unsupported`, 4 `unknown`; among nine explicit candidates, 6 `candidate-checked`, 2 `unsupported`, 1 `unknown`. Wasmer moved from refusal to checked candidate. Terraform, Helm, gh, Deno, and libusb candidate controls passed. Shared-release Python siblings and unresolved platform/auxiliary-source cases remain in the broader-coverage roadmap. Supplied candidate versions in this local survey are structural probes, not claims of available upstream releases.

## Isolated live exercise

Used a separate ports clone, database, artifact directory, and Tart home under `/private/tmp/dockhand-target-exercise`. The pristine Tahoe base image was copied with APFS cloning; original images and ports branches were not changed. Current authoritative MacPorts source was `0f8e26f480b8a6f0fd39ea57c58f2083e259e06b`.

Terraform commands, with the isolated tree/database selected globally:

```sh
dockhand bump terraform-1.16 --no-verify --json
dockhand verify terraform-1.16 --image dockhand-base-tahoe --trace --json
dockhand publish terraform-1.16 --dry-run
dockhand verify terraform-1.16 --wait --json
dockhand bump terraform-1.16 --no-verify --json
dockhand status terraform-1.16
```

The HashiCorp listing selected 1.16.3 from 1.16.0 on 2026-09-16; the Portfile's evaluated livecheck supplied the 1.16.x boundary. Preparation refreshed both architecture archives without changing other series. The prepared commit was `c2dccd8b2894b00ad6798fe30f7d4835d618e947`; Tart attempt `attempt_FNW5CGVQDONGPOPHW2DYZSLBUL` passed lint/build/install on macOS 26.6.2 arm64, MacPorts 2.12.6, CLT 27, Tart 2.37.0. The second verify inherited the exact settings and reused that attempt. Repeating bump returned the original job and branch. Target-selected publication preview selected that same commit/evidence and the expected personal-fork-to-MacPorts destination. No remote branch or PR was created.

Wasmer commands:

```sh
dockhand bump wasmer --no-verify --json
dockhand verify wasmer --image dockhand-base-tahoe --trace --json
dockhand publish wasmer --dry-run
```

Automatic discovery selected 7.4.2 from 7.4.0. Preparation checked the original Cargo declarations, regenerated dependencies, refreshed the archive, and preserved the Darwin < 23 arm64 rejection. The prepared commit was `51a3c7865407bb3bb8228bbd71d29255e2f4c7f2`. The exact earlier reported source `1a43a39ca19448b8904bb750a9a06f0d3eb30185` also passed the local 7.4.1 candidate check. Tart attempt `attempt_HKGUUIHIL5BDMU6TI7NMR55AMS` passed lint, build, and installation on the same Tahoe/CLT profile at 2026-09-16T17:26:33Z. MacPorts reported no broken files or ports. Target-selected publication preview retained that prepared commit and passing attempt, with title `wasmer: update to 7.4.2`. No live PR was created.

No executable version output was used as a verification criterion. These runs do not establish support on the platform Wasmer explicitly rejects.

Full local observations, command output, and patches are retained outside the repository under `~/Documents/ChatGPT/Dockhand/exercises/2026-09-16-target-workflow`; source patches are exercise evidence, not changes to Dockhand's repository. The roadmap records remaining broader coverage separately.

## Cleanup

Both verification resources were released by normal driver cycles. The final isolated database contains five completed jobs and two released resources; a checked backup and full logs/patches were retained in the external evidence directory. After confirming that Tart listed only the stopped disposable base copy, that copy and the 81 MiB exercise artifact directory were removed. The exercise Tart home is empty. Its image directory had reported 26 GiB allocated before removal, but APFS cloning/shared extents mean this is not a measurement of uniquely consumed or reclaimed space. Original user images and the default database remain unchanged.
