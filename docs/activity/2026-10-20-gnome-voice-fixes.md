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
- A guest was offered the kept archives of a target's direct dependencies
  and of every port an earlier result had active. gnome-voice's line named
  about 230, libadwaita, geoclue2, and gst-plugins-bad among them, which
  its version no longer needs, and a first check missed its dependencies'
  dependencies. `dependencyInstalls` (engine/reuse.go) now offers the
  dependency closure of what the guest builds, at the check's revision.
  That's each target's direct dependencies as MacPorts evaluates them, and
  theirs on from the port index (`DependencyGraph`), never an earlier
  result's active ports. Where the index can't be read, the direct
  dependencies alone are offered. The index names ports in lower case, so
  `DependencyArchives` matches a port's name in any case. The line before
  the build is a count, "the guest may install up to N dependencies from
  archives earlier checks kept", and each target's build says what it
  installed from them, by the digests the guest reported active,
  "harbor-viewer: installed 2 dependencies from archives earlier checks
  kept: cargo, rust". That's a progress line beside the result, which the
  check's output and journal carry, not a field of the stored result.
- Field testing's check-484 of gnome-voice, on 78da24cf, read all three.
  Its widest status line was 121 columns, `logs` said the target was
  building, and none of the three ports was offered. The count said "up
  to 221 dependencies" where 223 archives were given: freetype and
  p5.34-uri each had their two newest kept archives offered
  (`dependencyArchivesPerPort`). The line now says the archives too where
  they differ: "up to 221 dependencies from archives earlier checks kept
  (223 archives, the newest two of some)" (`offeredWords`).
