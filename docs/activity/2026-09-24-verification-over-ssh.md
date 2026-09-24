# 2026-09-24: verification reaches its clones over the channel

Step 2 of the roadmap's Next; decision 42 of the [contracts
direction](../reviews/2026-09-23-contracts-direction.md): verification
talks to its guest only over SSH, so `tart exec`'s lost output (#1347),
wedged control socket (#1346), and buffered input (#1348) cannot reach
what dockhand reads back.

- **Reaching a clone.** A clone is reached at the address `tart ip` gives
  and held to the host keys setup recorded for the image it was cloned
  from, with dockhand's key (`verify/tart.native.reach`).
- **Ready.** It waits for the guest agent, marks the clone with a random
  token through `tart exec`, which reaches exactly the named VM, and reads
  the token back over SSH, which proves the address is that clone's and
  not an earlier VM's lease; then it checks that SSH carries input,
  output, and exit status (`host.CheckGuestTransport`). `exec` is used
  only to write, never to read.
- **Staging** uploads the input archive, checked by size and sha256, and
  unpacks it in the guest. **Launch** is a command. **Inspect** reads
  `result.json`, the verdict, as a checked transfer, and the runner's
  state as one word. **Logs** copies the build and runner logs out as
  checked transfers; `--trace` reads the running build's log by checked
  ranges.
- **The Mac's VM limit.** A clone that stops before its agent answers
  says why, from its run's log. One Tart refused because the Mac already
  runs two macOS VMs (some started where neither home shows) is
  withdrawn: stopped and deleted, its request released with no result,
  which closes it to submission with nothing left, so reconciliation
  reports it closed with no resources and the workflow queues a new
  submission, waiting as it waits for capacity. Reconciliation now
  reports any request released without a result that way, since its
  clone is already gone.
- `host.Machine.Ready`, the exec-based wait and transport check, is gone.

Tests: status, logs, and log ranges run against a stand-in `ssh` over a
local guest directory, including a result published between reads;
`TestReadyNamesTheMacsVMLimit` and `TestAClonePastTheMacsVMLimitIsWithdrawn`
cover the limit. The real acceptance test runs once the images live in
dockhand's own home.
