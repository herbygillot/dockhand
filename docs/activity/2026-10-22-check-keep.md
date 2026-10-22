# check --keep

Roadmap part 1, item 7's kept guest (the person's "Add that as well.",
2026-10-20 03:20Z, to keeping one that passed).

- `model.Plan.Keep`, from `check --keep` through `CheckRequest.Keep` and
  `PlanRequest.Keep`, beside `Fresh`, and `keep` in the plan's JSON.
  Only check sets it; serve's and ship's plans never do. It can't go with
  `--baseline`.
- The Tart provider keeps each environment's clone where its guest
  finished, its targets passed or failed: the deferred delete is skipped,
  the clone is still stopped, and `tart.KeepClone` marks it beside the
  Tart home (~/.dockhand/tart-kept, keyed as the locks are). After
  trouble, where the guest didn't finish, it's deleted as before. A
  kept clone holds no VM slot, only disk, and keeps its name, so its
  execution still names it; it's a `dockhand-check-` clone, never an
  image a check clones from.
- Leftovers carry `KeptAt`, from the mark. Automatic cleanup spares a
  kept clone for `engine.KeptFor`, 24 hours, saying "kept for dockhand
  shell until …", except where it runs because space is low. `clean`
  removes it, and so does the branch's next check (`QueueCheck`'s
  `forgetKept`). Removing a clone takes its mark away.
- check's plan says "Keeps … for dockhand shell; clean or the branch's
  next check removes it".

Next: `dockhand shell check-N`, which starts a kept clone.

## dockhand shell

`dockhand shell check-N [--on <release>] [--port <port>] [-- <command>]`
starts a clone the check kept, runs the command in the port's work
directory (MacPorts' `port work`, in the guest), else its staged
directory, streams what it says, exits with its code, and stops the
clone, which stays kept. With no command, on a terminal, it opens a login
shell there; without a terminal it refuses with no command.

- `engine.Shell` finds the run's kept clone among the leftovers (which
  now carry the `Execution` that made each), chooses by `--on` where it
  kept several, saying the releases to choose from, and takes the first
  port the check changed, or `--port`'s, from the plan.
- `buildenv.ShellProvider`, the Tart provider's: it refuses anything but
  a kept `dockhand-check-` clone, starts it, reaches it by the image the
  kept mark now records, and runs the command through
  `guestssh.Guest.Attach`, which streams both outputs, gives a terminal
  with `-tt` where asked, and returns the command's exit status apart
  from ssh's own 255.
- The help-reference test reads no flags after `--`, which are the
  command's.

Left of the kept guest: `shell --test`.
