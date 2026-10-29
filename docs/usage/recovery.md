# Recovering a change that stopped

A stopped command does not usually mean its work was discarded. An update can
leave edited files, tidy can leave commits, a check keeps completed results, and
publication can have reached GitHub before the final reply arrived. Begin by
finding what now exists, then choose the operation that addresses that state.

This page assumes dockhand is set up for your ports checkout. Run
`dockhand usage` for setup. Example names such as `jq-update`, `check-42`, and
`jq` stand for the branch, check, and port your output names. Use the actual
reported names when following a diagnosis.

## Start with the branch and the particular check

```sh
dockhand status -b jq-update
dockhand explain -b jq-update
dockhand explain check-42
```

Branch status tells you what is current: edits, commits, checks, review state,
and the next action. Explain develops why a check failed or why the branch needs
attention. Naming `check-42` fixes the investigation to that check;
`logs -b jq-update` instead selects whichever check is latest when you run it.

For the failure's own output:

```sh
dockhand logs check-42
dockhand logs check-42 jq
dockhand logs check-42 jq --phase install
```

The first lists provider runs and log locations; the second selects a build of
jq, preferring a failed build or failed tests over a pass. It names other builds
when the port ran in more than one place. A recorded step lets `--phase` isolate
fetch, dependencies, install, test, or another named step. `--full` includes
dependency-install output otherwise omitted from the port's own build. Use the
provider-run ID the listing gives when you need a specific environment's log.

While it runs, `dockhand logs check-42 jq --follow` follows the host's growing
copy where the provider supports it. GitHub Actions logs arrive differently;
dockhand explains that limitation and points to the workflow page.

## Choose the recovery by what the result says

- **A target failed.** The recorded build or required test step failed. Next:
  Read that port's log and fix the reported cause, or compare with a baseline
  where eligible.
- **A target is blocked.** Another planned target it needs did not pass. Next:
  Diagnose the prerequisite's failure first. A blocked target has not
  independently demonstrated the same failure.
- **An environment is unmet.** It cannot meet a planned requirement, such as
  full Xcode. Next: Supply the required environment or explicitly choose a
  suitable one. Repeating the unchanged request does not add the missing
  capability.
- **An execution had infrastructure trouble.** The VM, connection, or provider
  failed to finish delivering results. Next: Check provider health; repeat the
  same capture once the cause is resolved.
- **The check stopped.** Its driving process ended before finishing. Next:
  `wait` can resume it; `cancel` ends it.
- **The branch is held for review.** A policy concern needs a look; the build
  may already have passed. Next: Inspect the concern, make the needed edit or
  decision, and continue submission.
- **A check is older, incomplete, or no longer stands.** Its results do not
  establish the files or environments required now. Next: Make a new check of
  the intended current files.
- **A batch was partly done.** Some ports advanced and others did not. Next:
  Inspect each reported branch before resuming only the remaining work.

These are different kinds of evidence. A held submission is not a failed
compiler invocation. An unmet environment is not proof the port cannot build. A
completed dependency failure can block several targets without giving each one
its own compiler problem.

## Decide whether you want the old capture or the new files

This choice is often the most important one after an interruption:

```sh
dockhand retry check-42
```

Retry repeats the same files, plan, and environments as check-42, whatever is
now in the branch. It is appropriate after a transient mirror or provider
problem when you want the same experiment again. Applicable earlier results may
still be reused. It does not include the fix you just wrote to the Portfile.

After editing the branch, make a new capture instead. For committed fixes:

```sh
dockhand tidy -b jq-update
dockhand check -b jq-update --head
```

For uncommitted tracked edits in that branch's worktree, explicitly request its
working files when invoking from elsewhere:

```sh
dockhand check -b jq-update --working-tree
```

An untracked patch is not included merely because it is present on disk. Add the
specific file with `--include <path>` or stage it and choose the appropriate
capture. `dockhand usage checks` explains working files, `--head`, `--staged`,
and `--include` in detail.

`--fresh` builds without reusing earlier results when a clean repeat is the
question you need answered. It does not repair an unavailable image, change a
Portfile, or change which test policy you selected.

## Resume, replace, or cancel an active check

```sh
dockhand wait check-42
```

