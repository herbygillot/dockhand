# The fault sweep: every outside effect of a workflow failed in turn, and the recovered world compared with a clean run's

A core item for v0.9.0, [#129](https://github.com/herbygillot/dockhand/issues/129), and v0.9.0's acceptance bar beside "everything passes as before", placed at the person's word ("Yes. The rc isn't cut yet, so perhaps we can get that bugfix in.", 2026-11-10 17:28Z, in the architecture thread, approving this design's placements). It is built first when v0.9.0 opens, against the code as it then stands: the two seams, the driver, oracle and normalizer, and the six scratch scenarios below made real; the first run's findings are triaged as bugs. The other scenarios come with the core item whose workflow they sweep, and each item is done when its positions are green. The issue says where it stands; this file is the design, Part 1 of architecture's "A systematic test for two bug shapes" (2026-11-10, read at main 281f8545, at the person's ask in the blind-spots thread, 16:43Z). Its Part 2, the ownership gate, is [#128](https://github.com/herbygillot/dockhand/issues/128); the description bug its scratch run found is [#127](https://github.com/herbygillot/dockhand/issues/127). The five core items it refers to by number are those of architecture's assessment of Codex's review of main b2e7b616 (2026-11-10), which still await the person.

The scratch run's test, its two seams as a diff, and its reports at b2e7b616 and 281f8545 are kept with architecture's design note, outside the repository.

## The two shapes

1. **Acts, then records.** An outside effect (a commit, a push, a pull request update, a restore) is recorded afterwards or in a second transaction, so a failure in between loses state: #113, #116, #117, #118, and W5 to W7 of the review of 2ad896d2.
2. **Outward effects with no ownership test** (the ownership gate, #128, not this item). #112 deleted an adopted pull request's head branch in a repository that is not the person's, MacPorts' included. #115 is its neighbour: cancel never told the provider.

Why the suite misses the first shape: every fix carries its own hand-written failing double (`failingUpdate` in engine/submit_test.go, `failingStore` in engine/history_test.go, `holdFailureStore`, `serveCountFailure`, `failsOn`, `failingWrites` in coord, `failingReviewers` in command/submit), and the acceptance failpoints (`internal/failpoint`) are eight named steps. Both fail only the places someone thought of. This class of bug lives where nobody did.

## The sweep

For each workflow, run it once to count its effects, then once per effect with that effect failing. After each failure, recover as a person would, and compare the world with the clean run's.

### Where it lives, and how it reaches the engine's dependencies

**At the command line, in process**, on `cmdtest`'s World and `cmdtest.Dockhand(t, args...)`. Three reasons:

- Each command line opens the engine afresh, as a process does, so whatever recovery runs when dockhand starts runs here too.
- Recovery is then what a person does: the same command again, or the Next the failure named.
- The composites and follow-ups are in the command layer. #117's visible half, asking the reviewers again, is in `command/submit` (`followUp`, submit.go:314), not in the engine. An engine-level sweep would not see it.

**The plan enters through `compose.Dependencies`**, where every stand-in already enters. One plan object holds the counter and the log, so effects at every boundary share one sequence.

| Boundary | Seam today | What the sweep needs |
| --- | --- | --- |
| Store | none: `compose.Open` opens SQLite itself | a `Store func(store.Store) store.Store` field that wraps the opened store. The engine's services read `e.Store` when they're made (checkservice.go:17, history.go:13, session.go:15), so one wrap reaches them all |
| Forge | `Dependencies.Forge` | nothing; the sweep wraps the forge stand-in's seven write methods |
| Git | none: `*git.Repository` is concrete, and its only seam is the executable | a hook on `git.Program`, asked before each command in `run` and `runStatus` (repository.go:107, :125): one nil check each |
| GitHub Actions | `Dependencies.Actions` | nothing; its pushes go through the git hook |
| Local and Tart providers | `cmdtest.WithScript` | nothing; a provider's run is one effect at the engine's edge. Its inner steps are that package's own sweep, later |

Production code gains one `Dependencies` field and one `Program` field, about thirty lines. The sweep itself is a test package above `compose` (say `internal/command/sweep`), so it imports nothing that imports it.

The hand-written doubles stay where they are, as the regression tests of their bugs.

### Effects are counted by calls, and named by caller

**By calls at the boundary.** A named point exists only where someone put one, which is the weakness being fixed. Counting needs no upkeep: an effect added to a workflow is swept the day it is written, with no test edited.

- **Store:** each `Store.Update` transaction is one effect. It commits or it doesn't, so failing a write inside it is the same case. Event appends are `Tx.AppendEvent`, inside a transaction, and need no separate count.
- **Git:** each writing command: push, fetch, update-ref, update-index, read-tree, reset, switch, add, branch, worktree add, remove and prune, sparse-checkout set and add. Commands that only write objects (hash-object, write-tree, commit-tree, merge-tree) are not effects. A listing test requires every subcommand the git package issues, about 45 today, to be classed read or write.
- **Forge:** Create, Update, MarkReady, MarkDraft, PostReview, RequestReviewers, RerunFailed, and the GitHub CLI's two.
- **Provider:** the Actions API's Rerun and Cancel; a local or Tart run.

**Each effect is named by boundary and caller**, such as `store.update@engine.ApplySubmit`, `git push`, `forge.Update`. Failures print the name, and declarations use it. Nothing refers to an effect by its number.

**Three ways an effect fails:**

- `fail`: it returns an error and did not happen. The workflow's own error handling runs.
- `dead`: it and every later effect fail. As far as the outside world goes, this is a kill just before it; cleanup code runs but can reach nothing.
- `lost`, remote effects only: it happens and the reply is lost.

The named failpoints stay for what only a real kill of the real binary shows (acceptance rows D-I3, D-I4, D-I7, G21). No new ones are needed.

**What makes it systematic rather than as good as its scenarios:** names are call sites, so a listing test requires every effect call site to appear in some scenario's clean run, or to be on a short list with a reason. There are about 80 today: 61 store writes outside the store package, 5 remote git writes, 9 forge writes, 4 Actions calls.

### Invariants

**Generic, one oracle for every workflow:**

1. **The recovered world equals the clean run's.** The world is the GitHub stand-in's state (pull requests, drafts, reviews, re-requests, reruns), every repository's refs (MacPorts', the fork, another person's fork, the clone), the worktree's head, index and status, and every table of the database. This one comparison carries "no hold, limit or count lost" and "no outside effect left unrecorded": an unrecorded effect shows either in the record or in what the rerun then did.
2. **Nothing done twice.** Recovery performs no effect more often than the clean run did, for effects GitHub does not treat as repeatable: Create, PostReview, RerunFailed, Cancel.
3. **A failed run says so.** With an effect failed, the command returns an error, unless that effect is declared best-effort.
4. **Recovery ends.** The rerun finishes, or refuses with a Next, and the world still equals the clean one.
5. **Ownership holds under every fault.** No run changes anything in MacPorts' repository or in another person's.

Comparing two worlds needs four generic normalizations, all in the scratch version: the temporary root, random record IDs, clock times, and commits, each named by its tree and subject.

**Per workflow, as declarations beside the scenario, not code:**

- The arrangement and the command line.
- Recovery where it is not "run it again": the Next's command, `dockhand wait` for a check, serve's restart.
- Best-effort effects, by name, each with its reason. A declaration that names an effect the scenario never makes fails the test, so none goes stale. The scratch run found three: `PlanSubmit`'s note of what took the network to find (submit.go:593, which says so), clean's pull request refresh, and the session's end.
- Equivalences the oracle can't know: checkpoint numbers after an abandoned attempt; the event table's rule (the clean run's events appear in order among the recovered ones, extras allowed); and where recovery legitimately words an event differently, as undo's "recorded the restore ... which a stopped dockhand had made" (history.go:316) or clean's one "removed A, B, C" against two partial lines.
- Checks beyond equality: attempts within `MaxAttempts`, serve's daily limit across midnight, ship's grouped holds.

### What the scratch run found

Six scenarios, 32 effects.

| Scenario | Effects | At b2e7b616 | On main 281f8545 |
| --- | --- | --- | --- |
| First submit | 4 | push event lost when Create fails (#117's cause) | clean, but for the "opened" event below |
| Resubmit after review | 8 | **#117 found unaided**: with `forge.Update` failing, "github.rerequested: clean has, recovered lacks: ryandesign" | #117 gone. **New bug**, below |
| update | 2 | not run | edit left unrecorded, below |
| tidy | 6 | not run | index left stale after a failed `git reset`; not verified |
| undo | 3 | not run | clean: #113's fix holds, and recovery words its event differently |
| clean after merge | 9 | not run | event wording only |

**New bug on main, verified by its consequence** ([#127](https://github.com/herbygillot/dockhand/issues/127)). Submit to an open pull request with a new commit. GitHub takes the updated description, then dockhand's record fails to save (or the update's reply is lost). `dockhand submit` again finishes. From then on every submit says "you wrote the pull request description, and Dockhand will preserve it", and the description is never refreshed again: the next commit does not appear in its table. Cause: the plan decides whose each section is by comparing GitHub's text with the text it last recorded writing (`prdescription.Merge`, submit.go:280). After the failed record those differ, so dockhand's own text reads as the person's edit, and the stale text is recorded again as what it wrote. `--rewrite-description` is the only way out. It is the same family as #117: a retry after a partial submit misreads the state.

**Same shape, milder, verified.** `update jq` edits the Portfile, then its record fails. `update jq` again says "jq is already at 1.8.1; nothing to change", and the edit is never recorded. Tidy then calls it "changes dockhand's commands did not make; review them" and wants an interactive review. This is core item 3's case exactly, and is recorded as that item's agreement case, not filed on its own.

**Reported, not verified:** tidy's rerun leaves the index stale after a failed `git reset` (the failure does name `git reset`); the "opened #N" event is missing from the timeline when the record is retried.

**What it taught about the oracle:**

- Scenarios must be realistic. My first resubmit scenario gave the review no time, and the sweep "found" a missing re-request on main that was the scenario's fault. Checking a finding by its consequence, as above, is part of triage.
- The first run needs triage: four of six scenarios reported something, and about half of it was equivalences to declare, not bugs.

### Does it subsume the tests of the five named core items?

Three of five.

- **Item 2 (publication continuation), item 3 (a preparation result recorded before the work is recoverable), item 5 (a check's lifecycle with its provider): yes.** Their agreement cases are fault positions. The sweep is stricter than the probes, since it fails every position and not the reported ones. The probes stay as named regression tests.
- **Item 4 (a resolved change request with its remaining actions): half.** The sweep covers remaining actions surviving a failure. It does not cover findings 10 to 12, where requested work is dropped with no failure at all (`--source` at the current version). That is a third shape: a request neither done nor refused. Its check is that every flag of a request is acted on or refused by name. Not designed here.
- **Item 1 (one resolved-source requirement): no.** It is four readers agreeing on one fact, and no fault exposes it. Its test is the agreement table the item already names.

So v0.9.0's bar would be: everything passes as before, the sweep is green over the ten workflows, and items 1 and 4 pass their agreement cases.

### Cost in suite time

Measured, scratch, a four-core Linux container, run serially: 73 runs in 62 seconds, about 0.85 seconds a run. Nearly all of that is building the world again, three to six command lines each time. For scale, the engine package's 469 tests take 64 seconds on the same machine.

Projected, the full sweep: ten workflows (ship, bump, submit, serve's submit, tidy, rebase, clean, undo, check, cancel), about three scenarios each, 300 to 400 effects, 700 to 900 runs. The effect count is an estimate until ship, check and serve are written; they are the long ones. That is 10 to 13 minutes of processor time on this container, and each run has its own world, so they run in parallel as the command tests do: about three minutes of wall time on four cores, less on the M5.

If that is too much for every run: `fail` on every run, `dead` and `lost` in the heavy tier. That halves it.

To build: the two seams; the driver, oracle and normalizer, about 500 lines of test; about 15 lines a scenario; and triage of the first full run, which is the unknown.

## What this does not cover

- The third shape: requested work dropped with no failure (item 4's other half).
- Real kills, which stay with the acceptance failpoints.
- Two dockhands at once: leases and sessions are compared, not raced.
- A Tart guest's own lifecycle, until the provider packages get sweeps of their own.
- File writes in the worktree as counted effects. `dead` at the next effect leaves them made and unrecorded, which is the state that matters.
- GitHub's real behaviour. The sweep runs against the stand-in.

## Usage docs

- **The sweep:** none for users. `docs/development.md` gains how to run it, how to add a scenario, and what a declaration is. `docs/architecture.md` names the two new seams on `compose.Dependencies` and `git.Program`.
