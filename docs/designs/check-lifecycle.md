# A check's lifecycle: how long a command stays, which build it means, what an environment is, and how it stops

A core item for v0.9.0, [#193](https://github.com/herbygillot/dockhand/issues/193). The issue says where it stands; this file is the design. Architecture wrote it on 2026-11-11, Codex reviewed that draft against the code the same day (the person's "I'd like Codex to co-design and help review as well", 2026-11-11 05:48Z, in the architecture thread), and this is the draft revised for that review, read again at main f45d5b7a3. "What the review changed" lists each correction. It is built once the person adopts it.

## In short

Four things about a check are each decided in several places today. Each gets one value or one rule, made once and passed.

- **Staying.** How long a command stays with a check is chosen by which of four helpers it calls, and how. It becomes one value, `Stay`, and one function.
- **Selection.** `logs`, `artifacts` and `shell` each work out which check, attempt, port and phase they mean. One resolver answers, and a command refuses by name what it can't serve.
- **Identity.** An environment whose identity couldn't be read is treated as one that has none ([#190](https://github.com/herbygillot/dockhand/issues/190)). It becomes one of three: known, none, or unread with its cause. An unread one holds a submission nobody attends.
- **Stopping.** In every provider the work can outlive a driver that is killed, and only GitHub's is ever stopped. A cancel is finished only when each provider has seen its work end, by whichever of three paths the cancel is applied.

Two smaller issues close with it: [#189](https://github.com/herbygillot/dockhand/issues/189), earlier builds loaded where nothing reads them, and [#38](https://github.com/herbygillot/dockhand/issues/38), the Tart minimum.

No flag or command is added. One thing a person sees changes outside dockhand: a GitHub check's branch in the fork is named for the check as well as the commit.

## Staying

**Today,** in `internal/command/checkrun/run.go`:

| Helper | What it does | Called by |
| --- | --- | --- |
| `RunQueued(…, enqueue=false)` | to the end: drives the check here where nobody leads it, follows whoever does | `check`, `check --baseline`, `retry`, `review`, `ship`, and the configured baseline after a check that had no `--for` (check/check.go:289) |
| `RunQueued(…, enqueue=true)` | queues and returns; starts a worker where nobody leads | `check -d`, `check --baseline -d`, `retry -d` |
| `QueuedFor(…, bound)` | queues, starts a worker where nobody leads, follows for the bound | `check --for`, `check --baseline --for` |
| `WaitFor(…, bound)` (:197) | for a check that already runs: reports one that ended; else starts a worker where neither serve nor another command leads it (`LeadOf`, :206), follows for the bound, and refuses as still running | `wait --for` (runcmd/queue.go:71), `check --for` where a covering check runs (check.go:233) |
| `Wait` | for a check that already runs: to the end, driving here where nobody leads | `wait` (queue.go:73), `check` (check.go:235) and `review` (review/reviewpr.go:211) where a covering check runs |

Each command picks a helper by its own flags and by whether admission queued a new check or joined one. That last fact is `checks.Queued.Followed` (checks/queue.go:52); a run doesn't hold it. Two rules live in `check` alone: `check -d` refuses where a covering check already runs (check.go:227, "is already running for these files; dockhand wait check-N follows it"), and under `--for` the configured baseline is queued and said, not followed (:249-263).

**The change.**

```go
// Stay is how long a command stays with a check it asked for.
type Stay struct {
	Detach bool  // -d: queue it and return
	For    Bound // --for: follow it this long; zero is to its end
}

func See(ctx context.Context, e *engine.Engine, queued checks.Queued, streams term.Streams, places config.Places, stay Stay, drain func() (int, error)) error
```

`See` takes what admission answered, so it knows a joined check from a new one. `wait` asks for nothing new and passes its run as joined.

- To its end: drive it here where nobody leads it; follow whoever does.
- Detached: start a worker where nobody leads, and return. `check -d` keeps its refusal of a joined check where it is today, in the command and ahead of `See`, so `See` never turns it into a line that says queued.
- Bounded: never drive it here. Start a worker where nobody leads, follow for the bound, and refuse as still running.
- A check that had already ended is reported, whatever the stay.
- Detached and bounded together is refused by `See`, as the flags refuse it.

`See` chooses among the engine's existing operations. Capacity, leases and who leads stay the engine's and are not decided again. The terminal's reporting, JSON, the exit on Ctrl-C and the words for a worker that couldn't start are kept. The configured baseline under `--for` stays queued and said. `Follow` and `FollowFor` stay as `See`'s two halves: one may drive, the other never does.

`retry` and `review` don't gain `--for`. The value makes that a line each, and it is another issue's to ask.

## Selection

**Today.**

| | Nothing named | A check | One attempt, or a provider's run | Port | Phase |
| --- | --- | --- | --- | --- | --- |
| `logs` (runcmd/queue.go:134) | the branch's newest check in any state | yes | yes, through `RunOrExecution` (engine/status.go:895) | none: every selected attempt's log. With a port: that port's result, failures first (`logs.ForPort`) | `--phase`, `--full` and `--follow` each need a port |
| `artifacts` (runcmd/artifacts.go:19) | the same newest check | `check-N` or a number | not read: any other name is taken for a branch (:36-41) | none: every passed cell of the evidence | none |
| `shell` (engine/shell.go:43) | refused, with a hint of what is kept | yes, by `RunNamed` | not resolved | `--port`; else the first changed | none; `--on` picks the release |

Three resolvers. `RunOrExecution` answers with a check and no single attempt where one provider run is shared by several environments (status.go:905). `logs jq --phase fetch` is refused though `logs jq` works, because the flags are judged before the lone argument is read as a port (queue.go:188-213).

**The change.** One engine function resolves once what was named, and keeps two things about it: the kind of name (nothing, a branch, a check, one attempt, a provider's run) and the attempts it stands for, which may be several. With it go the port, the release and the phase asked for. Each command then says what it serves:

- `logs` serves all of it. The name is resolved before the flags are judged.
- `artifacts` serves nothing named, a branch, or a check. Given one attempt or a provider's run it refuses and names the check: `artifacts lists a check's passed builds; <name> is one attempt of check-N. Run dockhand artifacts check-N`. Its list is of judged passes, some of them reused or filled from earlier checks, so "what this attempt built" is not something it can answer.
- `shell` serves a check. Given one attempt or a provider's run it refuses the same way and names the check and `--on`. It does not gain attempts in this item.
- A port the check didn't build, an `--on` the named attempt didn't run on, and a phase with no port are each refused by name. `shell`'s exit status, its refusal where several clones are kept, and what a running shell does to the evidence are kept.

## Identity

**Today.** `Service.Identity` (checks/service.go:197) passes a read error through. `Service.Identities` (:208) leaves an environment out both when the read failed and when the provider can't say (:211). Its readers then treat an unread environment as one with no identity: evidence takes its results as they were (`current`, evidence/evidence.go:993), and the driver's `anotherCanReuse` (checks/runner.go:394) doesn't count it. The driver itself already keeps them apart for an attempt (`unreadIdentity`, runner.go:1021).

**The change.** `Identities` answers, for each environment, known with its value, none, or unread with the cause.

| Reader | none | unread |
| --- | --- | --- |
| Evidence | results taken as they were, as today | taken as they were, never marked remade, and the words and the JSON say the identity couldn't be read and why |
| A submission nobody attends, `ship --yes` among them | goes on, as today | held, by the concern `identity-unread` |
| A submission a person attends | goes on | says it, and goes on |
| `anotherCanReuse` | not counted | not counted |
| The driver's attempt | builds without reuse | fails the attempt as the environment's and reads again, as today |

The hold is for the environments the submission's evidence rests on, and no others. It names the environment and the cause, and it lifts by itself: every submission reads the identities again. `holds.allow = ["identity-unread"]` lets serve go on. It is judged where a submission is planned (`PlanSubmit`), beside `source-unknown` (engine/servesubmit.go:323), which is the same rule for a source: what couldn't be checked holds. A publication already accepted goes on by its record ([#191](https://github.com/herbygillot/dockhand/issues/191)) and is not judged again.

## Stopping

**Today.** `RequestCancel` (checks/runner.go:214) records the cancel. Three paths then finish it:

| Path | What it stops first |
| --- | --- |
| The canceller, where no driver holds the check (runner.go:239-247) | `stopUnfinished` (:258): each `buildenv.Stopper`, once for each recorded reference, returning at the first failure |
| A driver that takes a check whose cancel is already recorded (:434) | nothing. So after a stop that failed, the next driver, serve's among them, marks the check canceled with its work running |
| The live driver, once its providers return (:513) | whatever each provider's own cleanup did, whose errors are dropped: GitHub's cancel (ghactions/provider.go:272), Tart's stop and delete (tart/provider.go:714, :727) |

| Provider | Reference saved | Does the work outlive a killed driver | A cancel no driver attends |
| --- | --- | --- | --- |
| GitHub Actions | the run's URL, once the run appears after the push (provider.go:263) | yes | cancels the run and returns without seeing it end (:621). A reference that isn't a run's URL is taken as nothing to stop (:598). A driver that died between the push and the URL left a run nothing names |
| Tart | the clone's name, before the clone exists (tart/provider.go:852) | yes | nothing: Tart is no `Stopper`. The clone stays until `clean` removes it (`RemoveLeftover`, :616) |
| Local | the check's directory, before the guest program starts (local/local.go:239) | yes: a process group of its own under a background context (:504), which nothing ends with the driver | nothing. The next check in the prefix waits for it (`awaitOrphans`, :584), and sees it only once it has written results, by a PID with no start time |
| Script | the script's own reference, after its result (script/script.go:201) | yes: a session of its own (session_unix.go:20) | nothing, and nothing recorded names the shell |

**The change, one rule.** A cancel is finished only when every provider reference of the check has answered that its work ended or never started. The three paths become one function: stop, then finish. Until then the check stays unfinished with its cancel recorded, `status` says which work isn't known to have stopped, and `dockhand cancel check-N` asks again, as it can today (runner.go:221 lets a second cancel through).

A provider's `Stop` gives one of three answers, and the service tells them apart:

| Answer | What the provider saw | The cancel |
| --- | --- | --- |
| Ended | the work is over, or was never started, and what it held is released | finishes |
| Ended, something left | the work is over, and a clone, a branch or a changed prefix remains | finishes, and says what is left and what removes it |
| Not known | it asked and hasn't seen the end, or couldn't ask | stays requested, with the provider's words and the command to run |

- `stopUnfinished` asks every provider and reference pair once (two providers may use one string), goes on past a failure, and reports them all.
- It keeps asking finished attempts, because an attempt a driver left is recorded as failed while its work runs. So each provider tells its unfinished work from what it keeps on purpose.
- A worker that lost its lease starts nothing. In every provider the launch follows a `Refer` that succeeded, and `Refer` is a write under the check's lease (runner.go:1381). GitHub's and Script's move ahead of their launch for this; Tart's and Local's are there.
- Three things stay three: Ctrl-C on a command that follows stops the watching; a driver that shuts down leaves hosted work for the next driver (`Build.Canceled` is false); a cancel stops the work.

**GitHub.**

- The check's branch in the fork becomes the check's own: `dockhand-check/<commit>-<check>`, from the first twelve characters of the commit and eight of the check's random ID, the same for its matrix and for every attempt. Today it is named for the commit alone (provider.go:217), and a check that finds the branch already at its commit takes the newest run there (:227, :248). Two checks of one commit, from one Mac or two, can then share a run, and stopping one check's run would stop the other's. `clean` finds the branch by the same name (engine/clean.go:439), so the name comes from one function in `buildenv`.
- Before the push, `Refer` saves the check's place in the fork: the repository, the workflow file, the branch and the whole commit, in a form `Stop` reads. If that write fails, nothing is pushed. The run's URL replaces it when the run appears. Nothing is lost by that: the branch and the commit come from the check's own record and are never read back from the reference.
- `Stop` given the place lists the runs of that workflow for that branch and commit, which the listing already narrows to (ghactions/github.go:78-81), and:
  - cancels each one that isn't completed, then reads them for a short bound. All completed: ended. Else: not known;
  - with none listed and the fork's branch not at the commit, the push never landed: ended;
  - with none listed and the branch at the commit, a run may still appear: not known, until ten minutes after the attempt began. That is `appear` (provider.go:176), the time the driver itself gives GitHub to start a run before it calls the attempt failed. After it: ended, and the cancel says GitHub listed no run.
- `Stop` given a run's URL cancels it and reads it the same way. A run already completed or gone is ended. A URL in another repository is refused, as today (#128). A reference of neither form is an error, where today it is silence.
- The live driver's own cancel stays, for a check that is canceled and not for a driver that only stops. The one rule then asks `Stop` as for anyone else, so its error is no longer dropped.

**Tart.** `Stop` is the removal Tart already has (`RemoveLeftover`: stop the VM, delete the clone, forget its marker), given the clone the reference names. A clone that isn't there never started. A clone a finished attempt kept for `shell` carries the kept marker (`keepClone`, tart/provider.go:507) and is not unfinished work: `Stop` leaves it, so a cancel removes no more than it does today. A stop or a delete that fails is "not known", with Tart's error, and the reference stays for the next ask. `Stop` runs under the check's lease, which `clean` takes or skips the clone for (engine/clean.go:984).

**Local.**

1. `start` records the guest program as it records the driver: its PID and start time, in the check's directory beside `owner.json`, as soon as it has started it. `building` and `orphans` read that record, where they read a bare PID from the guest's results today (local.go:681). That closes both gaps in the table for every reader, the next check among them.
2. A driver killed between starting the guest and writing the record leaves a guest the record doesn't name. The guest's command line names the check's directory (local.go:503), which no other process's does, so where the record is missing the guest is looked for by that.
3. Local is a `Stopper`. It signals the guest's group as the live driver's stop does (local.go:938), only where the recorded process is alive by PID and start time, and waits until it is gone. Then it puts the prefix back with the restore a check already runs (`restore`, :518), under the prefix's lock (own.go:55). Where another check holds that lock, that check puts the prefix back before it builds (`restoreLeft`, :710), and `Stop` leaves it to it.
4. Putting back a prefix a check changed as root needs sudo. `Stop` never prompts: where sudo would ask, it answers "ended, something left", keeps the snapshot and the directory, and gives the words `status` already has for putting it back by hand (`byHand`, :321).
5. Where it finds a directory with no record, no process naming it and results still changing, it answers "not known".

**Script.** It saves its job's directory as a reference before it starts the shell and records the shell there as Local records its guest. `Stop` signals that session's group as its live cancel does (SIGINT, then SIGKILL after the grace, session_unix.go:21) and waits until it is gone. That is the whole promise: the shell and what it started on this Mac. What a person's script started elsewhere is the script's own, and the usage page says so. The script's own reference replaces the directory when its result gives one, as today.

Local and Script share one small helper beside `procid`: record a started child, and end a recorded group and see it gone.

## The two smaller issues

- **#189.** `driver.earlier` (checks/reuse.go:31) is called before every attempt (runner.go:629), and its result is read only before the first attempt of a check that isn't fresh. It is called only there. Its test counts the loads for a fresh check and for a second and a third attempt, and keeps a first attempt that reuses. Installing a dependency from an earlier build still reads what it needs.
- **#38.** The minimum rises from 2.39.0 to 2.41.1 (`ASIFVersion`, tart/images.go:34; the person's "Yes on raising the Tart minimum version", 2026-11-11). What goes: the retry of a listing that raced a delete (`ErrListingRaced`, images.go:84, :148), and `Delete` discarding what `tart delete` itself said (tart/host/machine.go:194). What stays in `Delete`: refusing a VM that runs, and confirming the delete by a second listing. `TooOld` (images.go:69) says the new reason, and setup and the release notes name the new minimum. The Tart on each Mac that runs checks is read before this lands.

## What the review changed

| Codex's point | What was wrong in the first draft | Taken |
| --- | --- | --- |
| L1 | "`artifacts` accepts an attempt and ignores it". It never resolves one | The table; the refusal stays, as a fix to how the name is read |
| L2 | `logs`' default for a port | The table: with no port it prints every selected attempt |
| L3 | "I haven't shown whether a local guest outlives a killed driver" | It does. Local is a `Stopper`, with a recorded guest and a serialized put-back |
| L4 | "Script leaves nothing: it runs inside the driver" | Its shell has a session of its own. Script records it and stops it |
| L5 | `WaitFor` "follows"; `See` reads new or joined "from the run" | `WaitFor` starts a worker. `See` takes `checks.Queued` |
| Choice 1 | A lean | Adopted, with its scope, its name and its way off |
| Choice 2 | A lean | Adopted |
| Choice 3 | Left open | Adopted, as above |
| GitHub's early reference | A branch and a commit, which several checks can share | The check's own branch, the place saved before the push, the bound on an empty listing |
| "Calling a cancel API is not stopping" | `Stop` returned after the call | The three answers |
| The live driver | Only `stopUnfinished` changed | One rule for three paths. The second path, a driver taking a check whose cancel is recorded, is architecture's finding while checking the review |
| Cleanup | A defer | "Ended, something left"; Tart's kept clone is left alone |
| Stay and selection | Detached with bounded; `check -d`; the configured baseline; a shared run | Each said above. The configured baseline under `--for` is already queued and not followed (check.go:249), so nothing is decided there |
| `shell` and attempts | The first draft added them | Trimmed, as the review suggests: a refusal by name is what the issue asks |
| #38 | "Trusting a delete only by the clone's absence goes" | The second listing stays; discarding the delete's own error goes |
| "`RemoveLeftovers` waits for the lease" | It takes the lease or skips | Corrected |

## Choices the person should see

Codex and architecture agree on each. They are here because each changes something the person will notice.

1. **serve holds a submission when it can't read what an environment is now.** Today it goes on. `holds.allow` lets it.
2. **A GitHub check's branch in the fork is named for the check.** More branches can stand in the fork between cleans, one for each check of a commit and not one for the commit.
3. **A cancel of a GitHub check finishes ten minutes after its attempt began if GitHub lists no run.** Nothing proves a run can't appear later. The driver already acts on the same bound.
4. **A cancel stops a local build and a script's shell, and puts a local prefix back.** Today it stops neither.

## Tests

- **Staying,** through the commands: every caller (`check`, the explicit and the configured baseline, `retry`, `wait`, `review`'s joined check, `ship` single and batch); a new check, a joined one, one already ended; to the end, detached, bounded; nobody leading, serve leading, another command holding it. Each row names who drives, what is printed, the JSON, and how the command ends, with Ctrl-C, a wait for capacity, a worker that fails to start, and `check -d`'s refusal.
- **Selection,** through the commands: a branch, `check-N`, a number, an attempt's ID, a provider's URL, a URL shared by a matrix, a name that is pruned or missing, several kept clones, and a release that conflicts. `logs` with no port, with a port and a phase, and following one attempt to its end. `shell`'s exit status and what it does to the evidence.
- **Identity:** the three answers against the readers, with the cause and with a read that was itself canceled; the words and the JSON; an unattended submit and ship held; an attended one that goes on; an unread environment the submission doesn't rest on, which holds nothing; `anotherCanReuse` and the attempt's retry, apart.
- **#189:** the counted loads above.
- **The one rule,** in `internal/checks`: two providers with one reference string, several references with one that fails, a matrix sharing a reference, a second cancel after a failure, and each of the three paths with a stop that fails.
- **GitHub,** with a fake API that holds a run queued, running and cancelling: the place fails to save; the push's reply is lost; a run appears after an empty listing; an older run of the commit; another check of the same commit; another repository; the URL fails to save; the cancel's reply is lost; a cancel accepted and not yet complete; a worker that lost its lease; the branch's guarded removal.
- **Local and Script,** with a real second process: the driver killed after the launch, and before the child is recorded; the survivor found; a PID that was reused left alone; the stop seen through; no sudo. A put-back that fails keeps its snapshot, and a new check in the prefix waits for it. Automated against a throwaway prefix.
- **Tart:** the name saved and no clone; a clone made though the reply was lost; a clone already gone or stopped; a stop that fails; a delete or a marker that fails; a kept clone beside an unfinished one; `clean` and a retry competing for the lease.
- **Acceptance,** since the fault sweep ([#129](https://github.com/herbygillot/dockhand/issues/129)) leaves two real dockhands and the Local and Tart lifecycle outside: G15 and G4 gain a cancel of the check whose dockhand was killed, proving the guest gone and the prefix as it was; a row beside D-I5 cancels a Tart check whose driver was killed and proves the clone gone and its place free; a row cancels a GitHub check whose driver was killed before the run's URL was saved.
- The earlier fixes' tests stay: the bounded baseline, the selected attempt's log, retry's cleanup, a detached cancel.

## How it is built

1. #189, with its counted test.
2. `Stay` and `See`, then one command a commit onto it.
3. The selection, then `logs`, `artifacts`, `shell`.
4. Identity's three answers (#190), then the hold.
5. Stopping: the one rule and `stopUnfinished`, with the service's test; GitHub's branch name in the provider and in `clean` together, then its saved place and what `Stop` sees; Tart's `Stop`; the recorded child, then Local's `Stop` and Script's.
6. The Tart minimum (#38).

After the source comparison ([#192](https://github.com/herbygillot/dockhand/issues/192)), #129's foundation, [#152](https://github.com/herbygillot/dockhand/issues/152)'s rename of `command/runcmd`, and [#197](https://github.com/herbygillot/dockhand/issues/197), which changes how the same GitHub provider polls. [#181](https://github.com/herbygillot/dockhand/issues/181) lands before the last queue cases.

## Not checked

- Nothing here was run. Local's and Script's survival is read from how they launch and from G15, which already kills a dockhand and finds its guest building.
- Whether an ordinary user's `ps` on macOS shows the arguments of a guest that runs as root (Local's step 2), and whether a signal from a second process of the same user reaches a guest under sudo as the driver's own does (step 3).
- How long GitHub can take to list a run after a push it accepted.
- Which Tart the M1 and the M5 run.
- `shell`'s entry and `ForgetKept` beyond their call sites, and every caller of `RunNamed`.

## Usage docs

- `docs/usage/checks.md`: `cancel` stops the work at every provider and says what it couldn't see end; what it does for a local prefix and for a script; `artifacts` and `shell` take a check, not an attempt; `logs <port> --phase` works.
- `docs/usage/unattended.md`: serve holds a branch whose environment it couldn't identify, and `holds.allow = ["identity-unread"]`.
- `docs/usage/setup.md`: the Tart minimum, 2.41.1, where it describes installing Tart; and the name of a check's branch in the fork, where it describes the fork.
- `docs/architecture.md`: `Stay`, the selection, the three answers of a stop, and which providers stop.
