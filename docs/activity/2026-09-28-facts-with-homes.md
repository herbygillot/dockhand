# 2026-09-28: facts with homes

The fifth step of the private-helper review's item ([review](../reviews/2026-09-28-private-helper-ownership.md), finding 7 and its table; [reconciliation](2026-09-28-private-helper-review-reconciled.md)). Each fact moves to the package that owns it, as an operation over it, in its own commit.

## Which releases run on which architecture

The observer, choosing which platforms to evaluate a Portfile on, knew by itself that arm64 begins at Darwin 20 (`current >= 20`, `major >= 20`). It also took every release to run on x86_64, so it evaluated Golden Gate on Intel. That platform doesn't exist: Apple made macOS 26 the last release for Intel Macs, and MacPorts' only builder for Darwin 27 in the facts table is arm64. `TestProfilesSkipTheDarwinThatNeverShipped` had pinned the Intel profile.

The fact is now `macos.RunsOn(darwin, architecture)`, beside `ProductForDarwin`: Intel's x86_64 until Darwin 25, as every release before Apple silicon is modelled, and arm64 from Darwin 20. The observer keeps its own policy of which profiles to sample. It samples each side of a boundary on the first architecture its release runs on, and on each where the port reads the architecture. So Golden Gate is sampled on Apple silicon alone.

`TestReleasesRunOnTheArchitecturesTheirBuildersDo` holds the rule to the facts table's builders, both ways, wherever the table has them. The profile tests now say Golden Gate isn't Intel, and that a boundary the port doesn't make architecture-dependent gets one architecture each side. Four mutations each fail a test.

MacPorts' own architecture rules, its universal archs and deployment target from Darwin 20 (`macports/platform.go`), mirror what MacPorts does rather than what a Mac runs, so they stay with it.

**Seen, not changed:** a boundary at Golden Gate, `${os.major} >= 27`, samples nothing below it. Its lower neighbor, Darwin 26, never shipped, so the release below the boundary, 25, is never evaluated. That's the observer's sampling policy, on the roadmap's smaller items.
