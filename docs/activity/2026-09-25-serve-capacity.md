# 2026-09-25: serve runs checks up to each provider's capacity

Design v3 §11: "each provider has its own capacity." Until now `serve` drove one run at a time, whatever it built on.

## What changed

- **`serve` drives runs concurrently.**
  - Each run takes a slot on every provider its plan builds on, and keeps it for as long as it runs. Serve starts every candidate whose providers all have a free slot, in the order `Next` always used: a run a gone driver left running, then a person's, then serve's own, oldest first.
  - A run that can't have all its slots waits, and the runs after it that can go ahead.
  - As each run finishes, serve reports it, frees its slots, and looks again.
  - Stopping serve stops the runs it drives. They are left running for the next serve, as before. An error in one run stops serve, after the others have stopped.
- **Capacity.** `[providers.command] capacity` (1 when unset) and `[providers.github] capacity` (2 when unset) are validated by `config.Load` and read with `File.Capacity`. Serve's first line gives each provider's: "builds on command (1 at a time), github (2 at a time)".
- **`Engine.Candidates`** lists every run serve could drive, less those a live session holds. `Next` is now its first. **`Engine.RunProviders`** names the providers a run's plan builds on.
- **Safe to share.**
  - `coord.Session` guards its record and last heartbeat with a mutex, and keeps its ID apart, since it never changes. The heartbeat goroutine already wrote them while the driver read them.
  - The engine guards the forge it builds on first use, which concurrent github checks and serve's pull-request following share.
  - Serve's output goes through a locked writer, so concurrent runs' lines never interleave.
  - The serve tests, and the coord and engine packages, pass under `-race`.

## Decisions

- **Two for github by default, one for everything else.** A github check spends one runner per macOS release MacPorts' matrix builds on, three today. GitHub's free plans run five macOS jobs at a time, and queue what they won't start, so two checks is what fits without waiting on GitHub. A command script is one machine's, so one at a time is the safe default.
- **A slot per provider, held for the whole run.** A run whose plan builds on two providers builds on them in turn, so it holds a slot it isn't using. Handing slots back between environments would let a later environment of the same run wait behind another run. Holding them is simpler, and it's what a person reading "2 at a time" expects.
