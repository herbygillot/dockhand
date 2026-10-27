# gopls built in Tart, and the release-notes drafts

2026-10-27, after Then's items 11 to 15 (prioritization's order, a343806b).

- **gopls in Tart**, item 14's done line. The setup:
  - dockhand built at 4be89876, in a clean worktree;
  - a scratch clone of macports-ports at bcba0d8603f;
  - scratch DOCKHAND_DB, DOCKHAND_CONFIG, and worktrees, after the
    field-testing check had freed the VMs.

  `create go:golang.org/x/tools/gopls --new --name gopls-dh` wrote the
  Portfile and filled its checksums. It was named gopls-dh because the
  tree has devel/gopls. `check --on tart` then passed on macOS 26 arm64
  with Xcode: "gopls-dh ✓ (2m 9s)", the build in `build.dir
  ${worksrcpath}/gopls` with `go.offline_build no`, and "links no port's
  libraries".
- **docs/releases/v0.6.0.md and v0.7.0.md**, drafted on v0.5.0's shape.
  - v0.6.0 is to be tagged on rc6, 0dbf87e2, if its acceptance on the M1
    passes. Its known issues are D-N6's tokenless read, and an adopted
    pull request's Portfile evaluated on this Mac, unsaid.
  - v0.7.0's Upgrading says to delete `providers.tart.capacity`, which a
    config is now refused by name for, as any unknown key is. It says
    schemas 43 to 49 migrate on first run, and that unattended means
    serve, or ship or bump with --yes or nobody asked.
  - The acceptance paragraphs and results are placeholders until each
    stage runs. Publishing either waits for the person's typed word.
