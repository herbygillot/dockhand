# 2026-09-24: `--os` adds to the host's release, and every release must pass to publish

Step 1 of the roadmap's Next. `verify --os`, built by a Linux session on
2026-09-23 (`b4a0553`, [note](2026-09-23-verify-on-named-releases.md)),
made two choices the contracts walkthrough later decided the other way
([direction record](../reviews/2026-09-23-contracts-direction.md)), and
the person settled that the decisions supersede it.

## Decision 4: `--os` adds releases to the host's

The named releases were the whole set, so `verify jq --os sonoma` built
on Sonoma alone. Now the host's release is always built, first, and
`--os` adds to it; naming the host's release too builds it once, and
`--os available` adds every prepared release.

- `app.Services.buildPlatforms` puts the evaluated platform first.
- `workflow.resolvedPlatforms` refuses a requested set that does not
  start with the evaluated platform, so the rule holds for every caller,
  not only the CLI's intake.
- The resolution's `Build` is therefore the host's, and the further
  releases are `PlatformBuilds`, which keeps a later plain `verify`
  inheriting from jobs without them as before.

## Decision 14: publication requires every requested release

A job with further platform builds settled strictly already (one failed
release fails the job), but publication cited the host's passing attempt
and `policy.PublicationCoverage` returned early for a job without
dependents or a shared-release scope, so the host's pass could publish a
job whose Sonoma build failed. Coverage now treats platform builds as it
treats dependents: the whole plan must have passed, each target on each
release, with no newer non-passing result. The pull request gains a
"Verification on each macOS release" section listing each release's
result (`publish.PlatformSummary`); a shared release verified on several
releases names the release on each line of its own section.

## Tests

`policy.TestPublicationCoverageRequiresEveryRequestedRelease` fails
without the coverage change. `publish.TestPlatformSummaryNamesEachRelease`
covers the pull request section. The binding and intake tests now expect
the host first (`TestResolvedBuildsMatchTheNamedPlatforms`,
`TestNamedPlatformsEachBuildAndAllMustPass`,
`TestNamedPlatformsAreNotRecordedSettings`,
`TestBuildPlatformsNameReleasesAndPreparedImages`). `docs/build-platforms.md`,
`docs/usage.md`, `docs/cli-design.md`, and the flag's help say it.

`verify --os` against real images is one of the handoff's Mac-only
checks, run separately.
