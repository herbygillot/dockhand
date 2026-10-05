# Acceptance harness

The release-candidate test, run as scripts, as the project's plan/prime-time.md sets out its rows and plan/acceptance-harness.md the work. Nothing here changes how dockhand itself behaves, and nothing runs against your own state: a run needs `ACCEPT_STATE`, a scratch directory the stage sets up.

```sh
make acceptance                     # the quick stage: tools/acceptance/quick.sh
tools/acceptance/quick.sh --rows A3 # some of its rows
tools/acceptance/quick.sh --image   # once: the stage's own Tart image
tools/acceptance/quick.sh --dry-full  # the full stage's rows, each up to its first host step
make acceptance-selftest
tools/acceptance/full.sh --candidate v0.3.0-rcN   # on the test host, as dhtest
tools/acceptance/resume.sh done     # answer a WAITING checkpoint: done, skip, or fail
```

**The quick stage** (`quick.sh`) builds the dockhand under test from this checkout and makes its environment afresh in `ACCEPT_STATE`, `~/.dockhand-acceptance/quick` unless set:
- its own database, configuration (automatic cleanup off), caches, and worktrees;
- a scratch ports clone whose master, `upstream.git`, is pinned at `ACCEPT_PIN`, a fixed older commit of MacPorts' master, so the same ports are always due and two runs compare. It borrows the objects of `ACCEPT_PORTS_SOURCE`, `~/Source/macports-ports` unless set, which it only reads;
- a local bare fork, `fork.git`, so nothing is pushed anywhere real; and, where `ACCEPT_GH_FORK` names the test account's fork on GitHub (`herbyg-test/macports-ports`), a remote `github` read over HTTPS, which submit's preview finds as the fork. Its push URL is SSH, and the stage's `GIT_SSH_COMMAND` refuses every connection, so nothing is pushed there either; with it, B2's bump runs a whole check, whose guest builds rust and cargo from source on a macOS release MacPorts has no archives for yet, so the quick stage leaves B2 out unless `ACCEPT_B2_BUILD=1`;
- its own Tart homes under `tart/`, dockhand's (`DOCKHAND_TART_HOME`), its SSH keys (`DOCKHAND_SSH_DIR`), and Tart's (`TART_HOME`), kept between runs with the one image the check rows build in. `quick.sh --image` makes it once; a run without it stops and says so.

`upstream.git` borrows its objects through Git's alternates, so a `gc --prune` or a fresh clone of `ACCEPT_PORTS_SOURCE` can take objects it needs; `quick.sh` notices and makes it again.

`lib/guard.sh` refuses any quick run where dockhand's database, configuration, caches, tree, upstream, or Tart homes would be outside `ACCEPT_STATE`, and any full run but the test host's `dhtest` user.

- **A row** is `rows/<ID>.sh`. Its header line `# stages: quick full` says the stages it runs in. It defines `setup`, `act`, `assert`, and `teardown`, which runs whatever came before it, and uses `lib/common.sh`:
  - `dh` and `dh_json` run dockhand, the second once with `--json` for H8;
  - `expect_exit` checks a command's exit status;
  - `allow_change`, `allow_ref_gone`, `allow_push`, `allow_prs`, and `allow_running` say what the row is meant to change;
  - `row_pass`, `row_refused_well`, `row_known`, and `row_fail` give its result.
- **The harm sweep** (`lib/harm.sh`) snapshots between `setup` and `act` and again after `act`, then checks prime-time.md's invariants, H1 to H8, and the quick stage's H9:
  - H1, no work lost;
  - H2, nothing pushed that the row didn't say;
  - H3, no pull request it didn't say;
  - H4, no token written;
  - H5, status matching Git;
  - H6, every `Next:` line accepted, run as its `--plan` where it would change something;
  - H7, nothing left running, and nothing past the row's teardown, allowed or not. Before H7 reads the queue, the runner cancels each check a row left stopped, recorded as running with no live process behind it, and lists it in the row's notes: a row whose command `with_timeout` or a guard cut (`cut_command`) isn't run, with what was cut, and one with no cut fails, never an H7 blocker for a serve the runner stopped (the M1's run at d302e744, B3);
  - H8, every `--json` envelope agreeing with its exit code;
  - H9, in the quick stage, your own `~/.dockhand`, `~/.tart`, and `~/.ssh` as they were. Your own dockhand at work during a run, such as a `serve` agent, trips it too.
  
  A row any of them breaks is a blocker, whatever it said of itself.
