# 2026-09-24: step 1, the defects, done

Step 1 of the roadmap's Next, worked through after the placeholder tag
`v0.0.0-20260924.0`. Each item landed in its own commit with its own
note; this one gathers them and records the checks the Linux sessions'
[handoff](2026-09-23-handoff-to-macos.md) left for a Mac.

| Item | Note |
| --- | --- |
| Startup check split around `mportinit`; Base admitted by version (2.11, 2.12; master only with the preview adapter, in tests) | [version gate](2026-09-24-base-version-gate.md) |
| Golden Gate is Darwin 27; no Darwin that never shipped is modeled | [Darwin 27](2026-09-24-golden-gate-darwin-27.md) |
| A named subport's bump moves its obsolete follower | [follower](2026-09-24-obsolete-follower-authorization.md) |
| Tart through `list` and `get` only; confirmed deletes; name checks; ASIF declined; setup's `tart run` in its own group; locks in `~/.dockhand` | [Tart](2026-09-24-tart-official-interfaces.md) |
| No newer-OS gate | [gate](2026-09-24-no-newer-os-gate.md) |
| `--os` adds to the host's release; publication requires every release | [--os](2026-09-24-os-adds-to-the-host.md) |
| One Command Line Tools generation per release | [tools](2026-09-24-command-line-tools-generation.md) |
| Base master's lazy fetch code, Tcl 9, tests that choose their Base, the parent-side gap pinned | [Base](2026-09-24-base-compatibility-fixes.md) |
| Optional capabilities stated; `errNotImplemented` gone | [capabilities](2026-09-24-optional-capabilities.md) |
| One closed-request rule | [closed](2026-09-24-closed-request-rule.md) |
| The last raw Tcl booleans | [booleans](2026-09-24-tcl-booleans-everywhere.md) |
| An oversized job log kept to its cap; the fork-branch lock key pinned | [log and lock](2026-09-24-job-log-cap-and-lock-key.md) |
| The `publish --adopt` test in `cli`, 32 s to 2.6 s | [test](2026-09-24-publish-adopt-test-in-cli.md) |
| Setup's SSH wait says why it gave up | [SSH](2026-09-24-setup-ssh-wait-message.md) |
| `amend` refuses a checkout on another branch or with unstaged edits | [amend](2026-09-24-amend-refuses-up-front.md) |

## The Mac-only checks

1. **The suite on macOS.** Passes: at the start (1,712 tests, the ten
   skips all opt-in) and after every step. With Base taken only from
   `DOCKHAND_TEST_MACPORTS_TCLSH`, it passes with no MacPorts program on
   `PATH`, and with the variable unset. The patch-failure tests pass
   against BSD patch.
2. **Real Tart acceptance.** `TestRealTartBuildSurvivesSubmittingDriverExit`
   on `dockhand-base-tahoe` passes (115 s). New opt-in contract tests pin
   Tart 2.37.0's listing, stop, delete, and ASIF behavior.
3. **`verify --os` on real images.** `verify tree --working-tree --os
   sonoma` built on Tahoe and Sonoma, both admitted with their own images
   and both passing (4 min 49 s in all), the status listing one verdict
   per release. Publication now requires both.
4. **Linux modeling against a Mac.** Not run: it needs the Linux host.
5. **git-devel.** The dry run found it already current at 2.56.0-rc2 on
   master, through the GitHub API with no fallback to git. A real bump
   would publish to macports-ports, so none was run for this check.
6. **The crate alignment.** A fresh preparation of xan 0.61.0 right-aligns
   all 409 crate lines. The three open pull requests prepared before the
   fix (xan #34844, kasane #34849, fnm #34857) were realigned at the
   person's request with `amend`, each verified again and republished as
   one commit, and their bodies' environment sections rewritten.

## Carried into step 2

The Tahoe images still carry the macOS 27 tools; `setup --check` says so.
They are made again on the 26 generation in dockhand's own Tart home. A
scratch provision showed the rest of setup's new path (the recovery
partition freed on a Command Line Tools image, cleanup on failure) but
could not reach the guest: from an app without macOS's Local Network
permission a Go dial gets "no route to host", while Apple's `nc`
connects, which is why step 2 moves setup to `/usr/bin/ssh`.
