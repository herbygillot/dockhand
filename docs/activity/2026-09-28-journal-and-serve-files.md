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

**The journal is pruned, read where it's needed, and observed once per command (finding 34).**
- **Pruning.** Nothing deleted events or sessions, so the journal only grew.
  - `watch` alone opened two observer sessions a redraw, each a row and two events: about 17,000 rows a day.
  - Design §11 promised events pruned with their runs, but runs are never pruned.
  - Automatic cleanup now prunes events older than `cleanup.after`, as it does the port index cache, and the sessions that ended, or whose heartbeat stopped, before then. A session a lease still names stays, since the lease row refers to it.
  - Nothing reads a pruned session: a session is read by its own process, or through a lease. The day's count of serve's pull requests reads today's events only.
  - `store.Tx.PruneJournal` does both in one transaction, and the pass journals what it pruned.
- **Reading.**
  - A command following a check, and `wait`, read the whole journal from sequence 0 on each start, to show one run's events. They now read that run's, through an index on the run (schema 21).
  - `watch` read everything once only to pass it by. It starts at the newest sequence now.
  - `store.Reader.RunEvents` and `LastEvent` are the store's reads, and `Engine.RunEvents` and `LatestEvent` the engine's.
- **One observer session per command.**
  - `status` opened one to judge stopped checks and another for serve's line, on every render, and `watch` rendered every 30 seconds.
  - A command now takes one observer, on its context (`observing`), and every judgment under it shares it. That is `status`'s whole render, and `watch` and `queue` for as long as they run.
- **Tests.**
  - `TestCleanupPrunesTheJournal`: an old event, an ended session, a quiet one, and a quiet one a lease holds.
  - The store's journal test gains the run's events and the newest sequence.
  - `TestStatusOpensOneObserverSession` counts session starts across one `status` of every branch: one, where there were two.

