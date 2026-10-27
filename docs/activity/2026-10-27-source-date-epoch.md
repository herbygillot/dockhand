# A Portfile reading source_date_epoch plans again

2026-10-27, for rc6 at the person's choice: Link Dupont's report on
macports-dev (046989), reproduced on main at 43b81539 and so in rc5 and
v0.5.0. `dockhand bump syft` and `update syft --plan` refused: "version
probe is inconclusive: modeled context depends on host state: a host
process executed while evaluating the Portfile".

- **base's git probe** (prioritization's narrowed form of implementation's
  first option). syft's Portfile reads `${source_date_epoch}` in its build
  arguments, whose default in base 2.12.7 is
  `portmain::get_source_date_epoch` (portmain.tcl:169-248): git's
  `rev-parse --is-inside-work-tree` of the port's directory and its
  PortGroups', then `status` and `log` only within a work tree, else the
  files' modification times. A tree dockhand evaluates is a snapshot with
  no .git (workspace.Open), so the dispatcher answers that one question
  of a directory in it as git does there, not a work tree, without a
  process (toolchain.tcl's `work_tree`), in every observed context, since
  git's answer is the same in the Mac's own; base then takes the
  modification times, reads the observation admits. Any other process,
  and the same question of a directory outside the tree, still refuses.
- **Tcl's clock.** uv and cloudflared format it, `[clock format
  ${source_date_epoch} ...]`, and clock's first use reads Tcl's own
  library and probes the Mac's zoneinfo and /etc/localtime. An observed
  worker now sets `TCL_TZ` to `:UTC` and loads clock before the Portfile
  runs, and a read in Tcl's own library is the interpreter's, not the
  host's. dockhand's fingerprint already reads source_date_epoch as
  unstable, so its value decides nothing compared.

Planned with a scratch database against the main checkout: syft
1.54.1 → 1.55.0, uv 0.13.4 → 0.13.6, cloudflared 2026.10.3 → 2026.10.4;
golines, socket_vmnet, ctop, and pdfcpu are current and say so.
TestSourceDateEpochIsReadFromTheCapturedTree reads the Portfile's time,
formatted in UTC, with no host event, and the same question of a
directory outside the tree still reads as the host. uv's and
cloudflared's refusal, the clock's, was found running these ports, not
in prioritization's placement, which named cloudflared with syft.
