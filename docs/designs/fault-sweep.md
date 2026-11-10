# The fault sweep: every outside effect of a workflow failed in turn, and the recovered world compared with a clean run's

A core item for v0.9.0, [#129](https://github.com/herbygillot/dockhand/issues/129), and v0.9.0's acceptance bar beside "everything passes as before", placed at the person's word ("Yes. The rc isn't cut yet, so perhaps we can get that bugfix in.", 2026-11-10 17:28Z, in the architecture thread, approving this design's placements). It is built first when v0.9.0 opens, against the code as it then stands: the two seams, the driver, oracle and normalizer, and the six scratch scenarios below made real; the first run's findings are triaged as bugs. The other scenarios come with the core item whose workflow they sweep, and each item is done when its positions are green. The issue says where it stands; this file is the design, Part 1 of architecture's "A systematic test for two bug shapes" (2026-11-10, read at main 281f8545, at the person's ask in the blind-spots thread, 16:43Z). Its Part 2, the ownership gate, is [#128](https://github.com/herbygillot/dockhand/issues/128); the description bug its scratch run found is [#127](https://github.com/herbygillot/dockhand/issues/127). The five core items it refers to by number are those of architecture's assessment of Codex's review of main b2e7b616 (2026-11-10), which still await the person.

The scratch run's test, its two seams as a diff, and its reports at b2e7b616 and 281f8545 are kept with architecture's design note, outside the repository.

Revised 2026-11-10, at the person's word ("Revise all", 23:24Z, in the architecture thread), after Codex's design read of this file at main 9a0214ab and architecture's check of it: the seams are where the code has them, uncertain local outcomes are fault modes, a stopped run's session is made dead, an effect is addressed by call site and occurrence, the oracle is stricter and tested against itself, and the stand-ins get contract tests. The scratch run below stands as what it found; its oracle does not.

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
- The composites and follow-ups are in the command layer. #117's visible half, asking the reviewers again, is in `command/submit` (`followUp`, submit.go:318), not in the engine. An engine-level sweep would not see it.

**The plan enters through `compose.Dependencies`**, where every stand-in already enters, and reaches the engine through two `engine.Options` fields that compose fills. One plan object holds the log, so effects at every boundary share one sequence. The log is safe to write from several goroutines: a check's driver and a session's heartbeat write while the command does.

| Boundary | Seam today | What the sweep needs |
| --- | --- | --- |
| Store | none: `engine.Open` opens SQLite and registers the repository itself (engine.go:400, :424). `Dependencies.Store` is taken; it is the credential store (compose.go:217) | an `engine.Options` field, say `WrapStore func(store.Store) store.Store`, applied in `Open` once SQLite is open and migrated and before the repository is registered. Registration is then an effect; opening and migrating are not, since a database that won't open is a refusal with nothing done. The engine's services read `e.Store` when they're made (checkservice.go:17, history.go:13, session.go:15), so one wrap reaches them all |
| Forge | `Dependencies.Forge` | nothing; the sweep wraps the forge stand-in's seven write methods |
| Git | none: `*git.Repository` is concrete, and `git.Program` is a value that `Options.git()` builds anew wherever one is wanted (engine.go:186), ten places today, `Engine.GitProgram()` among them, which gives discovery its own (preparer.go:55) | a hook field on `git.Program`, asked before each command in `run` and `runStatus` (repository.go:107, :125), one nil check each, and set from an `engine.Options` field inside `Options.git()`, the one place a Program is made. A repository and its worktrees keep the Program they were opened with |
| GitHub Actions | `Dependencies.Actions` | nothing; its pushes go through the git hook |
| Local and Tart providers | `cmdtest.WithScript` | nothing; a provider's run is one effect at the engine's edge. `WithScript` is a command provider, not the Local or Tart lifecycle, which stays outside the sweep (below) |

Production code gains two `engine.Options` fields, the `Dependencies` fields that fill them, and one `Program` field, about thirty lines. The hooks sit beneath `outward`'s rules ([#128](https://github.com/herbygillot/dockhand/issues/128)), not beside them: the sweep sees the effect the rules let through. The sweep itself is a test package above `compose` (say `internal/command/sweep`), so it imports nothing that imports it.

**The seams are tested before any scenario is written.** A write through each thing that holds a store or a Program (the engine's services, the repository, a worktree, discovery's Program, the preparer's) must appear in the log. A Program built somewhere new without the hook then fails a test, where it would have left an effect unswept.

The hand-written doubles stay where they are, as the regression tests of their bugs.

### Effects are found by calls, and addressed by call site

**By calls at the boundary.** A named point exists only where someone put one, which is the weakness being fixed. Counting needs no upkeep: an effect added to a workflow is swept the day it is written, with no test edited.

- **Store:** each `Store.Update` transaction is one effect. It commits or it doesn't, so failing a write inside it is the same case. Event appends are `Tx.AppendEvent`, inside a transaction, and need no separate count.
- **Git:** each writing command: push, fetch, update-ref, update-index, read-tree, reset, switch, add, branch, worktree add, remove and prune, sparse-checkout set and add. Commands that only write objects (hash-object, write-tree, commit-tree, merge-tree) are not effects. A listing test requires every subcommand the git package issues, about 45 today, to be classed read or write.
- **Forge:** Create, Update, MarkReady, MarkDraft, PostReview, RequestReviewers, RerunFailed, and the GitHub CLI's two.
- **Provider:** the Actions API's Rerun and Cancel; a local or Tart run.

**Each effect has an address: its boundary, its call site, and which call of that site it is**, such as `store.update@engine.ApplySubmit#2`, `git push@engine.ApplySubmit#1`, `forge.Update@engine.ApplySubmit#1`. The call site is the first caller outside the boundary's own package. Two transactions in one method are two addresses. Failures print the address, and declarations use it.

**A fault is placed at an address, never at a position in the sequence.** A session's heartbeat is a store write on a real ticker (coord.go:194), and a check's driver writes while the command does, so the fifth effect of one run needn't be the fifth of the next. A run whose chosen address was never reached fails and says so. Heartbeat writes have an address of their own, and the sweep's worlds set the coordinator's interval (`Coordinator.Heartbeat`, coord.go:59) so that none falls inside a run, unless a scenario is about one.

**Five ways an effect fails:**

- `fail`: it returns an error and did not happen. The workflow's own error handling runs.
- `dead`: it and every later effect fail. As far as the outside world goes, this is a kill just before it; cleanup code runs but can reach nothing. Run in process, though, it is an outage and not a death: the stopped run's session carries the test's own process id, so the next command's `Judge` calls it alive (coord.go:274). Before recovery the sweep therefore reports that session's process gone through `Coordinator.Liveness`, the seam that exists for it (coord.go:54). Recovery is then the one a real kill gets, with no waiting out `HungAfter` and no record cleared by the test.
- `lost`, remote effects: it happens and the reply is lost.
- `landed, unknown`, store transactions and local ref updates: it happens, and returns that its outcome is unknown (`store.ErrUncertain`, store.go:60; `git.ErrRefUpdateUncertain`, refs.go:161).
- `not landed, unknown`: it does not happen, and returns the same.

The last two are the store's and Git's own contract. `store.Recorded` (store.go:70) reads a write back when its commit is unknown, at ten call sites, and a ref update does the same. A sweep whose store failure is always a plain error never runs one of them.

The named failpoints stay for what only a real kill of the real binary shows (acceptance rows D-I3, D-I4, D-I7, G21). No new ones are needed.

**What makes it systematic rather than as good as its scenarios:** addresses are call sites, so a listing test requires every effect call site to appear in some scenario's clean run, or to be on a short list with a reason. There are about 80 today: 61 store writes outside the store package, 5 remote git writes, 9 forge writes, 4 Actions calls. A call site only recovery reaches needs an arrangement that reaches it.

**That is coverage of call sites, not of states.** One visit to a shared helper doesn't show that an adopted pull request, a description the person edited, a preparation half done, a detached check and a cancel all recover. Each scenario names the state it is for, and those five are owed whatever the listing says.

### Invariants

**Generic, one oracle for every workflow:**

1. **The recovered world equals the clean run's.** The world is the GitHub stand-in's state (pull requests, drafts, reviews, re-requests, reruns), every repository's refs (MacPorts', the fork, another person's fork, the clone), the worktree's head, index and files, and every table of the database. This one comparison carries "no hold, limit or count lost" and "no outside effect left unrecorded": an unrecorded effect shows either in the record or in what the rerun then did.
2. **Nothing done twice.** Recovery performs no effect more often than the clean run did, for effects GitHub does not treat as repeatable: Create, PostReview, RerunFailed, Cancel. This is read from the effect log, and it counts effects that happened, one whose reply was lost included, apart from attempts that failed.
3. **An unresolved operation says so.** With an effect failed, the command returns an error, unless that effect is declared best-effort, or the code resolved it. A write whose commit was unknown, read back and found (`store.Recorded`), is done, and success is the right answer.
4. **Recovery ends.** The rerun finishes, or refuses with a Next, and the world still equals the clean one.
5. **Ownership holds under every fault.** No write in the effect log, at any point of any run, names MacPorts' repository or another person's. The final state alone would miss a change made and then undone.

**How two worlds are compared:**

- **A commit is named by its tree, message, author, and its parents' names in order**, so its name covers its history. Only the committer's time and the hash are left out. Two commits with one tree and one subject and different parents are different commits; tidy, rebase and undo are where that matters.
- **The worktree is its files:** each tracked and untracked file's content, mode and link target, beside the head and the index. Two different edits both show ` M`.
- **A random record id becomes a name for the record it identifies**, the same name wherever the id appears, so a row that points at the wrong record differs.
- **Times keep their order and their day.** Each is replaced by its rank in the run and the day it falls on. Attempts' order and serve's daily limit read both.
- **The temporary root** becomes a fixed word.
- **The database is compared through a written projection, table by table.** A failed attempt and its retry rightly leave rows a clean run lacks: a session that ended, an attempt abandoned, a journal entry of a step undone. Each such row is allowed by a rule that names its table and the state the row must be in. No rule drops extra rows in general.
- **Events that dockhand reads back as state compare exactly:** the same events, the same number of times, in the same order. `serve.submit` is one, since serve's daily limit counts them (model/session.go:117). A listing test names every event kind that code outside the tests queries. The other events keep the looser rule below.

**The oracle and the stand-ins are tested before they judge anything:**

- **Oracle tests.** Two worlds made different on purpose must compare different: a wrong parent, the same status over different content, a second `serve.submit`, a pull request left as a draft, a row pointing at another record.
- **The forge stand-in becomes a world.** Today its `MarkReady` and `MarkDraft` append to a list and leave the pull request's draft state alone (forgetest/github.go:306, :314), and `RequestReviewers` drops which pull request it was for (:334). Each write method gets a small contract test: the state it leaves, a lost reply, and its count.
- **A stand-in that answers what the real thing doesn't hides the bug.** [#122](https://github.com/herbygillot/dockhand/issues/122)'s fix passed its test on a preparer stand-in that always returns what the real preparer returns only sometimes. The contract tests are how the sweep's own stand-ins avoid that.

**Per workflow, as declarations beside the scenario, not code:**

- The arrangement and the command line.
- Recovery where it is not "run it again": the Next's command, `dockhand wait` for a check, serve's restart.
- Best-effort effects, by name, each with its reason. A declaration that names an effect the scenario never makes fails the test, so none goes stale. The scratch run found three: `PlanSubmit`'s note of what took the network to find (submit.go:642, which says so), clean's pull request refresh, and the session's end.
- Equivalences the oracle can't know: checkpoint numbers after an abandoned attempt; the rule for events not read as state (the clean run's events appear in order among the recovered ones, extras allowed); and where recovery legitimately words an event differently, as undo's "recorded the restore ... an earlier Dockhand process restored it before stopping" (history/history.go:316) or clean's one "removed A, B, C" against two partial lines. Each equivalence is narrow and named; a fault mode is not one of them.
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

**New bug on main, verified by its consequence** ([#127](https://github.com/herbygillot/dockhand/issues/127)). Submit to an open pull request with a new commit. GitHub takes the updated description, then dockhand's record fails to save (or the update's reply is lost). `dockhand submit` again finishes. From then on every submit says "you wrote the pull request description, and Dockhand will preserve it", and the description is never refreshed again: the next commit does not appear in its table. Cause: the plan decides whose each section is by comparing GitHub's text with the text it last recorded writing (`prdescription.Merge`, submit.go:289). After the failed record those differ, so dockhand's own text reads as the person's edit, and the stale text is recorded again as what it wrote. `--rewrite-description` is the only way out. It is the same family as #117: a retry after a partial submit misreads the state.

**Same shape, milder, verified.** `update jq` edits the Portfile, then its record fails. `update jq` again says "jq is already at 1.8.1; nothing to change", and the edit is never recorded. Tidy then calls it "changes dockhand's commands did not make; review them" and wants an interactive review. This is core item 3's case exactly, and is recorded as that item's agreement case, not filed on its own.

**Reported, not verified:** tidy's rerun leaves the index stale after a failed `git reset` (the failure does name `git reset`); the "opened #N" event is missing from the timeline when the record is retried.

**What it taught about the oracle:**

- Scenarios must be realistic. My first resubmit scenario gave the review no time, and the sweep "found" a missing re-request on main that was the scenario's fault. Checking a finding by its consequence, as above, is part of triage.
- The first run needs triage: four of six scenarios reported something, and about half of it was equivalences to declare, not bugs.
- The scratch oracle was too loose. It named a commit by its tree and subject, read the worktree by its status, and let extra events pass. Codex's read made two commits with one tree and subject and different parents, and the scratch normalizer called them equal. The comparison rules above replace it. The findings in the table stand, since each was checked by its consequence.

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

These figures were projected for three fault modes. The two unknown modes add two runs for each store transaction and local ref update a scenario makes, which is most of its effects, so the full sweep is nearer 1,200 to 1,500 runs; inferred, not measured.

To build: the two seams and their tests; the driver, the addressing, the oracle with its projection and self-tests, and the stand-ins' contract tests, perhaps 800 lines of test, up from the 500 first written here and not measured; about 15 lines a scenario; and triage of the first full run, which is the unknown.

The run time above is a planning figure for scenarios not yet written. Before the sweep is called v0.9.0's bar, this file carries the measured time and the list of call sites covered and left out.

### Order of building

1. **The foundation:** the seams and their tests, the five fault modes, addressing, the oracle and the stand-ins with their tests, the six scratch scenarios made real, and triage of what they find.
2. **[#94](https://github.com/herbygillot/dockhand/issues/94), [#16](https://github.com/herbygillot/dockhand/issues/16), [#110](https://github.com/herbygillot/dockhand/issues/110).** None of them needs the whole sweep. #94 comes early, before the scenarios multiply, and scenarios compare state, never a command's printed report.
3. **The remaining scenarios**, each with the core item whose workflow it sweeps. `dead`, `lost` and the two unknown modes run for the release even if they move to the heavy tier.

## What this does not cover

- The third shape: requested work dropped with no failure (item 4's other half).
- Real kills, which stay with the acceptance failpoints.
- Two dockhands at once: leases and sessions are compared, not raced.
- The Local and Tart providers' own lifecycle, until the provider packages get sweeps of their own. `cmdtest.WithScript` stands in for a provider's run, not for a guest.
- What no fault exposes: readers agreeing on one fact (item 1), a modelled context kept apart from the native one (#110), a wrong source chosen (#16). Each keeps its own tests.
- File writes in the worktree as counted effects. `dead` at the next effect leaves them made and unrecorded, which is the state that matters.
- GitHub's real behaviour. The sweep runs against the stand-in.

## Usage docs

- **The sweep:** none for users. `docs/development.md` gains how to run it, how to add a scenario, and what a declaration is. `docs/architecture.md` names the two new seams on `engine.Options` and `git.Program`.
