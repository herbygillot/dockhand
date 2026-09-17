# Interface pass, step 5: the live status table

Per the [output design](../output.md), `status` on a terminal is now a live table built with Bubble Tea (`internal/tui`).

## What it does

The table polls the contribution projection every two seconds and renders one row per contribution: port, change, phase, state, next. Columns fit the terminal width, the widest fixed column giving way first and NEXT keeping a readable minimum; rows scroll around the selection when there are more than fit. Enter expands the selected row to its branch, targets, contribution ID, PR, log location, active job with its last detail, and the job history with IDs and times.

Keys map onto the existing verbs: `w` wait, `v` verify, `p` publish, `r` refresh, `c` cancel, `a` abandon, `o` open the PR, `l` open the failed build's log, `q` quit. A verb runs the corresponding dockhand command in-process on this runtime's configuration, selecting the row's contribution exactly by `--change`, or by `--job` and port for standalone work, so a key has the authority of the command and nothing more. Verify, publish, cancel, and abandon ask `y/n` first. The command's output streams into the message strip at the bottom, prefixed by port; one verb per contribution runs at a time, and the snapshot is reread when it finishes.

The table is inline rather than an alternate screen, so the last frame stays in the terminal after `q`. Non-terminal output, `--plain`, `-v`, and `--json` keep the plain forms from step 4.

## Exercise

Driven through a pseudo-terminal with `expect` at 120 columns against the real ports tree: the table listed the recorded contributions with fitted columns, took `j`, Enter, and `q`, and exited cleanly. The model's key handling, confirmation flow, verb arguments, and expansion are unit tested without a terminal.

## Friction items closed

From the [new-user exercise](../reviews/2026-09-16-new-user-deno-exercise.md): item 22 in full for `status`, and the design's "realtime table" request. The interface pass is complete; remaining friction items (first-run cost announcement, amend/rebase help, `setup --check` listing) are outside the output contract.
