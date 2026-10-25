# The local provider

2026-10-25, roadmap bc94b2bb, the last item before v0.6.0's rc3, asked
for by the person ("I'm curious if we can get this into 0.6.0. I'm fine
restarting acceptance for this."). `check --on local` builds in a MacPorts
prefix on this Mac, /opt/local or one of the person's own, and records
the check like any other provider's. docs/local-provider.md has it for a
person; design-v3 §7's `prefix` text is replaced by it.

## How it's built

- **A prefix is an environment.** model.Environment has a Prefix, empty
  for every other provider, so `--on local:/opt/local --on
  local:/opt/macports-test` is two environments, each with its own
  attempts and evidence. Plans carry it in their JSON; executions in a
  column, schema 41, which rebuilds executions and results (as schema 24
  did) since the uniqueness of a run's environment and attempt takes the
  prefix in. Reuse and archive pruning partition by it. Dependency
  archives aren't keyed by it: local keeps none.
- **Tart's guest program, run here.** tart.Local lives beside Tart's
  provider because it is that program, guest.tcl, under the prefix's
  port-tclsh, and Tart's follower reading its results (Provider.local and
  root, a host guest of plain file reads). guest.tcl changes only where
  input.local is set, so a Tart check takes rc2's path: VerifierProtocol
  stays 3 and the pin moves.
- **The prefix is the person's.** MacPorts reads the staged tree through
  a macports.conf of the check's own, in PORTSRC, whose sources_conf lists
  the tree first and the prefix's own sources after; the prefix's
  etc/macports is never written. It builds as-is unless the prefix is in
  `[providers.local] clean`; the active count and foreign package
  managers are environment facts, and the Tested on line names them.
  `port snapshot` is made first, its ID read back from `--list` by the
  check's note, since `--create` says none. The put-back activates each
  changed port's old version and deactivates what the check activated,
  then deletes the snapshot; `--keep` or `keep = true` leaves the
  branch's versions active and the snapshot standing.
- **Ending early.** The check's directory in /var/tmp, with an owner.json
  naming the process, is the record that a prefix is changed. A stopped
  check puts it back as it ends; one whose process died leaves it, status
  says so with `sudo port restore --snapshot-id N` (engine.Changes,
  buildenv.ChangingProvider), and the next check there puts it back first.
- **Root.** No sudo where the person can write the registry; otherwise
  one `sudo -v` on /dev/tty, then `sudo -n`. Without a terminal it's
  refused, needing a sudo rule with no password. serve skips local unless
  `serve = true` and doesn't list it; an adopted pull request is refused
  on local (engine.localAllowed).
- **subprocess.Spec.Group.** The guest runs in a process group of its
  own, so a terminal's Ctrl-C reaches dockhand, which stops it with
  SIGTERM to the group; its output goes straight to runner.log, so the
  program writes on if dockhand dies. The environment rule holds: every
  program the provider runs goes through subprocess with compose's Env.
- An image of the branch's version installed already would be activated,
  not built, so the check stops, "refused", which follow takes as another
  attempt won't fix it.

## Tested

The guest's local mode against the fake port, which grew a registry of
its own (ACTIVEFILE), snapshots, and PORTSRC's copy: as-is with the put-
back, keep, clean, the refusal, and the restore run. The provider end to
end on a fake prefix whose port-tclsh runs guest.tcl under the test's
MacPorts tclsh: a build put back with its conf as staged, a check stopped
mid-build put back, and a directory left by a dead process reported and
put back by the next check. The engine's: `--on local:<prefix>`, the
refusals, and Changes. Not tested: sudo's prompt, and an Intel Mac,
inferred to work since nothing in it is arch-specific. The four M1 rows
for rc3 are the acceptance.
