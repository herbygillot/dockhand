# A10 skips setup github and stands the stage's port in for ripgrep

A harness fix, from rc5's rerun on the M1 (Prime-time, 2026-10-19). The
README's walkthrough gained `dockhand setup github` and took ripgrep as its
example port. A10 runs every example as written, and so:

- `setup github` started GitHub's device login, which opened a browser on
  the M1's screen and waited 15 minutes for the code to expire. The stage's
  login was left as it was. A10 now lists it as not run, with why, as it
  does `setup tart`.
- ripgrep is current at the stage's pin (15.2.0), so its examples made no
  branch. A10 now puts the stage's Go port, `$ACCEPT_GO_PORT`, in place of
  ripgrep, as it does for jq: the port's word, a `--new=ripgrep-` name, and
  a `dockhand path ripgrep-` line. The row's own run name then follows, so
  `cd "$(dockhand path ripgrep-update)"` finds the branch that `update
  ripgrep --new=ripgrep-update` started.

No product change.