Wait follows a check and, when no other process is driving it, can resume a
stopped one where it left off. A running serve can also take it up. A repeated
`check` for the same files can follow the active check rather than start a
duplicate; read the message naming the check it follows. A request for different
files or a stopped check may instead refuse and tell you what is holding the
branch.

If you want to abandon the active capture and check your current committed fix:

```sh
dockhand check -b jq-update --replace --head
```

Replace stops the old check, keeps completed results, and queues the new
request. To stop without creating another:

```sh
dockhand cancel check-42
```

Ctrl-C has two meanings. When the check runs in the foreground here, it stops
that run and keeps completed results. When another serve process drives the
check and you are only following, it stops following; the check continues. Use
status or wait to see which state you actually have before replacing or
cancelling it.

For work that should survive your terminal or agent invocation ending,
`check -d` queues and returns. Serve drives it, or dockhand starts a background
drain when no serve is leading. Use the check name printed by that command with
wait and logs. `dockhand usage checks` describes the execution model and
`dockhand usage unattended` covers a persistent serve.

## Investigate a build failure or failing tests

Read the first relevant compiler or tool error as well as the final MacPorts
error. Dockhand's summary can quote useful log lines, but the full log has the
preceding context. For a blocked target, find the prerequisite it names and read
that target's failure first.

If your edit caused the failure, fix the branch and make a new check. Where it
is unclear whether the failure predates the branch, ask for a baseline:

```sh
dockhand check -b jq-update --baseline --plan
dockhand check -b jq-update --baseline
```

The baseline builds eligible failed ports at the master commit the original
check started from, in the relevant environments. It does not fetch today's
master and call that the comparison. By default it targets failures at install
or test, including advisory test failures; `--only <port>` can name a port
explicitly. It compares with the branch's newest finished check; `--of check-12`
names an earlier one. It builds with that check's environments, variants, and
tests, so it refuses `--on`, `--build`, `--variants`, `--tests`, and the
capture's flags. A port newly added by the branch has no baseline version to
build. A failure before building, such as an incorrect checksum, usually needs a
Portfile or source investigation instead.

Read both outcomes and their phases. A baseline failure is an observation, not
an automatic acceptance. If the same eligible failure is reproduced, direct
submission can record the appropriate `--accept` and name the baseline in the
pull request. `dockhand usage publishing` explains those acceptances and
standing dependency failures.

Tests use a policy. Under the default `declared`, declared tests run but a test
failure is advisory: the build can pass while reporting failed tests.
`--tests required` makes those failures fail the check; `--tests skip` omits the
phase. Do not interpret a passing default-policy check as proof that every test
passed. Explain and the result table distinguish the two.

## Repair an unmet environment or infrastructure problem

First inspect what dockhand can run:

```sh
dockhand setup
dockhand check -b jq-update --plan
```

Setup reports provider readiness. The plan says where each target can build and
which requirements cannot be met. For example, a port or changed prerequisite
needing full Xcode is unmet in an image without it. Set up the image the message
names, or choose an already suitable environment with `--build` or `--on`. An
image restore or rebuild is an explicit setup operation; retrying a check is not
a request to download and recreate it.

If a guest could not boot, SSH was lost, a script did not return a usable
report, or GitHub was unavailable, examine the provider detail before changing
the port. Dockhand retries infrastructure failures up to its attempt limit while
keeping completed target results. It does not retry a verdict such as a compiler
failure as though that were infrastructure.

Once the external cause is resolved, `retry check-42` repeats that capture. If
you changed the files or need another environment, use a new check with explicit
selections. The provider-specific setup commands, local-prefix behavior, and
timeout settings are in `dockhand usage checks` and
`dockhand usage configuration`.

## A green check can still leave work for review

```sh
dockhand explain -b jq-update
dockhand submit -b jq-update --plan -v
```

An update can change a license, source repository, build requirements, supported
macOS range, or something else a successful install cannot decide. Missing
upstream comparison data is also reported. Bump and serve stop at such concerns
rather than treating a passing build as the whole decision. Commit-rule errors
and new-port lint warnings have their own remedies too; use the printed code or
rule to find the exact requirement.

Use the distinction in the output:

- **Fix the files** when the Portfile should change, then tidy and check the
  resulting files.
- **Record a concern decision** with
  `decide <rule> -p <port> --because "<reason>"` when you have examined a hold
  and can explain why it is acceptable. `--waiting "<what for>"` records why to
  wait and blocks submission until replaced.
