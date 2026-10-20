# The gnome-voice notes' fixes for main

Next's fixes from the gnome-voice session's field notes (placed in
06987cab and 21ca218f), taken while v0.4.0's tag waits.

- status's "Needs you" no longer shares one tabwriter across its rows.
  One long message had set every row's command column, which ended near
  column 400. Each row's message now wraps at the terminal's width (80
  where the output isn't a terminal) beside its branch, with its command
  on the line below (`writeAttention`, command/status.go;
  `Streams.width`, `terminalWidth`).
- `logs <check> <port>` for a target still building said "recorded no
  log", which read as a failure. `RunLogs` now carries a running run's
  latest progress, and `RunLogs.Building` reads the target and step from
  it as the Tart provider words them. logs says "gnome-voice is building
  in check-482 (configure); its log comes back when it ends, and dockhand
  wait check-482 follows it". A target not reached yet is said as such.
  The README's known issue is marked fixed on main.
