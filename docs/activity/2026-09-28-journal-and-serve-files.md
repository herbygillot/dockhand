# 2026-09-28: the journal, and serve's files

This is roadmap item 1b of the block before item 6: what grows without bound, and what several checkouts in one database got wrong. The numbers are the code-organization review's findings. Each part is its own commit.

**Serve's files are per checkout, and the day's look is stamped once it has run (findings 35 and 37).**
- **The files.** Serve kept four small files beside the database:
  - `serving.json`, which status reads;
  - the day's `outdated.stamp` and `outdated.json`;
  - `cleanup.stamp`, which automatic cleanup has used since decision 36.

  One database can register several checkouts, while serve's lease and the journal are per checkout. So two serves leading two checkouts shared one day. Since automatic cleanup, a command in one checkout, cleaning up after itself, held off the other checkout's cleanup of its merged branches: the stamp said a pass had just run.
- **Now per checkout.** The files are `serve/<repository>/…` beside the database. A checkout keeps its own day, and the global caches, port indexes and Tart's images, are pruned by whichever pass runs, harmlessly twice.
- **The day's look is stamped once it has run.** It was stamped before, so a serve stopped in the middle skipped the day. Now a look cut short by serve stopping isn't stamped, and the next serve looks again. A look that failed is stamped as before, and tried the next day, its problem said once.
- **The cleanup schedule says why in a type.** `CleanupDue` returned a sentence, and its two callers tested whether it began with "only " to tell low space from a day passing. It returns a `CleanupReason` with `LowSpace` and `Words`.
- **Upgrading.** The first run after an upgrade finds no stamps where it looks. So it cleans up, and serve looks at the day's releases, once more. A serve still running the old build writes the old `serving.json` until it's restarted, as `serve --install` asks after an upgrade.
- **Tests.**
  - `TestEachCheckoutKeepsItsOwnCleanupDay`: two clones in one database.
  - `TestServeLooksAgainAfterStoppingMidLook`, which fails when an interrupted look is stamped.