- **Accept a recorded failure** with submit's `--accept` only where its
  eligibility rules apply. This is separate from answering an upstream concern.
- **Share incomplete work as a draft** when that is the intended next step; the
  draft still shows the limitations of its evidence.

A direct submission shows concerns for you to weigh. A stored waiting decision
remains a block. Read `dockhand usage publishing` before using acceptance,
no-check submission, or draft readiness, and `dockhand usage unattended` for the
automatic path's holds. Repeating bump without resolving the same hold normally
reaches the same stopping point.

## Recover from stale evidence or a moved branch

A check's files may be older than the branch, an environment may have been
rebuilt, a fetched Git tag may now identify another commit, or required coverage
may be missing. These cases require establishing what should be checked now; the
existence of an old green row is not enough.

For a stale file capture, inspect the branch and make the intended new check. A
rebase changes the base and may require another check, but can reuse individual
builds whose inputs still stand. `retry` repeats the old base and files, so it
is not the normal response to “these files have changed.”

If ship says the branch moved while being checked, inspect the intervening work.
It binds submission to the commit it carried, and will not substitute a newer
one silently. Resume ship for that branch only after deciding which work is
current:

```sh
dockhand status -b jq-update
dockhand diff -b jq-update
dockhand ship -b jq-update
```

A refusal about someone else's push is different: the remote changed since
dockhand observed it. Refresh and inspect the pull request and remote history
before deciding how to integrate that work. Repeating a forced push is not the
recovery dockhand implements.

A saved tidy plan also becomes stale when its bound base, commits, or files
change. Generate a new `tidy --plan` rather than applying the obsolete file. If
a tidy or rebase itself was the operation you want to reverse, checkpoints and
`undo` are explained in `dockhand usage branches`.

## Keep a failed Tart build for investigation

Ask to retain the clone before the check runs. This example requires the Tahoe
Tart image to be set up:

```sh
dockhand check -b jq-update --head --on tart:26 --keep
```

Use the check name it prints. After a guest finishes, its retained clone is
stopped and available for inspection whether its targets passed or failed:

```sh
dockhand shell check-42 -- otool -L /opt/local/bin/jq
dockhand shell check-42
```

The command form works without an interactive terminal; the second form opens a
shell when a terminal is available. The shell starts in the selected port's work
directory, or its staged directory if the build directory is gone. For several
retained releases add `--on tahoe`; for several ports add `--port jq`.

For a test-phase experiment after editing the branch's current test code:

```sh
dockhand shell check-42 --port jq --test
```

This copies the port's current files into the clone and reruns only the test
phase against the existing build. Changes outside that phase are not applied.
Neither a shell experiment nor `--test` becomes recorded check evidence. Make a
new check to validate a change for publication.

Retention is temporary. The branch's next check and clean can remove kept
clones; automatic cleanup can remove them later too. A check that was not run
with keep cannot have its vanished clone recovered by shell. Logs and kept port
archives may still be available through `logs` and `artifacts`.

The local provider has different keep semantics: it leaves the branch's built
versions active in its prefix. `shell` is for retained Tart VMs. Consult
`dockhand usage checks` before choosing local when changing the active ports in
a prefix would matter to other work.

## Resume a partial batch without losing the successful parts

A batch is not one transaction covering every port. Some branches may already be
edited, committed, checked, or submitted when another port refuses. Start with
the batch's per-port result and the current overview:

```sh
dockhand status
dockhand status -b jq-update
```

Inspect the named branch's files and latest check. A branch that reached a good
commit can continue from that stage; there is no need to delete it just because
a sibling operation stopped. `ship -b jq-update` can continue the branch,
following or taking passing evidence where it stands. Use `check` for changed
files and `retry <check>` for the same failed capture, as above. A port already
published belongs to its existing pull request, so continue there rather than
opening a parallel branch by accident.

For scripts and agents, retain the structured result as well as the exit code:
partial completion can mean useful work remains on disk even when the batch
exits nonzero. A review-needed result is not proof that nothing happened.
`dockhand usage batches` covers selections and grouping ports into branches;
`dockhand usage agents` covers JSON and exit codes.

Do not use archive or clean as a generic reset after a failure. Archive pauses a
branch; clean removes ended work and unused build resources.
`dockhand usage branches` explains the previews, what each operation retains,
and when deleting an ended record also discards unmerged work.
