# Following a running target's log

The first of part 1's last items after v0.4.0's tag (the order in
1ae2e87e), from the gnome-voice session's notes. The README now says
v0.4.0 is released, with main heading for v0.5.0.

- `buildenv.LogName` names a target's log in its build's directory, and
  in a Tart guest, so the engine finds it where the driver writes it. It
  moved from tart's `logName`.
- The Tart driver copies the log of the target the guest is building at
  each read of its results (`liveLog`, through the guest's new `Append`,
  which streams what the guest's file has grown by with `tail -c`). The
  finished log's checked Download replaces the copy.
- `Engine.LiveLog` finds that copy: the newest of the run's build
  directories holding the target's log, by its ID or its port's name.
- `logs <check> <port> --follow` prints the copy as it grows, a second
  between reads, in any process. Once the target's result is recorded, it
  prints the rest from the finished log and ends. A run that ends or stops
  without one says so. Before the build reaches the port, it says it's
  waiting. A check that builds only on GitHub Actions, whose log comes
  back with its run, says it can't be followed and names the run's page.
  `--follow` needs a port, and doesn't go with `--full`.
- The README's known issue on `logs` of a running check is gone.
  usage.md and `logs --help` say `--follow`.
