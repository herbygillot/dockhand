# The rc1 full stage's findings, after rc2 (v0.4.0)

The v0.4.0-rc1 full stage's findings, relayed by the project's coordinator,
for main before v0.4.0 final. The held update's hint naming `update
--submit` was fixed in rc2 (e2dcb9b4); the rest is here.

- **ship --json says what stopped in a batch.** A change that stops in a
  batch, as one another pull request is open for, goes in `result.skipped`
  with the error as its reason, beside `--mine`'s; before, only stderr said
  it, and the result counted only what went through (S2). One port alone
  stays the command's error, as any refusal before work is.
  TestShipsJSONSaysWhatStopped.
- **The help-reference test reads built strings.** It read only string
  literals, so `"dockhand update " + port` with `"%s --new --submit"`
  in a format call passed after `--submit` had gone. It now renders
  concatenations and format calls (a function named `…f` whose first
  literal argument is the format), following a function's variables that
  hold a command, `+=` included; anything else stands for one word that's
  neither a command nor a flag. Main has no such reference.
  TestBuiltStringsAreRead parses the old hint and finds "dockhand update
  has no --submit".
- **The README's known issues are v0.4.0's.** Eight of v0.3.1's are fixed
  and go: the moved tag (D-N5), the `GH_TOKEN` account, `submit --json`'s
  reviewers, `dockhand path` for a kept merged branch, terraform's stub
  wording, an old Tart in check and serve, a deleted base image's restore,
  and the unclassified guest error tried three times (X2, 4d3d28b8). The
  from-source dependencies item says what the check now says. Codex's
  review of 832f059e's high 2 and high 3, deferred to v0.5.0, join as two
  items, as does `logs` of a running target. pastReferences' `dockhand
  auth` entry went with the GH_TOKEN item. The README's banner and its
  install tag still name v0.3.1; they change with the release.
- **Harness, D-T1:** a `?mode=ro` open can't read the stage's WAL
  database without its -shm. `stage_db` in lib/common.sh copies the
  database with its -wal and -shm and queries the copy, as A5 does.
- **Harness, D-I7:** since 4d3d28b8 an error nothing classifies isn't
  tried again, and the row still expected it to be. A third failpoint
  action, `tart.results:infrastructure`, fails a read as a guest that
  stopped answering, which is tried again; the row now asserts the fault
  and the unclassified error each said at once after one attempt, and
  the infrastructure trouble tried again.
- **Harness, S2:** graded each port asked for as prepared or stopped with
  a reason, rather than 30 prepared: a maintainer's ports include ones
  another pull request is open for, which stop rightly.

A10's go-reflex collision was addressed in 24fae0ec (`-b` and `--new=`
names take a run_name suffix).
