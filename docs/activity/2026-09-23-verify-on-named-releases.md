# 2026-09-23: verify builds on named macOS releases

This answers item 3.3 of the contracts review, "the host's platform is the only platform", for `verify`. The design is in `docs/build-platforms.md`.

Verification had one platform, which served both to read the Portfile and to build it. It now has an evaluation platform, which stays the host, and one or more build platforms. `dockhand verify --os sonoma --os sequoia` builds the host-evaluated targets on each named release, each in its own prepared Tart image. `--os available` names every release that has a `dockhand-base-<release>` or `dockhand-xcode-<release>` image locally. Without `--os`, nothing changes.

Decisions the person made:

- **Opt-in.** The host remains the default. When `--os` is given, the named releases are the whole set, so the host is built on only if it is named.
- **Strict pass.** Every named release must pass. The settlement already worked this way for jobs with several attempts; one failed release fails the job.
- **GitHub is refused.** `--os` with `--provider github`, or with a configured GitHub provider, says that the fork workflow's runner matrix is the workflow's choice. Under `auto`, `--os` selects Tart like any other Tart option.
- **Verify only.** `bump` and the corrections still build on the host.
- **Name.** `available`, not `prepared`.

What changed:

- **`record.JobSpec.PlatformBuilds`.** These are the further builds of the same targets; `Build` is the first named release. SQLite keeps them with the other job options. Intake (`normalizePlatformBuilds`) accepts them only for `verify`. Each must be local, share the provider, test, and source policies, and name a distinct platform, and there can be no dependents.
- **`verify.Plan`.** The plan crosses targets with builds. Each target on each release is its own verification target, numbered across the plan. A single build keeps its old identifier.
- **`workflow/choice`.** `Options.Platforms` configures one Tart build per named release, with no fallback to GitHub. `verify.BuildOptions.Named` tells Tart that the release was asked for. A release past `tart.DefaultDarwin` is then built rather than refused, and a missing image asks for `setup --os <release>`.
- **`workflow.BindVerification`.** It checks that the resolution builds on exactly the named platforms, in order, or on the evaluated platform when none were named. It refuses named platforms when the verification covers dependents, including dependents inherited from the contribution's recorded settings.
- **`contributionBuild`.** The recorded settings a later `verify` inherits now come from the newest job that built on the evaluated platform alone. Without this, `verify --os sonoma` followed by a plain `verify` would have inherited Sonoma as the platform the Portfile is evaluated on.
- **`app.Services.buildPlatforms`.** It turns names into platforms that keep the evaluated platform's OS and architecture, as `setup --os` does, and reads `available` from `tart list` through `verify/tart.Provider.PreparedReleases` and `tart.PreparedReleases`.
- **The CLI.** `verify --os` is repeatable. It is refused before anything is opened when combined with `--image`, `--dependents`, or GitHub. It counts as a change of verification settings.

Still host-bound, and written down in the design note: whether a target needs full Xcode is read from the host evaluation for every release. Reuse is not consulted for a plan with several builds, and neither is it for several targets. `publish` selects evidence on the host platform, and each release's attempt stays that release's evidence.

Tests added:

- `verify.TestPlanBuildsEveryTargetOnEveryPlatform`
- `workflow.TestPlatformBuildsAreAVerificationsOwn` and `TestResolvedBuildsMatchTheNamedPlatforms`
- `workflow_test.TestNamedPlatformsEachBuildAndAllMustPass`, run through SQLite and the cycle with one release passing and one failing
- `workflow_test.TestNamedPlatformsAreNotRecordedSettings`, which fails without the `contributionBuild` filter
- `choice.TestNamedPlatformsBuildInTartOnEach`
- `tart.TestPreparedReleasesAreSetupsLocalImages`
- `verify/tart.TestNamedReleasesAreDeliberate`
- `app.TestBuildPlatformsNameReleasesAndPreparedImages`
- `cli.TestOSRefusesWhatCannotBuildOnNamedReleases` and `TestOSChoosesTart`

None of this has been run against real Tart images, because this environment is Linux.
