# Acceptance harness

The release-candidate test, run as scripts, as the project's plan/prime-time.md sets out its rows and plan/acceptance-harness.md the work. Nothing here changes how dockhand itself behaves, and nothing runs against your own state: a run needs `ACCEPT_STATE`, a scratch directory the stage sets up.

```sh
make acceptance                     # the quick stage: tools/acceptance/quick.sh
tools/acceptance/quick.sh --rows A3 # some of its rows
tools/acceptance/quick.sh --image   # once: the stage's own Tart image
make acceptance-selftest
```

**The quick stage** (`quick.sh`) builds the dockhand under test from this checkout and makes its environment afresh in `ACCEPT_STATE`, `~/.dockhand-acceptance/quick` unless set:
- its own database, configuration (automatic cleanup off), caches, and worktrees;
- a scratch ports clone whose master, `upstream.git`, is pinned at `ACCEPT_PIN`, a fixed older commit of MacPorts' master, so the same ports are always due and two runs compare. It borrows the objects of `ACCEPT_PORTS_SOURCE`, `~/Source/macports-ports` unless set, which it only reads;
- a local bare fork, `fork.git`, so nothing is pushed anywhere real;
- its own Tart homes under `tart/`, dockhand's (`DOCKHAND_TART_HOME`), its SSH keys (`DOCKHAND_SSH_DIR`), and Tart's (`TART_HOME`), kept between runs with the one image the check rows build in. `quick.sh --image` makes it once; a run without it stops and says so.

`upstream.git` borrows its objects through Git's alternates, so a `gc --prune` or a fresh clone of `ACCEPT_PORTS_SOURCE` can take objects it needs; `quick.sh` notices and makes it again.

`lib/guard.sh` refuses any quick run where dockhand's database, configuration, caches, tree, upstream, or Tart homes would be outside `ACCEPT_STATE`, and any full run but the test host's `dhtest` user.

- **A row** is `rows/<ID>.sh`. Its header line `# stages: quick full` says the stages it runs in. It defines `setup`, `act`, and `assert`, and uses `lib/common.sh`:
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
  - H7, nothing left running;
  - H8, every `--json` envelope agreeing with its exit code;
  - H9, in the quick stage, your own `~/.dockhand`, `~/.tart`, and `~/.ssh` as they were. Your own dockhand at work during a run, such as a `serve` agent, trips it too.
  
  A row any of them breaks is a blocker, whatever it said of itself.
- **Results** are `$ACCEPT_STATE/results/<candidate>/<row>.json`, with each invariant's verdict and the row's log.
- **The self-test** (`selftest.sh`) runs a harmless row, two that change only what they say, and one made to break each invariant, against a stand-in dockhand, `gh`, and `tart` in `selftest/bin`. Each must be caught by its own invariant alone.
- **Results are labelled** with the rc tag at HEAD where there is one, and HEAD's short commit otherwise, unless `--candidate` names it.
- **The fault kit** (`lib/fault.sh`) gives rows their faults: `faultproxy`, a proxy that tunnels without decrypting and can stall, cut, or answer 5xx, as `HTTPS_PROXY`; `PATH` shims that hide or age a tool; and a byte flipped, a file truncated, or bad TOML written.
- **Failpoints:** the kill rows run `DH_FAILPOINT_BIN`, built with the `acceptance` tag, where `DOCKHAND_FAILPOINT=<step>:kill` kills dockhand at that step (`internal/failpoint`). A release build has no failpoints, which a test proves.
- **The test host** (`host/`): `provision-host.sh` sets up a Mac once, `reset-user.sh` makes the full stage's `dhtest` user afresh and refuses without the host's marker or as any other user, and `stage-candidate.sh` writes the candidate's Portfile into the overlay. Each is a dry run, saying what it would do, unless given `--run`.