- **Results** are `$ACCEPT_STATE/results/<candidate>/<row>.json`, with each invariant's verdict, the row's log, `github_requests`, what the row's dockhands, H6's included, sent GitHub's API, and `github_used_after`, the most GitHub said was spent of the hour's allowance. Each dockhand writes a line to `DOCKHAND_GITHUB_LOG`, which the runner sets to the row's `github.log`; `--json` envelopes carry `github_requests` too, and `-v` says it.
- **The self-test** (`selftest.sh`) runs a harmless row, two that change only what they say, and one made to break each invariant, against a stand-in dockhand, `gh`, and `tart` in `selftest/bin`. Each must be caught by its own invariant alone.
- **Results are labelled** with the rc tag at HEAD where there is one, and HEAD's short commit otherwise, unless `--candidate` names it.
- **The stage's GitHub token** (`ACCEPT_GH_TOKEN`, or `~/.dockhand-acceptance/gh-token`) needs no scopes. GitHub's hourly limit is per account, so the test account's token keeps the stage from spending what your own work, field testing included, draws on.
- **The fault kit** (`lib/fault.sh`) gives rows their faults: `faultproxy`, a proxy that tunnels without decrypting and can stall, cut, or answer 5xx, as `HTTPS_PROXY`; `PATH` shims that hide or age a tool; and a byte flipped, a file truncated, or bad TOML written.
- **Failpoints:** the kill rows run `DH_FAILPOINT_BIN`, built with the `acceptance` tag, where `DOCKHAND_FAILPOINT=<step>:kill` kills dockhand at that step (`internal/failpoint`). A release build has no failpoints, which a test proves.
- **The test host** (`host/`): `provision-host.sh` sets up a Mac once, `reset-user.sh` makes the full stage's `dhtest` user afresh and refuses without the host's marker or as any other user, and `stage-candidate.sh` writes the candidate's Portfile into the overlay. Each is a dry run, saying what it would do, unless given `--run`.
- **The full stage** (`full.sh`) reaches GitHub with the test account's SSH key alone (`ACCEPT_GH_KEY`, which `host/reset-user.sh` copies into dhtest from `ACCEPT_TEST_KEY`), offered with `IdentitiesOnly=yes` and no agent, since the agent's keys may be a person's. It runs on the test host as `dhtest`, against its own ports clone, the dockhand row A1 installs, and the test GitHub account (`ACCEPT_GH_LOGIN`), with the day's small Go and Rust ports (`ACCEPT_GO_PORT`, `ACCEPT_RUST_PORT`). Its test pull requests go to a sandbox, the test account's fork (`ACCEPT_SANDBOX`, which full.sh passes to dockhand as `DOCKHAND_PULL_REQUESTS`), made within it, and the guard refuses a full run without one; only the few the person marks real go to MacPorts. `host/reset-user.sh` makes the sandbox's master MacPorts' again before each run, which needs the token `gh` uses to have Contents and Pull requests, read and write, on the fork: it stops, naming them, where the sync leaves master behind MacPorts'. It then clones the fork as dhtest's `~/Source/macports-ports`, `MACPORTS_TREE`, pushed to over SSH, with MacPorts' own as `upstream`, and says last that a person logs dhtest in, which nothing before it waits on. `full.sh` puts `/opt/local/bin` on dhtest's PATH, and stops where `MACPORTS_TREE` isn't a clone. Its protocol is `lib/protocol.sh`:
  - `checkpoint "<what>"` stops with `WAITING: <row>: <what>`, printed on the run's own output, where `full.sh`'s log has it, as well as in the row's `runner.log` and `$ACCEPT_STATE/waiting`, until `resume.sh` answers `done`, `skip` (the row is not run), or `fail`; `judged "<claim>"` asks a person whether what a row shows holds;
  - `host_only "<what>"` marks a row's first step that needs the host;
  - a row's `# prs: <port> test|real` lines are the pull requests it means to open. Before the first row, run.sh lists them all in `$ACCEPT_STATE/prs.intended` and waits: the person deletes a line to decline it, or changes `test` to `real` for a real update. `submit_pr` opens only an approved one, a test one in the sandbox titled `[testing]` with a `--note` saying it will be closed and `--skip-notification`; `close_test_pr` closes it once the row has its evidence, then runs `clean --closed`.

  In the quick stage and a dry run (`--dry-run`, which `quick.sh --dry-full` passes), a row stops as "not run" at its first checkpoint or host-only step, saying which. The self-test walks every full-stage-only row that way, with nothing of dockhand's run, and answers a checkpoint `done` and `fail`.
