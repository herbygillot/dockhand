# A check's lifecycle: how long a command stays, which build it means, what an environment is, and how it stops

A core item for v0.9.0, [#193](https://github.com/herbygillot/dockhand/issues/193). The issue says where it stands; this file is the design. Architecture wrote it on 2026-11-11, Codex reviewed that draft against the code the same day (the person's "I'd like Codex to co-design and help review as well", 2026-11-11 05:48Z, in the architecture thread), and this is the design revised for that review, for its short second pass and for a third look at "Stopping", read again at main 1fab1150b. "What the review changed" lists each correction. The person adopted it ("Adopt", 2026-11-11 08:56Z, on the architecture thread's card), while Codex's check of the last revision was still out: anything that check breaks comes back to the person as an amendment to this page. That check came back the same day and broke three points of "Stopping"; **the amendment** is "The last look" under "What the review changed". The person holds it until Codex has checked it ("After Codex", 2026-11-11 09:34Z, on the architecture thread's card). Codex checked it the same day, at main ab0e07431: it confirmed one of the three answers and broke two, what shows a root build's end and what answers for two pushes. Both are revised here, under "The check of the amendment". Codex checked that revision too: both mechanisms hold on what it read, and two claims were wider than their evidence, which are narrowed here. The person took the amendment as revised ("Take it", 2026-11-11 15:49Z, on the architecture thread's card). This page, amendment included, is the design.

## In short

Four things about a check are each decided in several places today. Each gets one value or one rule, made once and passed.

- **Staying.** How long a command stays with a check is chosen by which of four helpers it calls, and how. It becomes one value, `Stay`, and one function.
- **Selection.** `logs`, `artifacts` and `shell` each work out which check, attempt, port and phase they mean. One resolver answers, and a command refuses by name what it can't serve.
- **Identity.** An environment whose identity couldn't be read is treated as one that has none ([#190](https://github.com/herbygillot/dockhand/issues/190)). It becomes one of three: known, none, or unread with its cause. An unread one holds a submission nobody attends.
- **Stopping.** In every provider the work can outlive a driver that is killed, and where no driver attends, only GitHub's is stopped. A cancel is finished only when each provider has seen its work end, by whichever of three paths the cancel is applied. Launching and stopping an attempt exclude each other by a lock that every child able to start work holds from its fork, and what a provider launched is kept apart from the reference a person looks at.

Two smaller issues close with it: [#189](https://github.com/herbygillot/dockhand/issues/189), earlier builds loaded where nothing reads them, and [#38](https://github.com/herbygillot/dockhand/issues/38), the Tart minimum.

No flag or command is added. Two things a person sees change outside dockhand: a GitHub check's branch in the fork is named for the check as well as the commit, and a local build or a script shows a small dockhand process as its parent.

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

- Every provider launches through one call the driver gives it. The call takes an operating-system lock for the attempt (`internal/filelock`), reads again under it that the check is still this driver's and that no cancel is recorded, saves `Launch`, and only then runs the provider's start. The start may hand its note back changed, and the call saves that before it lets go: GitHub's does, to write down a launch that started nothing (below). It lets go when the start has returned and that is saved.
- `Stop` takes the same lock before it reads `Launch`.
- So `Stop` meets one of two things. A live process is still launching: not known, naming that process, and nothing is finished. Or `Stop` holds the lock: then nothing more can be launched for that attempt, because a launcher that gets the lock later reads the cancel and starts nothing. The cancel is recorded before any `Stop`.
- The lock is never taken from a live process. A driver that hangs inside its launch keeps the cancel unfinished until it is stopped, and the words say which process.
- **Every child that can start work, or that is the work, holds the lock from the moment it is forked.** The file is handed to it as the branch lock's is to `git` (internal/git/branch.go:41, repository.go:107): GitHub's `git push`, Tart's clone and its `tart run`, and the starter that Local and Script launch (below). A root build's guest holds it as well, through its sudo (rule 4 below). Tart already hands its image guard's file to its children this way; the attempt's file goes beside it, and the guard stays. A child that has been forked and has not yet run one instruction of its own already holds it. So `Stop` holds the lock only when no launcher and no such child is alive.
- Because `tart run` and the starter hold it for as long as the work lives, `Stop` first acts on what it can see, with no lock: it stops the VM the reference names, or signals the starter its record names. Both are the check's own, and the cancel is already recorded. Then it takes the lock, which they give up as they end. Where it can't have the lock within its bound, something is still alive: not known, naming it.
- The last revision handed the lock to Tart's clone alone and found Local's and Script's children by what they wrote. Codex's third look showed the gap with a harmless experiment: a child that didn't hold the lock was forked, its launcher died, a controller took the lock, and the child then ran on and wrote its first record while the controller held it.
- Running a finished GitHub run again starts work as a push does (provider.go:297). It goes through the same call: the lock, the cancel read again, and `Launch` saved again with the run and the attempt number it had, before the rerun is asked.

**GitHub.**

- The check's branch in the fork becomes the check's own: `dockhand-check/<commit>-<check>`, from the first twelve characters of the commit and the whole random part of the check's ID, sixteen characters holding eighty random bits (`store.NewID`, internal/store/store.go:355), the same for its matrix and for every attempt. Today it is named for the commit alone (provider.go:217), and a check that finds the branch already at its commit takes the newest run there (:227, :248). Two checks of one commit, from one Mac or two, can then share a run, and stopping one check's run would stop the other's. `clean` finds the branch by the same name (engine/clean.go:439), so the name comes from one function in `buildenv`.
- No other check pushes that name, so every run on it for the commit and the workflow is this check's, whichever of its attempts pushed, attached to it, or ran it again. A `Launch` saved before a push doesn't show the push landed, and doesn't need to: the branch being at the commit is the push having landed, and nothing else pushes there. Where the branch is at the commit and no attempt of the check has a `Launch` for it, the attempt fails and names the branch.
- `Launch` is the repository, the workflow file, the branch, the whole commit, the check's whole ID, and what the attempt is about to do: push, or run a named run again from the attempt number it has. It is saved before the push and before a rerun. With no `Launch`, nothing was pushed for the attempt. For a push it also keeps the newest run GitHub lists for the branch and commit before the push, which the driver reads today to tell its own run from an earlier one (`before`, provider.go:226-249). An attempt that finds the branch at its commit and pushes nothing keeps the run it takes. That newest run has one use: to know whether a push has shown its own run yet. It never decides which runs `Stop` cancels. Those are every run on the branch, one a later attempt attached to or ran again included, which is what the second revision got wrong when it had `Stop` take only newer ones.
- **A launch known to have started nothing is written down as that.** The start hands its note back with that on it, and the call saves it before it lets go of the lock, in these cases and no others:
  - nothing was sent: dockhand refused before it ran `git push` or asked GitHub (`remotewrite.Permit`, remotewrite.go:106), or the push's own read of the branch failed, or found it elsewhere than expected, or found it already at the commit (git/remote.go:77-86). The last of those returns today as a push that succeeded (:81), and it says nothing of the check's earlier pushes, which still need their runs;
  - `git push` ran, and its own report says the branch was rejected: `[rejected]` or `[remote rejected]` on the line for this very branch in `--porcelain` (remote.go:88). `[remote failure]` is not one. `Push` returns only an error today; it gains that line as it was read.

  Such a launch needs no run. **A rerun GitHub refuses is not among them yet.** GitHub documents the answer to a rerun that worked and nothing of what its refusals mean ("Re-run failed jobs from a workflow run"), so no status is taken as proof that nothing started. The status and the body are kept on the note as they came. A refusal is added to this list by name once an acceptance row on the test accounts has shown what it means, and only for a call that was sent once. Anything else leaves the note as it was first saved, and that note says only that something may have been started: a connection lost before the answer, any status GitHub gives a rerun but success, a `git push` that failed without a line for the branch, a note that couldn't be saved again, and a dockhand that died between the first save and the answer.
- `Stop`, holding the lock:
  - no `Launch`: nothing was pushed and nothing can be. Ended;
  - runs listed for that workflow, branch and commit: it cancels each that isn't completed, then reads them for a short bound. All completed, and every `Launch` of the check answered for: ended. Else: not known, naming the runs;
  - a `Launch` is answered for when it is written down as having started nothing, or when the run it started is listed. For an attempt that took a run already there, that is the run it took. For a rerun, the named run past the attempt number noted. For a push, a run newer than the one it noted. **Each push and each rerun is counted once, by its own note.** Two pushes that noted the same newest run need two runs newer than it, and two reruns that noted the same attempt number need the run two attempts on. No run answers for two pushes and no attempt of a run for two reruns, and it is each launch's own note that a run must be past, not a count of runs. An attach started nothing, so several can point at one run, and at a run some push answers for. GitHub is asked only for runs a push started (`Event: "push"`, ghactions/github.go:78), so each run listed is some push's;
  - a push with no run of its own listed is not known, whatever else is listed. Two cases show why. An earlier run of the check, completed, says nothing of a later push: a run completes, its logs can't be read, the person removes the branch, and the next attempt pushes the commit again (provider.go:373-393, :217-248); GitHub goes on listing the first run (:222) while the second hasn't appeared (the last look, L5). And one run says nothing of two pushes: a push's run isn't listed yet when its driver stops, the branch is removed, and a later attempt pushes again and notes the same newest run, here none. The first push's run then appears and completes, newer than both notes, while the second's can still start (Codex's check of the amendment, A7);
  - none listed: **not known**, however long it has been. GitHub accepted a push, and nothing shows a run can't still start. The branch being gone or moved doesn't show it either: the push can have landed and started a run before that. Each later ask looks again, and a run that appears is canceled then;
  - **what that costs.** A launch that started nothing, and isn't written down as that, is never answered for: a dockhand that died after it saved its note and before `git push` ran, or a push or a rerun whose answer was lost and which never took effect. Nothing dockhand can read tells those from a launch whose run is still to come, and time alone never does. The cancel then stays unfinished. `status` keeps its line, and every `cancel`, and serve as it passes, look again. Nothing else waits on it: a submission of the same files doesn't (above).
- The ten minutes the driver gives GitHub to list a run (`appear`, provider.go:176) stay what they are: the driver's patience, counted from its own first look after the push, after which it reports trouble and tries again. They are not evidence that no run exists, and `Stop` doesn't use them.
- An attempt recorded before this design has a run's URL and no `Launch`. `Stop` cancels that run and reads it the same way. A URL in another repository is refused, as today (#128). A reference of neither form is an error, where today it is silence.
- The live driver's own cancel stays, for a check that is canceled and not for a driver that only stops. The one rule then asks `Stop` as for anyone else, so its error is no longer dropped.

**Tart.** `Stop` is the removal Tart already has (`RemoveLeftover`: stop the VM, delete the clone, forget its marker), given the clone the reference names, in two parts: the stop first, then the delete once it holds the lock. Where no clone remains, and `Stop` holds the lock so that no clone can still be made and no `tart run` is alive, it is ended. A clone a finished attempt kept for `shell` carries the kept marker (`keepClone`, tart/provider.go:507) and is not unfinished work: `Stop` leaves it, so a cancel removes no more than it does today. A stop or a delete that fails is not known, with Tart's error, and the reference stays for the next ask. `Stop` runs under the check's lease, which `clean` takes or skips the clone for (engine/clean.go:984).

**Local and Script: which processes, and when they are gone.**

Both start one process in a group of its own and leave it running where the driver dies. What is launched isn't always what builds: where the prefix is root's, Local launches `sudo`, and the guest program is sudo's child (local.go:503). Whether that child stays in the group dockhand made depends on the sudo. So the rule doesn't lean on it.

1. **dockhand starts the build through a starter of its own.** The launcher's child is dockhand itself, run as a hidden command. It leads a session of its own, is handed the attempt's lock, records itself in the directory `Launch` names, and then runs the build's command as its child: the guest program, under `sudo -n` where the prefix is root's, or the script's shell. It keeps the lock to itself, except that a sudo is handed it for the guest (rule 4), and stays until that child has ended.
   - It holds the lock from the moment it is forked. So there is no moment when a child exists and `Stop` holds the lock.
   - It is the person's own process whoever the build runs as, so `Stop` can always signal it.
   - It leads its group, and the build's command is in that group from its own fork. The group's number is the starter's PID, so it is on record before anything is started. Where the command is a sudo, the guest may be given a group or a session of its own, so a root build's end is shown by the lock and not by the group alone (rule 4).
   - A starter that can't save its record starts nothing. A record that can't be read is an error, never "no record" (the last look, L2).
2. **Its record is its PID and the system's own start time for it**, as `procid` reads them (procid/start_darwin.go:15). A process with that PID is the starter only where its start time is the recorded one, exactly: the test `procid` already makes for a session's process. The last revision compared a time the child took from its own clock, and a clock set back would have passed a later process.
3. **`Stop` signals the starter, the starter stops its group, and `Stop` sees the end.** One signal crosses between processes: to the starter's PID, sent straight after rule 2's test. The starter sends its group the stop the live driver uses today, SIGTERM for Local and SIGINT for Script (subprocess.go:136, session_unix.go:21), which it ignores itself, and waits out the grace. Its last act is the SIGKILL to its own group, with no number, and it dies by it. A signal to one's own group reaches the sender too (Apple's kill(2)), so the starter can't outlive it and look afterwards, which is what the adopted page had it do (the last look, L3). `Stop` looks instead: the lock is free, and the starter's group, asked by its recorded number with a signal of zero, answers "no such process". macOS gives a number to nothing new while a group still carries it, so that answer can't come while any of the group is left. A later group that took the number can only answer as members, which reads as not known and never as ended.
4. **Under sudo: the guest holds the lock, and no sudo is killed outright.** Where the build's command is a sudo, what builds is the sudo's child, and the sudo may put it in a group or a session of its own. Codex showed two ways that goes wrong.
   - A SIGKILL sent to a sudo isn't passed on to its command (sudo's manual, "Signal handling"). A guest in a session of its own was left running, and one stopped before its first save had recorded no group to find it by: the starter's group empty, the lock free, the child alive (the last look, L4, made with Codex's own processes).
   - How a sudo ended shows nothing of its command either. The amendment first took any end but a SIGKILL as the guest's end, from the manual's "Process model". A sudo that dies of a fault of its own ends with the guest alive, and so can one that leaves by its error path, which signals its command and doesn't wait for it (`terminate_command`, sudo's exec.c; Codex's check of the amendment, A6, read from the source and not run).

   So nothing is read from a sudo's end, and the starter writes nothing about it. What shows a root build's end is the lock:
   - **The starter hands the sudo the attempt's lock as its standard input**: the same open file the starter holds locked (`flock`, internal/filelock/filelock.go:41), so whoever still has it open holds the lock. The guest is given no input today (local.go:506), so the descriptor is free. sudo closes every descriptor from 3 up before it runs a command (sudoers' `closefrom`), and leaves its standard input to the command as it came, with a pseudo-terminal or without, unless a plugin logs the command's input, when it puts a pipe there ("Not logging stdin, do not interpose", upstream's exec_pty.c; `interpose_pipes`, exec_nopty.c).
   - **The guest's first act is to see that its standard input is that file**, before its first save and before it starts anything: the device and file number of `/dev/fd/0` against those of the lock's path, which the starter gives it. Where they differ the guest says so and ends, having started nothing, and the attempt fails naming the sudo's logging of input. A guest that can't save its record starts nothing either, as for the starter. **The check names the file, not the opening of it.** It catches a pipe or a terminal put in the lock's place. That the guest's descriptor is the very opening the starter locked rests on how sudo hands it on, by duplicating it and never by opening the file again (upstream's exec_pty.c, exec_monitor.c:335 and exec_nopty.c), and on the test with this Mac's own sudo below. Codex showed that the check alone can't carry that: a child given a separate opening of the same file passes it, and `Stop` then takes the lock while the child lives (its second check, B4, run with its own processes). A guest run only to put a prefix back, or only to stop, builds nothing and makes no such check; it is given `/dev/null`.
   - So `Stop` holds the lock only when the starter, the sudo, the guest, and whatever the guest handed its standard input on to have all ended, however each of them ended. A guest that exists and hasn't made its check yet either holds the lock, or will find that it doesn't and start nothing.
   - What the guest starts gets the guest's standard input unless the guest gives it another (Tcl's `exec`, guest.tcl:161). Either way holds: a command that keeps it holds the lock, and one that doesn't is in a group `Stop` asks (rule 5).
   - Where `Stop` can't have the lock and the starter is gone, something that was handed the lock still runs: not known, with the guest's own record where it made one, and nothing is signalled.

   The stop itself goes as the last look left it:
   - Nothing of dockhand's sends a sudo a SIGKILL. With the lock in the guest's hands a killed sudo could no longer make `Stop` say ended. But the sudo is the one way to reach a guest that has recorded nothing, and a build that nothing can stop is the worse end.
   - The starter signals the sudo by its PID, with SIGTERM. The sudo is its own child and it hasn't collected it, so the number is still that child's.
   - Where that signal is refused, the starter runs the guest program under `sudo -n` to do only the stop, the way a put-back runs it to do only that (local.go:518): the same program the sudo rule already names, and never a prompt. As root it sends the sudo the same SIGTERM.
   - The starter waits for the sudo to end. Where it hasn't by the grace, the one SIGKILL before the end is root's, and it goes to the guest: the guest's PID and start time, which it records at its first save, are tested as rule 2 tests the starter's, and then its own group is killed where it leads one, or the guest alone where it is in the starter's. The sudo then sees its command end, and ends.
   - Where the guest recorded nothing, or the sudo still doesn't end, the starter stays and keeps the lock. `Stop` answers not known and names the starter and the sudo.
   - With the sudo ended, what is left in the starter's group is root's, so the last SIGKILL is root's too: the starter has the root stop send it to the starter's group, and both die by it. That is rule 3's last act, done by the one who can. A group the guest led is only asked by then, not signalled: with the guest gone its number is no longer held by anything dockhand can test.
   - Where sudo would ask, the answer is not known, with the one command to run by hand and `dockhand cancel check-N` to run after it.

   Nothing tests whether the person's own process may signal a root sudo at all, and being its parent isn't permission (Codex's third look); the code's comment assumes it (local.go:934). The real-process test settles that, that this Mac's sudo passes a SIGTERM on, and that it hands its standard input to its command with the lock still held, before Local's `Stop` is written. The rule holds whichever way the first comes out. Where a sudo puts a pipe there, the guest's own check refuses every root build on that Mac and says why, which is the safe end. Where a sudo opened the file again, nothing in the guest would notice: that is what the test with the real sudo is for, with a pseudo-terminal and without, and root builds are not stopped this way on a Mac that hasn't passed it.
5. **It is gone when `Stop` has seen it gone.** `Stop` holds the lock, so no launcher and no starter is alive. Each group is asked with a signal of zero, and each answer is read for what it is: "no such process" is a group that is gone; "not permitted" is members that can't be signalled, so not gone; anything else is not known.
   - No record: no starter ever ran the build. Ended.
   - A record from before the Mac's last restart, which the record's boot session shows: ended. Nothing from before a restart is running.
   - A build that wasn't under sudo: ended where the starter's group is gone.
   - A build under sudo: with the lock in `Stop`'s hands, the starter, the sudo and the guest have all ended, however each ended (rule 4). Ended where the starter's group is gone, and any group the guest recorded. A guest that recorded nothing started nothing: its save comes before anything it starts.
   - A group with members and the starter gone: not known, naming the group, and nothing is signalled, since those members can't be told from a later group that took the number.
6. **What remains unproved.** Between rule 2's test and the signal to the starter there is one system call, as for any signal sent by number. macOS doesn't hand out a number that a process or a group still carries (XNU's `forkproc`, kern_fork.c, read by Codex), so the wrong process could be reached only if the starter ended and its number came round again in that instant. A process that leaves the group on purpose, as a build's own daemon may, is outside what `Stop` sees. That a sudo passes a SIGTERM on to its command, and hands its standard input on with the lock still held, is read from its manual and its source, not yet seen on this Mac. A sudo, or a stand-in for one, that opens its standard input's file again is not covered: the guest would pass its check holding no lock.

The live driver's stop goes the same way: it signals the starter, which is its child. `building` and `orphans` (local.go:681, :615) read the lock and the starter's record, which closes the gap in the table above for the next check in the prefix as well: a directory whose lock is held is being built in, from the moment its starter is forked.

**Local: the prefix.** With the group gone, `Stop` puts the prefix back by the restore a check already runs (`restore`, local.go:518), under the prefix's lock (own.go:55).

- Where another check holds that lock, `Stop` does not restore beside it and does not assume it will. It answers "ended, something left": the snapshot and the directory stay, and the next check to start in the prefix puts it back before it builds (`restoreLeft`, :710), or `dockhand undo check-N` does.
- Putting back a prefix a check changed as root needs sudo. Where sudo would ask, the same answer, with the words `status` already has for doing it by hand (`byHand`, :321).
- A put-back that fails must keep its snapshot's name. Today the guest, run only to put back, starts with no facts and on a failure saves results without the snapshot (guest.tcl:59, :1285); the next check then takes the directory for one with nothing to restore and removes it (local.go:724), and `undo` finds nothing. Codex's probe of that branch confirmed it. It is a fault of its own, [#199](https://github.com/herbygillot/dockhand/issues/199), fixed with a test ahead of this item; the name is carried through the put-back's first save as well as its failure's.

**Script.** `Launch` is the job's directory, so `Stop` finds the starter's record whatever reference the script's result later gives. The promise is the starter's group on this Mac, seen gone. A script whose shell hands over to another program, or ends and leaves a child in its group, is covered by rules 3 and 5. What a script started elsewhere, or moved out of its group, is the script's own, and the usage page says so.

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

Its third look, at "Stopping" alone, found the earlier points met and four things open.

| Third look | What was wrong in the second revision | Taken |
| --- | --- | --- |
| The lock covers a paused launcher, not every child that starts work | Only Tart's clone was handed it. A `git push`, a `tart run` and a Local or Script child could go on after their launcher died | Every such child holds the lock from its fork. `Stop` acts on what it sees first, then takes the lock |
| Rule 6, the answer from absence | A child that exists and has done nothing yet has no record, and the lock didn't hold it back. Codex ran that ordering | Rule 6 is gone. Local and Script start through a starter that holds the lock; no record with the lock held means no starter ran |
| A rerun starts work too | `Execute` runs a finished run again outside any exclusion (provider.go:297) | The same call, with `Launch` saved again |
| A run a later attempt attached to | `Stop` took only runs newer than the one `Launch` noted, which left out that very run | No such mark. The branch is the check's own by its whole ID, so every run there is the check's |
| "Started no later than the record" | Two clocks, and one can be set back | The starter's PID with the system's start time for it, compared exactly |
| A signal of zero answers "with no privilege" | "Not permitted" is an answer too, and isn't an empty group | Each answer read for what it is |
| Checking a leader, then signalling its group | Two calls, with a gap | The starter signals its own group, with no number. The one signal by number is to the starter, and its gap is said |
| sudo passes a signal on | An assumption, untested, and a parent isn't thereby allowed | Said as one. The root stop runs the program the sudo rule already names; the real test comes first |

**The last look** (Codex, 2026-11-11, at main 1fab1150b, after the person had adopted the page) confirmed two of five points and broke three, two of them one fault in the starter. All are taken. This is the amendment, as the check below then revised it.

| Last look | What was wrong in the adopted page | Taken |
| --- | --- | --- |
| L1. The lock's file reaches each child | Confirmed, by experiment for a forked and an exec'd child. Tart's image guard goes the same way and must stay | Said. Whether `tart run` keeps the file open stays an acceptance check |
| L2. No record with the lock held | Confirmed, provided a failed save starts nothing and an unreadable record is an error | Both said in rule 1 |
| L3. The starter looks after it has killed its group | A signal to one's own group kills the sender too. Codex ran it: nothing after the kill ran, and the lock came free | The kill is the starter's last act, and `Stop` sees the end: the lock free and the group gone by its recorded number |
| L4. An empty group under sudo | With the guest in a session of its own and stopped before its first save, killing the starter and the sudo left it alive, in no recorded group, with the lock free. sudo doesn't pass a SIGKILL on | No SIGKILL is ever sent to a sudo. What shows a root build's end was first the sudo's own end, which the check below broke; it is now the lock, held by the guest |
| L5. A later push behind an earlier run | With a run listed and completed, `Stop` answered ended, though a later attempt had pushed again and its run hadn't shown | Every `Launch` must be answered for. A push keeps the newest run listed before it, and only a newer one answers for it, one run for each push (the check below) |

**The check of the amendment** (Codex, 2026-11-11, at main ab0e07431, the page read at 45df2864b) confirmed the answer to L3 and broke the answers to L4 and L5. Each was read again against the code, and L4's against sudo's source. Both are revised above.

| Check | What was wrong in the amendment | Taken |
| --- | --- | --- |
| A5. The starter's last act | Confirmed, for work that stays in the recorded group. Codex ran a starter that killed its own group with a child ignoring the stop: the starter died by the signal, the lock came free, and the group answered "no such process" | Nothing changes |
| A6. "A sudo that ended, and not by a SIGKILL, shows the guest ended" | It doesn't. A sudo can die of a fault of its own, or leave by an error path that signals its command and doesn't wait, with the guest alive in a group of its own and nothing recorded. `Stop` then said ended. Read from sudo's source, not run | Nothing is read from how a sudo ended. The lock reaches the guest as the sudo's standard input, the guest checks that it has it before anything else, and `Stop` says ended only while it holds that lock |
| A7. "A push is answered for by a run newer than the one it noted" | One run answered for two pushes that had noted the same newest run, while the second push's run could still start | Pushes are answered one for one, and reruns of one run likewise |
| A7. Launches that stay unanswered though nothing will start | A refusal wasn't kept, so a push GitHub rejected read as one that might still start a run. The cost of the rest wasn't said | A launch known to have started nothing is written down as that and needs no run. One whose fate nothing shows stays not known for good, and the page says so and what it costs |

Codex checked these answers the same day (at checkout fce0515a, the page read at b9905719a).

| Second check | What it found | Taken |
| --- | --- | --- |
| B4. The lock through the sudo | Holds on the path Codex read in sudo's source, exec_monitor.c included: the descriptor is duplicated, not opened again. But equal device and file number name the file, not the locked opening. Codex ran it: a child with a separate opening passed the check, and `Stop` took the lock while it lived | The check is said for what it shows, a pipe or a terminal in the lock's place. Continuity rests on sudo's duplication and the test with the real sudo, which gates stopping root builds on a Mac. A sudo that opens the file again is listed as not covered |
| B5. One for one | Fixes the two-push case. "Nothing was sent" and the two rejected forms are sound. Every status from 400 to 499 on a rerun is more than GitHub documents | No refusal of a rerun is taken as proof yet. The answer is kept, and a refusal is added by name once an acceptance row shows what it means. Attaches may share a run |

Reading for the second pass turned up three faults in today's code, each small. Codex's third look ran a probe of each and confirmed all three: a put-back that fails loses its snapshot's name; a guest whose driver died is unseen until after its snapshot, and the next check removes its directory; and a check whose cancel is recorded still holds back a submission of the same files. The first is [#199](https://github.com/herbygillot/dockhand/issues/199), fixed ahead of this item. The other two are fixed by it.

## Choices the person should see

The person took all five as written, with the page ("Adopt", 2026-11-11 08:56Z). The last look amends the third and the fifth, each by one case the person can meet; the person holds the amendment until Codex has checked it ("After Codex", 2026-11-11 09:34Z). Codex has, twice, and the two amended sentences below are as revised for its checks. The person took them ("Take it", 2026-11-11 15:49Z).

Each changes something the person will notice. The third is Codex's rule: architecture's first revision finished such a cancel after ten minutes, and the second pass showed why that was wrong.

1. **serve holds a submission when it can't read what an environment is now.** Today it goes on. Nothing lets it through but the read succeeding or the person's own submit.
2. **A GitHub check's branch in the fork is named for the check**, with the check's whole ID in the name. More branches can stand in the fork between cleans, one for each check of a commit and not one for the commit, and their names are longer.
3. **A cancel of a GitHub check stays unfinished while GitHub lists no run for a push it accepted.** `status` shows it, and every later `cancel`, and serve as it passes, looks again. If GitHub never starts the run, the line stays. The other way is to call the check canceled after a wait and say no run was listed; that gives up the rule this section sets, that a cancel is finished only when the work is seen ended. *Amended:* the same holds for each push of a check, counted one for one: where two attempts pushed the commit, two runs must be listed and ended. A push that was refused outright is written down and needs no run. A rerun GitHub refuses is not yet read that way, since GitHub doesn't say what its refusals mean. A launch whose answer never came, or whose dockhand died as it was sending, can't be told from one that landed: where no run ever shows for it the line stays, and serve goes on looking as it passes.
4. **A cancel stops a local build and a script's shell, and puts a local prefix back.** Today it stops neither. For that, dockhand starts each of them through a small process of its own, which shows as the build's parent.
5. **A dockhand that hangs while it launches keeps that check's cancel unfinished until it is stopped.** The words name the process. So does a build whose starter was killed outright and left processes behind: dockhand names them and signals none, since it can't tell them from a stranger's. *Amended:* a cancel never kills a sudo outright, since the sudo is the one way to reach a root build that has recorded nothing of itself. Where such a build ignores the stop, the cancel stays unfinished and names the sudo. A root build counts as ended only once nothing holds its lock, however its sudo ended. For that the build is handed the lock through the sudo, and dockhand starts no root build under a sudo set to log the command's input, where the lock wouldn't reach it. That this Mac's sudo hands the lock on is tested on the Mac before root builds are stopped this way.

## Tests

- **Staying,** through the commands: every caller (`check`, the explicit and the configured baseline, `retry`, `wait`, `review`'s joined check, `ship` single and batch); a new check, a joined one, one already ended; to the end, detached, bounded; nobody leading, serve leading, another command holding it. Each row names who drives, what is printed, the JSON, and how the command ends, with Ctrl-C, a wait for capacity, a worker that fails to start, and `check -d`'s refusal.
- **Selection,** through the commands: a branch, `check-N`, a number, an attempt's ID, a provider's URL, a URL shared by a matrix, a name that is pruned or missing, several kept clones, and a release that conflicts. `logs` with no port, with a port and a phase, and following one attempt to its end. `shell`'s exit status and what it does to the evidence.
- **Identity:** the three answers against the readers, with the cause and with a read that was itself canceled; the words and the JSON; an unattended submit and ship held; an attended one that goes on; an unread environment the submission doesn't rest on, which holds nothing; `anotherCanReuse` and the attempt's retry, apart.
- **#189:** the counted loads above.
- **The one rule,** in `internal/checks`: two providers with one reference string, several references with one that fails, a matrix sharing a reference, a second cancel after a failure, and each of the three paths with a stop that fails.
- **Launch and stop,** in `internal/checks`, with real processes: a launcher paused before it takes the lock, which then starts nothing; one paused while it holds it, which leaves the stop not known; one that died holding it; a launch that fails to save its note; a child forked and held before its first instruction while its launcher dies, which keeps the lock from `Stop` (Codex's experiment, with the lock handed on); a rerun asked while a cancel is recorded.
- **GitHub,** with a fake API that holds a run queued, running and cancelling: the note fails to save; the push's reply is lost; a run appears after an empty listing, after the driver's ten minutes, and after the branch is gone; a run a later attempt attached to, and one it ran again, each stopped; a rerun asked and not yet started; a run completed, the branch removed, and a second push whose run isn't listed yet, which is not known; two pushes with neither run listed, then one run that completes, which is not known, and the second run, which ends it (Codex's A7); a push GitHub rejects, one dockhand refuses before sending, and one that finds the branch already at the commit, each written down and needing no run; a push whose answer was lost and that never shows a run, and a note saved by a dockhand that died before `git push`, each not known however long; a rerun GitHub refuses, which stays not known with the status and body kept; a rerun whose answer was lost; two reruns that noted the same attempt number; two attaches to one run, which share it; a note that can't be saved again, which stays as it was; another check of the same commit, on its own branch; a branch at the commit that no attempt of this check has a note for; another repository; an old attempt with a URL and no note; the cancel's reply is lost; a cancel accepted and not yet complete; the branch's guarded removal.
- **Local and Script,** with real processes: the driver killed before the starter's record, and after it; the starter killed outright with a child left in its group, named and not signalled; a number that was reused with another start time, left alone; "not permitted" from a signal of zero, read as not empty; a shell that hands over to another program; the stop seen through from outside, to a free lock and a group that is gone; a starter that can't save its record, which starts nothing; a record that can't be read; a guest stopped before its first save in a session of its own, with the starter and the sudo killed, which stays not known (Codex's shape); the same with the sudo dead of another signal, and with a sudo that left after only signalling its command, each not known while the guest lives and ended once it is gone (Codex's A6); a guest whose standard input isn't the lock, which starts nothing and fails the attempt; a child given a separate opening of the lock's file, which passes the guest's check and holds no lock (Codex's experiment), kept as the case the check doesn't cover; a guest that can't save, which starts nothing; a guest's child that kept its standard input and outlives it, which keeps the lock; a sudo that ignores the stop, which is never sent a SIGKILL; a record from before a restart; under sudo, whether the person's signal reaches a root sudo, whether the guest keeps the starter's group, whether the lock handed to the sudo as its standard input is still held by its command, with a pseudo-terminal and without, and the root stop with `sudo -n` allowed and refused. A put-back that fails keeps its snapshot; one that finds the prefix's lock held leaves it, named, and the next check restores it. Automated against a throwaway prefix.
- **Tart:** the name saved and no clone; a clone made though the reply was lost; a clone still being made by a child whose driver died; a `tart run` forked as its driver dies, which keeps the lock until the VM is stopped; a clone already gone or stopped; a stop that fails; a delete or a marker that fails; a kept clone beside an unfinished one; `clean` and a retry competing for the lease.
- **What is said:** `status` and its JSON for a check whose cancel isn't finished; a submission of the same files, which doesn't wait on it.
- **Acceptance,** since the fault sweep ([#129](https://github.com/herbygillot/dockhand/issues/129)) leaves two real dockhands and the Local and Tart lifecycle outside: G15 and G4 gain a cancel of the check whose dockhand was killed, proving the guest gone and the prefix as it was; a row beside D-I5 cancels a Tart check whose driver was killed and proves the clone gone and its place free; a row cancels a GitHub check whose driver was killed before the run's URL was saved.
- The earlier fixes' tests stay: the bounded baseline, the selected attempt's log, retry's cleanup, a detached cancel.

## How it is built

1. #189, with its counted test.
2. `Stay` and `See`, then one command a commit onto it.
3. The selection, then `logs`, `artifacts`, `shell`.
4. Identity's three answers (#190), then the hold.
5. Stopping: the one rule and `stopUnfinished`, with the service's test, and what `status` and a submission make of an unfinished cancel; `Launch` and the launch lock, with every provider's start and GitHub's rerun moved under it and the lock's file handed to each child; GitHub's branch name in the provider and in `clean` together, then what its `Stop` sees; Tart's `Stop`; the starter, its record and the helper that asks a group, then Local's `Stop` and Script's. The real-process test under sudo, of a signal and of the lock handed on as standard input, comes before Local's `Stop` is written.
6. The Tart minimum (#38).

After the source comparison ([#192](https://github.com/herbygillot/dockhand/issues/192)), #129's foundation, [#152](https://github.com/herbygillot/dockhand/issues/152)'s rename of `command/runcmd`, and [#197](https://github.com/herbygillot/dockhand/issues/197), which changes how the same GitHub provider polls. [#181](https://github.com/herbygillot/dockhand/issues/181) lands before the last queue cases.

## Not checked

- Nothing here was run. Local's and Script's survival is read from how they launch and from G15, which already kills a dockhand and finds its guest building.
- Whether the person's own process may signal a root sudo or its child on macOS, and whether that sudo keeps the guest in the starter's group. Rules 4 and 5 are written so that either answer fits, and the real test comes before Local's `Stop`.
- Whether a `tart run` that is handed the lock's file keeps it open for the VM's life.
- That a signal sent to "my group" by a member needs no number, and what the guest program can read of its own group and start time from `ps`.
- sudo's own behaviour, from its manual and upstream's source and not from a run: that it passes a SIGTERM on, that it closes descriptors from 3 up, and that it leaves its standard input to the command where no plugin logs input (exec_nopty.c and exec_pty.c were read by architecture, and exec_monitor.c, which sets the command's descriptors under a pseudo-terminal, by Codex). The sudo on a Mac is Apple's build of it. The real-process test covers each on this Mac.
- That the guest program's Tcl reads a file's device and number from `/dev/fd/0`. XNU's source has that entry answer with the open file's own attributes (`fdesc_attr`, bsd/miscfs/devfs/devfs_fdesc_support.c); not run.
- Which commands of a build keep the guest's standard input. The rule holds either way.
- Which lines `git push --porcelain` prints for a branch GitHub rejects, from Git's manual and not from a push. What GitHub's refusals of a rerun mean; none is relied on.
- That `kern.bootsessionuuid` changes at every restart and at nothing else.
- That macOS doesn't hand a process number out again while a process or a group carries it, which rule 6's remaining gap rests on. Codex read it in XNU's source (`forkproc`, bsd/kern/kern_fork.c); nobody made the numbers come round on a real Mac.
- How long GitHub can take to list a run after a push it accepted, and whether it ever drops one.
- The three faults named under "What the review changed" were each confirmed by a probe of Codex's against the real reader or the real branch of the code, none by a real build, restore or kill.
- Which Tart the M1 and the M5 run.
- `shell`'s entry and `ForgetKept` beyond their call sites, and every caller of `RunNamed`.

## Usage docs

- `docs/usage/checks.md`: `cancel` stops the work at every provider and says what it couldn't see end; a cancel that isn't finished, what `status` shows for it, and that `cancel` again looks again; what it does for a local prefix and for a script, the dockhand process each is started through, and what a script's own children are; the command to run where a root build can't be signalled; that a root build which ignores the stop is not killed outright, and what the line says then; that a root build doesn't start under a sudo set to log the command's input; that a push or a rerun whose fate GitHub never shows keeps the cancel's line; `artifacts` and `shell` take a check, not an attempt; `logs <port> --phase` works.
- `docs/usage/unattended.md`: serve holds a branch whose environment it couldn't identify, until it can or the person submits.
- `docs/usage/setup.md`: the Tart minimum, 2.41.1, where it describes installing Tart; and the name of a check's branch in the fork, where it describes the fork.
- `docs/architecture.md`: `Stay`, the selection, the three answers of a stop, and which providers stop.
