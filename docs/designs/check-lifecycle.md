# A check's lifecycle: how long a command stays, which build it means, what an environment is, and how it stops

A core item for v0.9.0, [#193](https://github.com/herbygillot/dockhand/issues/193). The issue says where it stands; this file is the design. Architecture wrote it on 2026-11-11, Codex reviewed that draft against the code the same day (the person's "I'd like Codex to co-design and help review as well", 2026-11-11 05:48Z, in the architecture thread), and this is the draft revised for that review and for its short second pass, read again at main 3cb1dc8d7. "What the review changed" lists each correction. It is built once the person adopts it.

## In short

Four things about a check are each decided in several places today. Each gets one value or one rule, made once and passed.

- **Staying.** How long a command stays with a check is chosen by which of four helpers it calls, and how. It becomes one value, `Stay`, and one function.
- **Selection.** `logs`, `artifacts` and `shell` each work out which check, attempt, port and phase they mean. One resolver answers, and a command refuses by name what it can't serve.
- **Identity.** An environment whose identity couldn't be read is treated as one that has none ([#190](https://github.com/herbygillot/dockhand/issues/190)). It becomes one of three: known, none, or unread with its cause. An unread one holds a submission nobody attends.
- **Stopping.** In every provider the work can outlive a driver that is killed, and where no driver attends, only GitHub's is stopped. A cancel is finished only when each provider has seen its work end, by whichever of three paths the cancel is applied. Launching and stopping an attempt exclude each other by a lock, and what a provider launched is kept apart from the reference a person looks at.

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

The hold is for the environments the submission's evidence rests on, and no others. It names the environment and the cause, and it lifts by itself: every submission reads the identities again. It is judged where a submission is planned (`PlanSubmit`), beside `source-unknown` (engine/servesubmit.go:323), which is the same rule for a source: what couldn't be checked holds. Like that one it is no rule `holds.allow` may name (:322). The way on is the read succeeding, or the person's own submit. A publication already accepted goes on by its record ([#191](https://github.com/herbygillot/dockhand/issues/191)) and is not judged again.

## Stopping

**Today.** `RequestCancel` (checks/runner.go:214) records the cancel. The check gets no state of its own for it: it stays running, with the cancel recorded. Three paths then finish it:

| Path | What it stops first |
| --- | --- |
| The canceller, where no driver holds the check (runner.go:239-247) | `stopUnfinished` (:258): each `buildenv.Stopper`, once for each recorded reference string, returning at the first failure. An attempt with no reference yet is never asked |
| Any driver that takes a check whose cancel is already recorded (:434) | nothing. serve's loop, the background worker and `dockhand wait check-N` all come this way. So after a stop that failed, the next of them marks the check canceled with its work running. `status` meanwhile shows the check as stopped and says to run that `wait` |
| The live driver, once its providers return (:513) | whatever each provider's own cleanup did, whose errors are dropped: GitHub's cancel (ghactions/provider.go:272), Tart's stop and delete (tart/provider.go:714, :727) |

| Provider | Reference saved | Does the work outlive a killed driver | A cancel no driver attends |
| --- | --- | --- | --- |
| GitHub Actions | the run's URL, once the run appears after the push (provider.go:263) | yes | cancels the run and returns without seeing it end (:621). A reference that isn't a run's URL is taken as nothing to stop (:598). A driver that died between the push and the URL left a run nothing names. `Stop` is given the attempt, which carries the check's ID and not its commit, and the provider reads no store |
| Tart | the clone's name, before the clone exists (tart/provider.go:852) | yes | nothing: Tart is no `Stopper`. The clone stays until `clean` removes it (`RemoveLeftover`, :616) |
| Local | the check's directory, before the guest program starts (local/local.go:239) | yes: a process group of its own under a background context (:504), which nothing ends with the driver | nothing. The next check in the prefix waits for it (`awaitOrphans`, :584), but sees it only once its results carry its PID, which is after its snapshot is made, and with no start time. Before that the next check takes the directory for one nobody builds in and removes it (:724) |
| Script | the script's own reference, after its result (script/script.go:201), and nothing before | yes: a session of its own (session_unix.go:20) | nothing, and nothing recorded names the shell |

Two more things the code does with such a check: a submission of the same files waits on it as a check still pending (engine/submit.go:689), and nothing `status` prints says a cancel was asked.

**The change, one rule.** A cancel is finished only when every attempt of the check has answered that its work ended. The three paths become one function: stop, then finish. Until then the check stays unfinished with its cancel recorded.

A provider's `Stop` gives one of three answers. It returns what is left, or an error: no error and nothing left is ended; no error and something left is ended, with each thing named; an error is not known.

| Answer | What the provider saw | The cancel |
| --- | --- | --- |
| Ended | the work is over, or nothing was launched and nothing can be, and what it held is released | finishes |
| Ended, something left | the work is over, and a clone, a branch or a changed prefix remains | finishes, and says what is left and what removes it |
| Not known | it asked and hasn't seen the end, or couldn't ask | stays requested, with the provider's words and the command to run |

- `stopUnfinished` asks every attempt that launched or may have, once for each provider and launch (a matrix's cells share one), goes on past a failure, and reports them all.
- It keeps asking finished attempts, because an attempt a driver left is recorded as failed while its work runs. So each provider tells its unfinished work from what it keeps on purpose.
- While it isn't finished, `status` says a cancel was asked, which work isn't known to have stopped, and that `dockhand cancel check-N` asks again. It no longer offers `wait`. A check whose cancel is recorded is no pending check for a submission, as it is none for queueing today (`BranchCheck`, model/run.go:104).
- serve's loop and the worker ask again when they take the check, no more often than a provider polls.
- Three things stay three: Ctrl-C on a command that follows stops the watching; a driver that shuts down leaves hosted work for the next driver (`Build.Canceled` is false); a cancel stops the work.

**What a provider launched is kept, apart from the reference.** An attempt gains one field, `Launch`: the provider's own note of what it is about to start, in a form its `Stop` reads. It is saved before anything is started, and `Refer` never replaces it. The reference stays what it is today, the place a person looks. `Stop` still takes the attempt, which now carries both. This is the answer to what the first revision got wrong: it said GitHub's branch and commit could be worked out again from the check's record, and `Stop` has no such record to read.

**Launching and stopping exclude each other.** The first revision said a worker that lost its lease starts nothing, because every launch follows a `Refer` under the lease. That doesn't hold: a worker can pass its `Refer`, be paused, lose the lease, and wake straight into its launch, after a canceller saw nothing and finished. A lease read again only moves the gap.

- Every provider launches through one call the driver gives it. The call takes an operating-system lock for the attempt (`internal/filelock`), reads again under it that the check is still this driver's and that no cancel is recorded, saves `Launch`, and only then runs the provider's start. It lets go when the start has returned.
- `Stop` takes the same lock before it reads `Launch`.
- So `Stop` meets one of two things. A live process is still launching: not known, naming that process, and nothing is finished. Or `Stop` holds the lock: then nothing more can be launched for that attempt, because a launcher that gets the lock later reads the cancel and starts nothing. The cancel is recorded before any `Stop`.
- The lock is never taken from a live process. A driver that hangs inside its launch keeps the cancel unfinished until it is stopped, and the words say which process.
- A child that could finish the launch after its driver died holds the lock too: Tart's clone is handed the file, as its image lock is (tart/host/machine.go:38). Local's guest and Script's shell are not, since they run for the whole build; they are found by what they record of themselves, below.

**GitHub.**

- The check's branch in the fork becomes the check's own: `dockhand-check/<commit>-<check>`, from the first twelve characters of the commit and eight of the check's random ID, the same for its matrix and for every attempt. Today it is named for the commit alone (provider.go:217), and a check that finds the branch already at its commit takes the newest run there (:227, :248). Two checks of one commit, from one Mac or two, can then share a run, and stopping one check's run would stop the other's. `clean` finds the branch by the same name (engine/clean.go:439), so the name comes from one function in `buildenv`.
- The name keeps two checks' branches apart. It proves nothing. A branch found at the commit is taken as this check's only where one of the check's own attempts has a `Launch` that says it pushed there. Otherwise the attempt fails and names the branch.
- `Launch` is the repository, the workflow file, the branch, the whole commit, the check's whole ID, and the newest run already listed there. It is saved before the push. With no `Launch`, nothing was pushed for the attempt.
- `Stop`, holding the lock:
  - no `Launch`: nothing was pushed and nothing can be. Ended;
  - runs listed for that workflow, branch and commit, newer than the one noted: it cancels each that isn't completed, then reads them for a short bound. All completed: ended. Else: not known, naming the runs;
  - none listed: **not known**, however long it has been. GitHub accepted a push, and nothing shows a run can't still start. The branch being gone or moved doesn't show it either: the push can have landed and started a run before that. Each later ask looks again, and a run that appears is canceled then.
- The ten minutes the driver gives GitHub to list a run (`appear`, provider.go:176) stay what they are: the driver's patience, counted from its own first look after the push, after which it reports trouble and tries again. They are not evidence that no run exists, and `Stop` doesn't use them.
- An attempt recorded before this design has a run's URL and no `Launch`. `Stop` cancels that run and reads it the same way. A URL in another repository is refused, as today (#128). A reference of neither form is an error, where today it is silence.
- The live driver's own cancel stays, for a check that is canceled and not for a driver that only stops. The one rule then asks `Stop` as for anyone else, so its error is no longer dropped.

**Tart.** `Stop` is the removal Tart already has (`RemoveLeftover`: stop the VM, delete the clone, forget its marker), given the clone the reference names. Where no clone remains, and `Stop` holds the lock so none can still be made, it is ended. A clone a finished attempt kept for `shell` carries the kept marker (`keepClone`, tart/provider.go:507) and is not unfinished work: `Stop` leaves it, so a cancel removes no more than it does today. A stop or a delete that fails is not known, with Tart's error, and the reference stays for the next ask. `Stop` runs under the check's lease, which `clean` takes or skips the clone for (engine/clean.go:984).

**Local and Script: which processes, and when they are gone.**

Both start one process in a group of its own and leave it running where the driver dies. What is launched isn't always what builds: where the prefix is root's, Local launches `sudo`, and the guest program is sudo's child (local.go:503). Whether that child stays in the group dockhand made depends on the sudo. So the rule doesn't lean on it.

1. **The child records itself, first.** The guest program's first save carries its PID, its process group and the time by its own clock; today its first save has no PID, and the one that has comes after the snapshot (guest.tcl:1293, :1402). The script's shell writes its own PID into its job directory before the person's script runs; the shell leads its session, so that is its group. There is then no moment when a child is running and unnamed: either its record is there, or it hasn't done its first act.
2. **A recorded number is told from a reused one** by time. A live process with a recorded number, the child's or its group's, is the build's only where the system says it started no later than the child's record was made. `procid` reads that start time today for one process (procid/start_darwin.go:15).
3. **It is gone when its group is empty**, not when one process is. A signal of zero to the group answers that with no privilege, for root's processes too. The group's leader is the process whose number the group carries: the guest or the shell, or the sudo above the guest. A number isn't handed out again while a group still carries it, so:
   - the leader runs, and is the build's by rule 2: the group is the build's;
   - a process has the leader's number and is not the build's: the build's group emptied before that process began. Ended;
   - the leader is gone and the group has members: they are the build's children, or a later group that took the number and lost its own leader. The two can't be told apart. Nothing is signalled, and the answer is not known, naming the processes, until the group is empty.
4. **Only a group whose leader was just checked is signalled.** The stop the live driver uses: SIGTERM to the group for Local, SIGINT for Script, then SIGKILL after the grace, each sent while the leader still checks out (subprocess.go:136, session_unix.go:21).
5. **Root's group may need root.** The code's comment says sudo passes the signal on to the guest (local.go:934); nothing tests whether the person's own process may signal a root sudo at all. The real-process test settles that first. Where the signal is refused, `Stop` sends it with `sudo -n` and never prompts; where sudo would ask, the answer is not known, with the one command to run by hand and `dockhand cancel check-N` to run after it. The live driver's stop gets the same treatment, since it sends the same signal the same way.
6. **No record at all.** `Launch` says a start was begun, the launcher is dead, `Stop` holds the lock, the child never made its first save, and no process's command line names the directory. Then the child didn't run or ended at once: ended. This is the one answer here that rests on things being absent, and it needs all of them. Where the process list can't be read, or can't show another user's arguments: not known.

Local and Script share one small helper beside `procid` for 2 to 4. `building` and `orphans` (local.go:681, :615) read the same record, which closes the gap in the table above for the next check in the prefix as well.

**Local: the prefix.** With the group gone, `Stop` puts the prefix back by the restore a check already runs (`restore`, local.go:518), under the prefix's lock (own.go:55).

- Where another check holds that lock, `Stop` does not restore beside it and does not assume it will. It answers "ended, something left": the snapshot and the directory stay, and the next check to start in the prefix puts it back before it builds (`restoreLeft`, :710), or `dockhand undo check-N` does.
- Putting back a prefix a check changed as root needs sudo. Where sudo would ask, the same answer, with the words `status` already has for doing it by hand (`byHand`, :321).
- A put-back that fails must keep its snapshot's name. Today the guest, run only to put back, starts with no facts and on a failure saves results without the snapshot (guest.tcl:59, :1285); the next check then takes the directory for one with nothing to restore and removes it (local.go:724), and `undo` finds nothing. This is read, not run. It is a fault of its own and is fixed with a test ahead of this item.

**Script.** `Launch` is the job's directory, so `Stop` finds the shell's record whatever reference the script's result later gives. The promise is the shell's group on this Mac, seen empty. A script whose shell hands over to another program, or ends and leaves a child in its group, is covered by rule 3. What a script started elsewhere, or moved out of its group, is the script's own, and the usage page says so.

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
| Choice 1 | A lean | Adopted, with its scope and its name |
| Choice 2 | A lean | Adopted |
| Choice 3 | Left open | Adopted, as above |
| GitHub's early reference | A branch and a commit, which several checks can share | The check's own branch, and what was launched saved before the push |
| "Calling a cancel API is not stopping" | `Stop` returned after the call | The three answers |
| The live driver | Only `stopUnfinished` changed | One rule for three paths. The second path, a driver taking a check whose cancel is recorded, is architecture's finding while checking the review |
| Cleanup | A defer | "Ended, something left"; Tart's kept clone is left alone |
| Stay and selection | Detached with bounded; `check -d`; the configured baseline; a shared run | Each said above. The configured baseline under `--for` is already queued and not followed (check.go:249), so nothing is decided there |
| `shell` and attempts | The first draft added them | Trimmed, as the review suggests: a refusal by name is what the issue asks |
| #38 | "Trusting a delete only by the clone's absence goes" | The second listing stays; discarding the delete's own error goes |
| "`RemoveLeftovers` waits for the lease" | It takes the lease or skips | Corrected |

Its second pass found most of that met, and five things still open. Each was checked against the code again.

| Second pass | What was wrong in the first revision | Taken |
| --- | --- | --- |
| The run's URL replaces GitHub's place, and Script's reference its directory | "The branch and the commit come from the check's own record". `Stop` is given no commit and the provider reads no store | `Launch`, a field of the attempt that `Refer` never replaces |
| "Ended" ten minutes after the attempt began | `appear` is the driver's patience, counted from after the push, and shows nothing about GitHub. Neither does a branch that moved | No run listed is not known, for as long as none is listed |
| "A worker that lost its lease starts nothing" | A `Refer` under the lease doesn't stop a paused worker from launching later | Launch and stop under one lock for the attempt |
| Local's and Script's process, found by its command line | A list of processes can miss, and a leader that is gone says nothing of its group | The child records itself first; gone means the group is empty; a group whose leader is gone is never signalled |
| "Another check holds the prefix lock, so it puts the prefix back" | Holding the lock isn't having restored | "Ended, something left", with the snapshot kept |
| Tart: "a clone that isn't there never started" | Absence doesn't say that | "No clone remains", and the lock so none can still be made |
| The three answers | Not in `Stop`'s own signature | What is left, or an error |
| `identity-unread` could be allowed where `source-unknown` can't | A second policy for one rule | It can't be allowed either |
| "Only GitHub's is ever stopped" | The live driver stops Local's and Tart's | Said of a cancel no driver attends |

Reading for the second pass turned up three faults in today's code, each small and each read, not run: a put-back that fails loses its snapshot's name; a guest whose driver died is unseen until after its snapshot, and the next check removes its directory; and a check whose cancel is recorded still holds back a submission of the same files. The first is fixed ahead of this item. The other two are fixed by it.

## Choices the person should see

Each changes something the person will notice. The third is Codex's rule: architecture's first revision finished such a cancel after ten minutes, and the second pass showed why that was wrong.

1. **serve holds a submission when it can't read what an environment is now.** Today it goes on. Nothing lets it through but the read succeeding or the person's own submit.
2. **A GitHub check's branch in the fork is named for the check.** More branches can stand in the fork between cleans, one for each check of a commit and not one for the commit.
3. **A cancel of a GitHub check stays unfinished while GitHub lists no run for a push it accepted.** `status` shows it, and every later `cancel`, and serve as it passes, looks again. If GitHub never starts the run, the line stays. The other way is to call the check canceled after a wait and say no run was listed; that gives up the rule this section sets, that a cancel is finished only when the work is seen ended.
4. **A cancel stops a local build and a script's shell, and puts a local prefix back.** Today it stops neither.
5. **A dockhand that hangs while it launches keeps that check's cancel unfinished until it is stopped.** The words name the process.

## Tests

- **Staying,** through the commands: every caller (`check`, the explicit and the configured baseline, `retry`, `wait`, `review`'s joined check, `ship` single and batch); a new check, a joined one, one already ended; to the end, detached, bounded; nobody leading, serve leading, another command holding it. Each row names who drives, what is printed, the JSON, and how the command ends, with Ctrl-C, a wait for capacity, a worker that fails to start, and `check -d`'s refusal.
- **Selection,** through the commands: a branch, `check-N`, a number, an attempt's ID, a provider's URL, a URL shared by a matrix, a name that is pruned or missing, several kept clones, and a release that conflicts. `logs` with no port, with a port and a phase, and following one attempt to its end. `shell`'s exit status and what it does to the evidence.
- **Identity:** the three answers against the readers, with the cause and with a read that was itself canceled; the words and the JSON; an unattended submit and ship held; an attended one that goes on; an unread environment the submission doesn't rest on, which holds nothing; `anotherCanReuse` and the attempt's retry, apart.
- **#189:** the counted loads above.
- **The one rule,** in `internal/checks`: two providers with one reference string, several references with one that fails, a matrix sharing a reference, a second cancel after a failure, and each of the three paths with a stop that fails.
- **Launch and stop,** in `internal/checks`, with two real processes: a launcher paused before it takes the lock, which then starts nothing; one paused while it holds it, which leaves the stop not known; one that died holding it; a launch that fails to save its note.
- **GitHub,** with a fake API that holds a run queued, running and cancelling: the note fails to save; the push's reply is lost; a run appears after an empty listing, after the driver's ten minutes, and after the branch is gone; an older run of the commit; another check of the same commit; a branch at the commit that no attempt of this check pushed; another repository; an old attempt with a URL and no note; the cancel's reply is lost; a cancel accepted and not yet complete; the branch's guarded removal.
- **Local and Script,** with real processes: the driver killed before the child's first act, and after it; a leader that is gone with a child left in its group; a number that was reused, left alone; a shell that hands over to another program; the stop seen through to an empty group; under sudo, whether the signal arrives, with `sudo -n` allowed and refused. A put-back that fails keeps its snapshot; one that finds the prefix's lock held leaves it, named, and the next check restores it. Automated against a throwaway prefix.
- **Tart:** the name saved and no clone; a clone made though the reply was lost; a clone still being made by a child whose driver died; a clone already gone or stopped; a stop that fails; a delete or a marker that fails; a kept clone beside an unfinished one; `clean` and a retry competing for the lease.
- **What is said:** `status` and its JSON for a check whose cancel isn't finished; a submission of the same files, which doesn't wait on it.
- **Acceptance,** since the fault sweep ([#129](https://github.com/herbygillot/dockhand/issues/129)) leaves two real dockhands and the Local and Tart lifecycle outside: G15 and G4 gain a cancel of the check whose dockhand was killed, proving the guest gone and the prefix as it was; a row beside D-I5 cancels a Tart check whose driver was killed and proves the clone gone and its place free; a row cancels a GitHub check whose driver was killed before the run's URL was saved.
- The earlier fixes' tests stay: the bounded baseline, the selected attempt's log, retry's cleanup, a detached cancel.

## How it is built

1. #189, with its counted test.
2. `Stay` and `See`, then one command a commit onto it.
3. The selection, then `logs`, `artifacts`, `shell`.
4. Identity's three answers (#190), then the hold.
5. Stopping: the one rule and `stopUnfinished`, with the service's test, and what `status` and a submission make of an unfinished cancel; `Launch` and the launch lock, with every provider's start moved under it; GitHub's branch name in the provider and in `clean` together, then what its `Stop` sees; Tart's `Stop`; the child's own record and the helper that reads a group, then Local's `Stop` and Script's. The real-process test of a signal under sudo comes before Local's `Stop` is written.
6. The Tart minimum (#38).

After the source comparison ([#192](https://github.com/herbygillot/dockhand/issues/192)), #129's foundation, [#152](https://github.com/herbygillot/dockhand/issues/152)'s rename of `command/runcmd`, and [#197](https://github.com/herbygillot/dockhand/issues/197), which changes how the same GitHub provider polls. [#181](https://github.com/herbygillot/dockhand/issues/181) lands before the last queue cases.

## Not checked

- Nothing here was run. Local's and Script's survival is read from how they launch and from G15, which already kills a dockhand and finds its guest building.
- Whether the person's own process may signal a root sudo or its child on macOS, and whether that sudo keeps the guest in the group dockhand made. Rules 3 and 5 are written so that either answer fits.
- Whether an ordinary user's process list on macOS shows the arguments of root's processes, which only rule 6 needs.
- That macOS doesn't hand a process number out again while a group carries it, which rule 3 rests on. It is the system's documented behaviour; the test with a reused number checks dockhand's side of it only.
- How long GitHub can take to list a run after a push it accepted, and whether it ever drops one.
- The three faults named under "What the review changed" are read, not run.
- Which Tart the M1 and the M5 run.
- `shell`'s entry and `ForgetKept` beyond their call sites, and every caller of `RunNamed`.

## Usage docs

- `docs/usage/checks.md`: `cancel` stops the work at every provider and says what it couldn't see end; a cancel that isn't finished, what `status` shows for it, and that `cancel` again looks again; what it does for a local prefix and for a script, and what a script's own children are; the command to run where a root build can't be signalled; `artifacts` and `shell` take a check, not an attempt; `logs <port> --phase` works.
- `docs/usage/unattended.md`: serve holds a branch whose environment it couldn't identify, until it can or the person submits.
- `docs/usage/setup.md`: the Tart minimum, 2.41.1, where it describes installing Tart; and the name of a check's branch in the fork, where it describes the fork.
- `docs/architecture.md`: `Stay`, the selection, the three answers of a stop, and which providers stop.
