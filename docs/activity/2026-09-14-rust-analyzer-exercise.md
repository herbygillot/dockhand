# rust-analyzer bump-to-PR exercise

Rebuilt Dockhand from the current source and exercised `rust-analyzer` against `/Users/herby/Source/macports-ports`. The requested update was published as [MacPorts PR #34679](https://github.com/macports/macports-ports/pull/34679).

## Result

- Upstream tag: `2026-09-14`, commit `682a84e95b5a52cf06e9822fcaa4f628738554bf`.
- MacPorts version: `20260907` → `20260914`, revision remains zero.
- Contribution branch: `dockhand/bump/rust-analyzer-v4lqjsqqszeazi243mu4qmvlbv`.
- Contribution commit: `a70f5f48ac3e60935287f875213972fe2bf080cc`.
- Publication: `herbygillot/macports-ports` → `macports/macports-ports:master`.
- Main job: `job_V4LQJSQQSZEAZI243MU4QMVLBV`; verification: `attempt_Q2KXW6NOTEHDJVMUANAXN7ILDU`.
- Verification passed on macOS 26.6.2 / build 25G83 / arm64, MacPorts 2.12.6, Command Line Tools 27.0.0.0.1788430756, using `dockhand-base-tahoe`.
- Lint, debug build, debug install, activation, and linking checks passed. The Portfile declares no test phase; the PR accurately leaves that checklist item unchecked. Executable version output was not used as verification evidence.
- Cargo regeneration confirmed all 333 existing crate declarations were unchanged. The final diff changes only the setup date and the archive's rmd160, sha256, and size values: four additions and four deletions in one Portfile.
- The job completed with publication confirmed, its revision recorded as published, and its verification resource released.

The durable run began at 2026-09-15 03:46:46 UTC, was admitted at 03:53:07 UTC, and confirmed publication at 03:57:53 UTC (the evening of September 14 locally).

## Exercise commands

```sh
make build
./dockhand -T ~/Source/macports-ports bump rust-analyzer --diff
./dockhand setup --check
./dockhand -T ~/Source/macports-ports bump rust-analyzer \
  --publish --remote herby --upstream origin --trace
./dockhand -T ~/Source/macports-ports wait job_V4LQJSQQSZEAZI243MU4QMVLBV --trace
./dockhand -T ~/Source/macports-ports verify rust-analyzer \
  --branch dockhand/bump/rust-analyzer-v4lqjsqqszeazi243mu4qmvlbv --wait
```

Dockhand's stored Keychain credential was rejected. GitHub CLI authentication was valid for `herbygillot`; subsequent authenticated commands explicitly received that credential through `GH_TOKEN` in the child process environment. No credential value was logged or written into the repository, and the stored Keychain credential was not changed. This was an explicit credential choice, not a change to Dockhand's no-silent-fallback policy.

## Subsequent implementation

The calendar-specific rule below describes the implementation used for this exercise. It has since been replaced by evaluator-driven version probing; compact-date input inversion is no longer assumed. See [the portedit implementation report](2026-09-15-portedit-version-probing.md).

## Defects found and fixed

1. **Calendar tags and calculated versions.** The first preview rejected `github.version=2026-09-07` because the evaluated port version was `20260907`. `macports/source` now recognizes the date-to-compact-date convention when current evaluated values agree. Automatic selection compares the MacPorts version while native Tcl livecheck validates the source spelling. Preparation changes the literal setup argument and preserves the calculated `version` expression; native reevaluation must still produce the expected new version. Explicit date tags and compact version inputs are covered. Unrecognized transformations remain unsupported.
2. **Unchanged dependency formatting.** The first successful preview rewrote every Cargo row even though regeneration found no dependency changes. A dependency plan now preserves the original text of semantically unchanged blocks. The final PR has no Cargo formatting churn.
3. **Stale CLI help.** The bump help still described only GitHub, one archive, and literal checksums. It now describes the implemented GitHub/GitLab and dependency preparation capabilities.

The code and regression tests were authored during this exercise; no v1 comments or tests were copied.

## Additional checks

- Setup validated the existing base image in a disposable clone and reported both optional host helpers, `go2port` and `cargo2port`.
- A real-port preview with an intentionally missing `--cargo2port` failed with the port, tool, and block identified, before dependency preparation. It did not submit a job.
- Sent SIGINT to the attached driver after admission. The CLI exited with code 130 and instructions to resume; the guest and recorded attempt remained running. `dockhand wait` resumed the same attempt through publication.
- The follow-up verification job `job_I73E4CSUFUQ4AX26MEVHZC4LIV` completed with `verification passed (reused)`, referencing the original passing attempt and creating no new verification attempt.
- Confirmed the published PR's head commit, base branch, single changed file, and verification report through GitHub.
- `go test ./...`, `go vet ./...`, focused calendar/fidelity/formatting regressions, and the final `make build` passed.
- The native fidelity regression deliberately changes the calculated result for a new tag and confirms rejection before a candidate archive download.

## Follow-up observations

The initial full PortIndex generation took 3m48s, followed by a four-second update for changed source paths. This happened after reserving and starting the VM, reinforcing the existing roadmap item about preparing indexes before occupying verification capacity. It did not prevent this run from completing.

The invalid stored Dockhand credential still needs explicit replacement or removal if future invocations should use a different default credential source.

Session logs are in `/private/tmp/dockhand-rust-analyzer-*.log`, with preview diff and final JSON status alongside them. Those temporary logs are supporting evidence, not committed project artifacts.
