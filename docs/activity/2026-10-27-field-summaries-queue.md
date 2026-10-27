# A failed build's summary, and a queued check nothing runs

2026-10-27, the first two of field testing's outdated-ports block
(roadmap 933d4f0a), from a six-port batch on v0.5.0.

- **A failed build's summary says the tool's error** (lwan, teleport, and
  wails, whose whole logs the session opened). The guest's `why` kept the
  tool's last three lines before MacPorts' "Command failed:", and under
  make those were make's own. It now passes over make's and ninja's
  failure lines ("make[2]: *** …", "make[1]: Leaving directory",
  "ninja: build stopped", "FAILED: "), a compiler's count ("1 error
  generated."), and CMake's "-- Configuring incomplete, errors occurred!",
  so they're the tool's own last words; base's command stays as base
  printed it. buildlog's likely cause takes "CMake Error at <file>:<line>"
  with the line after it, as it takes rustc's error with its location.
- **A queued check nothing runs says so** (check-660, left queued when
  the session killed a wrapper). status's queue (`view.WriteQueue`, at
  -v) says, of a queued check where no serve or drain leads, "nothing is
  running the queue; dockhand wait check-660 runs it here, dockhand serve
  --drain runs the queue, cancel ends it"; with serve leading it reads as
  before.

The guest's command-failure test keeps a compile's line under make;
TestACMakeErrorIsReadWithItsCause; TestAQueuedCheckNothingRunsSaysSo.
