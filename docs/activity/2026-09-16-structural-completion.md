# Completing the structural review

The remaining items from the [structural review](../reviews/2026-09-16-bump-machinery-structure.md), each landed as its own commit. Sections are added as items complete.

## Checksum refresh through the observed archive plan

`refresh-checksums` used the plain declaration path, which requires the evaluated distfiles to cover every checksum exactly once. Ports that declare per-platform distfiles, deno among them, declare checksums for every platform while evaluating only the native one, so the refresh was refused with "checksums must cover every source distfile exactly once". The version bump already handled the same Portfiles by observing every modeled context.

`prepareChecksums` now plans through `planObservedChecksums` whenever the reader can observe: every modeled context is observed on the unchanged Portfile, each bound archive is planned once, and the existing `applyObservedArchives` downloads, rewrites the checksum tokens, and checks fidelity per context. Since nothing but checksums may change, every context is a protected context. `archivePlan` carries the commit subject so the apply path no longer composes it from a release, and it returns early with the downloads when the refreshed contents equal the original, which is how "already current" is reported. The plain path remains for readers without observation. `Result.Coverage` lists the observed contexts as it does for bumps.

Checks: a new test refreshes a Portfile with `build_arch`-conditional distfiles, downloads both archives, rewrites both checksum groups, preserves version and revision, restores the workspace, and reports no change on a second refresh. The existing preparation checksum tests now run through the observed path unchanged. On the real tree, `refresh-checksums deno --diff` observes darwin 25 arm64 and x86_64, downloads both archives, and reports the port current in 19 seconds.

## A policy leaf under workflow

`verification_reuse.go`, `publication_coverage.go`, and `publication_policy.go` were read-only questions over a `state.Reader`: which recorded evidence applies to a build question, whether evidence covers a publication's whole cohort, and whether a publication action still matches its job. They depended on `workflow` only for `ErrInvalidRequest`, the `preparationAction` predicate, and one `Engine` method that opened a state view. The decisive test was `publicationEvidence`, called from both submission and the publication runner; it needed nothing from the cycle.

They now form `workflow/policy`, with `SelectVerification`, `SelectRecordedVerification`, `PublicationCoverage`, `DescribeCoverage`, `PublicationEvidence`, and `ValidatePublicationAction` exported and the package importing only `record`, `state`, `verify`, and `publish`. `workflow.ErrInvalidRequest` is an alias of the policy error, so callers see the same identity. The unnamed revision-and-source pair `publicationInput` became `record.Job.EffectiveSource`, and `preparationAction` became `record.Action.Prepares`, both of which the review had asked for by other names. `verification_select.go` stays in `workflow`: it selects among an `execution`'s attempts and names a runner action, which is driver mechanics, not policy. The intake-versus-drive split remains possible but is not needed now that the shared questions have a home.

Checks: the workflow suite is unchanged in behavior; a record test covers the effective source before and after preparation and which actions prepare. `go test ./... -count=1` and `go vet ./...` passed.
