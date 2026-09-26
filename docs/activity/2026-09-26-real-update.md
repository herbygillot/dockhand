# 2026-09-26: a real update of five ports

The roadmap's step 7 waits for v3's path to work end to end on real work.
So five of the person's ports with newer releases were updated with v3:

- broot and nushell, Rust ports whose crate lists move with the version;
- miller, trufflehog, and prometheus, Go ports whose vendored modules do.

It ran in a scratch clone and database, with master fetched from GitHub's
macports/macports-ports (`1c84679`), and nothing pushed. What broke is
fixed here, each with a test.

## `outdated`

`outdated --all` found all five updatable:

| Port | Now | Newest |
|---|---|---|
| broot | 1.60.1 | 1.60.2 |
| miller | 6.21.0 | 6.22.0 |
| nushell | 0.115.1 | 0.116.0 |
| prometheus | 3.14.0 | 3.15.0 |
| trufflehog | 3.97.6 | 3.97.9 |

It took two minutes, 96 CPU seconds, for five ports, and as long again
when repeated. That is noted to measure, not yet explained.

## Fixed

- **`update --outdated` refused more than two ports.** Its help says it
  updates every port named, but the argument check was the single update's,
  a port and a version. It now takes any number with `--outdated`.
- **`update --outdated --plan` ignored `--plan`.** It showed the split and
  then asked to go ahead. It now shows the split and starts nothing.

## The update

`update --outdated --yes` made one branch each from fresh master, each
update tidied into one commit named in MacPorts' style ("broot: update to
1.60.2"):

- **The Go ports** (miller, trufflehog, prometheus) fetch their modules at
  build time (`go.offline_build no`), so only the version and checksums
  moved.
  - miller's and prometheus's `go.toolchain_min` went from `1.25.0` to
    `1.26`, the series their go.mod now requires. That is the Go toolchain
    PortGroup's unit, and MacPorts has `go-1.26`.
  - The upstream comparison listed each go.mod change. prometheus's two
    "adds" are the Azure SDK's `armcompute` and `armnetwork` moving to v8,
    with v5 and v4 dropped.
- **The Rust ports** (broot, nushell) had their crate lists rewritten:
  broot from 571 crates to 555, and nushell from 991 to 983.
- **`submit` had no `--plan`**, though Design v3 says every authoring
  command takes one. Its only preview was running it without a terminal
  or `--yes`, which shows the plan and then refuses. That is an accident of
  prompting, not a promise, and the session's permission rules rightly
  wouldn't run it against the person's fork. `submit --plan` now shows the
  preview and changes nothing, here or on GitHub. It refuses `--check`,
  `--passing`, `--yes`, and `--ready`.
