# The pre-v0.3.0 bridges, and what nothing produces

The person's answers to the cleanup's open list (2026-10-16): delete each
bridge from before v0.3.0 if it is safe to, and what nothing produces
unless a roadmap item needs it. Every bridge below read something only
builds from before 2026-09-28 wrote; v0.3.0 is from 2026-10-15.

Bridges deleted:

- **A rebase checkpoint without its base.** Every checkpoint since schema
  013 records the master it started from, and `AddCheckpoint` refuses one
  without it. restore said such a rebase couldn't be put back on its
  master; now it validates the checkpoint first, so an older row is refused
  before any Git change.
- **Plans from before each environment had its own** (2026-09-27). Reading
  one says what it is, instead of converting it. A branch still open from
  before then on the person's Mac would have checks clean, evidence, and
  reuse can no longer read.
- **JSON tidy plans** (version 1, before 2026-09-28): a JSON file is now
  not a saved tidy plan, as any other file is.
- **The `Assisted-By: Dockhand` and `Generated-by:` trailers** (before
  2026-09-19): only `Generated-By: Dockhand` is dockhand's attribution.
- **The portindex cache layout before generations** (2026-09-26): its
  `complete`, `standalone`, and `candidates` entries and `index.lock`, which
  any clean since collected once they aged past `cleanup.after`. The
  retention test covers a leftover mirror seed instead.

Never produced, and planned by no roadmap item:

- `OutcomeUnevaluated`, from model, the words, Design v3's outcome list, and
  the architecture table. The schema's CHECK still admits the word; a
  migration to drop it would change nothing.
- `ConcernClass` `Resolved`, from model and the assessment design.
- `OutdatedLook.Master`, which serve wrote and nothing read.
- `host.Machine.Guard`, which nothing set, so every tart command was handed
  a nil file subprocess dropped.
- The stubs built only for platforms other than macOS and Linux, five
  files, with `coord.ErrUnsupported`. Tart's `!darwin` stub stays, since it
  covers Linux.
- tools/acceptance/rerun-rc7, rc7's rerun list.

Left as they are: `distfetch.Client.MaxBytes` stays unset, since a distfile
may be gigabytes; and `upstream.Result.Evidence`, the person's call.

Tests: the whole suite, the Tart guest tests, vet at each commit, lint
(Linux and darwin), and `deadcode -test`.
