# Acceptance harness

The release-candidate test, run as scripts, as the project's plan/prime-time.md sets out its rows and plan/acceptance-harness.md the work. Nothing here changes how dockhand itself behaves, and nothing runs against your own state: a run needs `ACCEPT_STATE`, a scratch directory the stage sets up.

```sh
tools/acceptance/run.sh --stage quick --candidate v0.3.0-rc1 [--rows "A3 B1"]
make acceptance-selftest
```

- **A row** is `rows/<ID>.sh`. Its header line `# stages: quick full` says the stages it runs in. It defines `setup`, `act`, and `assert`, and uses `lib/common.sh`:
  - `dh` and `dh_json` run dockhand, the second once with `--json` for H8;
  - `expect_exit` checks a command's exit status;
  - `allow_change`, `allow_ref_gone`, `allow_push`, `allow_prs`, and `allow_running` say what the row is meant to change;
  - `row_pass`, `row_refused_well`, `row_known`, and `row_fail` give its result.
- **The harm sweep** (`lib/harm.sh`) snapshots between `setup` and `act` and again after `act`, then checks prime-time.md's invariants, H1 to H8:
  - H1, no work lost;
  - H2, nothing pushed that the row didn't say;
  - H3, no pull request it didn't say;
  - H4, no token written;
  - H5, status matching Git;
  - H6, every `Next:` line accepted, run as its `--plan` where it would change something;
  - H7, nothing left running;
  - H8, every `--json` envelope agreeing with its exit code.
  
  A row any of them breaks is a blocker, whatever it said of itself.
- **Results** are `$ACCEPT_STATE/results/<candidate>/<row>.json`, with each invariant's verdict and the row's log.
- **The self-test** (`selftest.sh`) runs a harmless row, two that change only what they say, and one made to break each invariant, against a stand-in dockhand, `gh`, and `tart` in `selftest/bin`. Each must be caught by its own invariant alone.

The quick stage's fixtures, its rows, the fault kit, failpoints, the host scripts, and the full stage's protocol follow, as plan/acceptance-harness.md's H2 to H7.
