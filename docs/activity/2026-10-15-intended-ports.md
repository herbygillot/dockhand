# A branch started for a port is that port's (v0.4.0 part 1, c)

The command-line UX second look's §1 (2026-10-07; added for v0.3.1 at the
person's "Go ahead", 2026-10-10, since `ship -p` builds on it). `start
--port jq` made a branch that `check -p jq` couldn't find, "no tracked
branch changes jq", and `edit jq` from the checkout's root started a
second beside it; `status --port jq` with two such branches said there
were none. A branch's ports came only from what it changes.

- **Schema 32** (`branches.ports`): the ports a branch was started for,
  by name, space-separated, empty for every branch before it.
  `model.Branch.Ports`, validated as names; the store reads and writes it.
- **Every start records them**: `start --port`'s; an authoring verb's
  auto-start, for the port it works on (`startNamed`, `namedOrStarted`,
  `startFor`, and update's start for the version it found); create's new
  port. `StartRequest.Ports`.
- **They're read beside what a branch changes**: `PortBranches`, so `-p`
  and an authoring verb's choice of branch find it; `BranchStatus.Touches`,
  which `status --port` reads; status's Ports line, "none changed yet;
  started for jq", and its PORTS column; and `--json`'s `started_for`.

Tests: TestABranchStartedForAPortIsThatPorts, TestBranchesKeepTheirRules
(the round trip).
