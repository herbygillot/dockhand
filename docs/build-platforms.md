# Build platforms

Answers item 3.3 of `docs/reviews/2026-09-23-contracts-review.md`, "the host's platform is the only platform", for `verify`. The walkthrough of that review settled more since, in the [direction record](reviews/2026-09-23-contracts-direction.md) (decisions 4–7, 9, 14, 22): the facts table makes per-release Xcode need answerable on a Mac. Where this document and those decisions differ, the decisions supersede it, and the code is being aligned: `--os` adds releases to the host's rather than naming the whole set (decision 4); publication requires every requested release to pass rather than selecting the host's evidence (decision 14); and an unnamed build on a release newer than the default is no longer refused (decision 5).

## Two platforms, not one

A verification has an **evaluation platform** and one or more **build platforms**, and until now they were the same thing.

- **Evaluation platform.** The macOS that MacPorts Base describes when dockhand reads the Portfile: the host on a Mac, the modeled Mac on Linux. It decides the targets, their variants, and whether a target needs full Xcode. It stays the host. Base derives `os.major` from the machine, so the bound here is MacPorts' own.
- **Build platform.** The macOS a Tart guest builds on. Nothing in a Tart build requires it to equal the evaluation platform. The accepted build record has to name the image's platform, and the guest has to confirm it at admission; both checks already exist. The PortIndex a guest is given is already generated per platform, with the buildbot's variable overrides, so it describes the guest and not the host.

## The contract

- `dockhand verify` builds on the host platform unless told otherwise. That is unchanged.
- `--os <release>` names a build platform. It takes the release names and major versions that `setup --os` accepts, and can be repeated. When it is given, the named platforms are the whole set: the host is built on only if it is named.
- `--os available` names every release with a locally prepared image, either `dockhand-base-<slug>` or `dockhand-xcode-<slug>`. When nothing is prepared, it is refused before anything is accepted.
- **Strict pass.** Every requested platform must pass. One failed platform fails the job; one blocked or errored platform leaves it needing attention. This is the rule the settlement already applies to every job with more than one attempt.
- **Evidence stays per attempt.** A passing Sonoma build is Sonoma evidence even when the Ventura build of the same job failed. `publish` selects evidence for the host platform, as before.
- **No inheritance.** `--os` applies to that one verification. A contribution's recorded settings come from its newest job that built on the evaluated platform alone, so a later `verify` without `--os` builds on the host as it did before. A contribution whose recorded settings cover dependents refuses `--os`, as `--dependents` does.
- **A named release past the default is deliberate.** An unnamed build on a release newer than `tart.DefaultDarwin` is refused, and the refusal asks for `--image`. Naming the release with `--os` is itself the deliberate choice, so it is not refused.

## What `--os` refuses

- `--provider github`. GitHub verification runs the fork workflow's runner matrix, which is the workflow's choice, not dockhand's; `--os` cannot narrow or widen it. Under `auto`, `--os` selects Tart, like any other Tart option.
- `--image`. An image is one platform.
- `--dependents`. Dependent discovery and its image overrides are planned on one platform. Dependents across platforms are a plan of plans, and are left for later.

## How it is built

1. **Intake (`cli`, `app`).** The verify command refuses the combinations above before anything is opened, and passes the names on. `app` turns each name into a platform that keeps the evaluated platform's OS and architecture, as `setup --os` does. It resolves `available` from the Tart image listing, through `verify/tart.Provider.PreparedReleases`, and builds a release that is named twice only once.
2. **Choice (`workflow/choice`).** `Options.Platforms` makes the resolver configure one Tart build per named platform, each with its own image. The resolution's `Build` is the first platform's build, and `PlatformBuilds` holds the rest. `BuildOptions.Named` tells the provider that the platform was named, so a release past the default is built and a missing image asks for `setup --os <release>`.
3. **Binding (`workflow`).** `VerificationRequest.Platforms` carries the named set. The binding evaluates on the evaluation platform as before. It then checks that the resolved builds name exactly the requested platforms, in order, or the evaluation platform when none were requested.
4. **Record (`record.JobSpec.PlatformBuilds`).** These are the further builds of the same targets, which intake validates. Only `verify` accepts them, and each must be a local build with the same provider, tests, and source policy as `Build`, on a distinct platform, with no dependents.
5. **Plan (`verify.Plan`).** The plan crosses the targets with the builds: every target on `Build`, then every target on each of `PlatformBuilds`, each pair its own verification target with its own ID and platform. With more than one build, reuse is not consulted. That is already the rule for a plan of more than one target.

## Not decided here

- **Per-platform Xcode need.** Whether a target needs full Xcode is read from the host evaluation, and applied to every platform. A Portfile that needs Xcode only on some releases is built with the host's answer. A build that lacks what it needs fails visibly in the guest; it does not pass silently. Evaluating on each build platform requires modeling a darwin platform on a darwin host, which the evaluator does only on Linux today.
- **Preparation.** `bump` and the corrections still prepare and verify for the host. Taking `--os` there means deciding what the prepared branch's recorded build is.
- **Capacity.** The Tart pool is per Tart home, so all platforms share one capacity, which defaults to two. Apple's license allows two macOS guests at once. More platforms queue; they do not run wider.
- **Hardware.** Apple's framework refuses to *install* a guest newer than its host from an IPSW, which dockhand never does; a prebuilt newer image runs. On 2026-09-23 Cirrus Labs' macOS 27 (Golden Gate) image booted and provisioned on a Tahoe 26.6.2 host ([direction record](reviews/2026-09-23-contracts-direction.md)). Golden Gate's images use ASIF disks, which dockhand declines until openai/tart#1344 is fixed.
