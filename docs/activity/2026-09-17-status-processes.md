# The live status table processes work

Decided with the user on 2026-09-17: the live `status` table is a processor, not only a viewer. A person sitting in front of the table wants the rows to move, and until now they only moved if some other process was driving.

## What changed

`liveStatus` builds the full services rather than the read-only reader and hands the table a `Processor` closure that runs the driver loop (`Processes.Run` over the whole repository) for as long as the table is open. The header reads `dockhand status (processing)`. The loop's progress reports pass through the usual level filter into the message strip, and cycle problems land there prefixed by job. Quitting the table cancels the loop and waits for it to return before the services close; accepted work stays recorded for the next `status`, `wait`, or `start`. The table polls the engine's own status reader every two seconds, so rows follow the cycles.

The keys that start work, `b`, `v`, and `p`, now run their verb with `--detach`: the command returns at admission and the table's processing carries the job on, so a key never blocks the table. `r`, `c`, and `a` finish inline as before. The `w` key is gone, since the table itself is the wait.

`--print` replaces `--plain`: it prints the snapshot once and processes nothing. `--json` and non-terminal output imply it, and those three paths remain the read-only status that scripts rely on.

## Exercise

Driven through a pseudo-terminal with `expect` against the real ports tree: the header showed `(processing)`, the table rendered, and `q` stopped the loop and exited cleanly with no pending claims left behind. The processor's start, reporting, and stop are unit tested without a terminal.
