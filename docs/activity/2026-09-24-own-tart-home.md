# 2026-09-24: dockhand's own Tart home

Step 2 of the roadmap's Next; decision 42 of the [contracts
direction](../reviews/2026-09-23-contracts-direction.md), with the home
the person chose.

- `tart.Client.Resolve` defaults to `~/.dockhand/tart`, or
  `DOCKHAND_TART_HOME`, and no longer reads `TART_HOME`: the person's
  `TART_HOME` names their own Tart, and a VM of theirs, such as a running
  ASIF one that keeps Tart from listing (openai/tart#1344), stays out of
  dockhand's view. Dockhand still runs Tart with `TART_HOME` set to its
  own home, Tart's documented setting. The image locks, keyed by the
  canonical home, move with it, and so does the verification pool, keyed
  the same way.
- `tart.PersonalHome` is the person's home, `TART_HOME` or `~/.tart`.
  Dockhand only reads it: to count the person's running VMs, and, in a
  later commit, to find the images an earlier dockhand made there.
- **The person's VMs count toward the limit.** A Mac runs at most two
  macOS VMs, whoever started them. Verification's capacity now counts the
  VMs running in the person's home as well as its own
  (`verify/tart.native.Running`), from a listing that takes and changes
  nothing there. A listing a running ASIF VM blocks counts as that one
  VM, as settled with the person, and a home that does not exist counts
  none. `TestRunningCountsThePersonsVMs` covers the three.
- Test binaries with opt-in live Tart tests pin both homes to the real
  ones (`testsupport.IsolateHomeKeepingTart`).

The images in `~/.tart` stay where they are until setup moves copies of
them (next commits). Until then, the live tests can reach them with
`DOCKHAND_TART_HOME=~/.tart`.
