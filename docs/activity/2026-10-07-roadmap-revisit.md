# 2026-10-07: the roadmap brought up to date

Docs only, from the prioritization thread's roadmap revisit and the person's answers.

- **Next** says where the prime-time pass stands: rc1 to rc6 tagged, run 4 on rc6 with 79 rows and no harms, every product finding fixed on main through 9213fc80, and what's left before v0.3.0 and macports/macports-ports#34756. After it, one ordered list: the three items from Later's top, archive listing the ignored files it removes, layer 1, then the survey rules.
- **Done** holds what Next held before, finished from 2026-09-27 to 2026-10-04, with its activity notes; **Later** keeps only unordered items.
- **Decisions for the person** lists what's open: the architecture review's after-release items, the privacy follow-up, reusing an archived branch's name, and warning before a dependency builds from source. **Decided** records the 2026-10-07 calls: ports that don't need Xcode keep building in the Xcode image; bump without a terminal opens its pull request; submit doesn't check by itself after a rebase; `check.on` defaults to this Mac's release; and archive's ignored files, after the release.

Later the same morning, the person decided two of the four: an archived branch's name is reused by renaming the archived branch or forgetting it, and the dependencies that build from source are found by the guest's `port -b install`, binary archives only. Both are Next's after-release items 7 and 8, and Decided says so; the architecture review's slotting and the privacy follow-up stay open.

At 11:05Z the person chose "Fixes first" for the architecture review's findings after the release: a fixes batch first, released as v0.3.1 (X2, L2b, L3a, M3's test, and the small ones riding along), then the three items approved on 2026-10-05, the engine's file moves, the structural work led by C3, and layer 1 with its findings folded in. Next orders them so, ahead of the survey rules and the three other after-release items, and Decided records it; only the privacy follow-up stays open.
