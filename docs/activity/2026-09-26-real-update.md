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
