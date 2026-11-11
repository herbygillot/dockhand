# A check's lifecycle: how long a command stays, which build it means, what an environment is, and how it stops

A core item for v0.9.0, [#193](https://github.com/herbygillot/dockhand/issues/193). The issue says where it stands; this file is the design, architecture's of 2026-11-11, read at main 8cad319a3. It is a draft: Codex reviews it against the code before the person sees it for adoption (the person's "I'd like Codex to co-design and help review as well", 2026-11-11 05:48Z, in the architecture thread).

## In short

Four things about a check are each decided in several places today. Each gets one value, made once and passed.

- **Staying.** How long a command stays with a check is chosen by which of four helpers it calls. It becomes one value, `Stay`, and one function.
- **Selection.** `logs`, `artifacts` and `shell` each work out which check, attempt, port and phase they mean. One resolver answers, and a command refuses by name what it can't serve.
- **Identity.** An environment whose identity couldn't be read is treated as one that has none ([#190](https://github.com/herbygillot/dockhand/issues/190)). It becomes one of three: known, none, or unread with its cause.
- **Stopping.** A cancel that no driver attends stops only GitHub's runs. Every provider whose work can outlive its driver stops it, and a stop that fails doesn't keep the others from being tried.

Two smaller issues close with it: [#189](https://github.com/herbygillot/dockhand/issues/189), earlier builds loaded where nothing reads them, and [#38](https://github.com/herbygillot/dockhand/issues/38), the Tart minimum.

No flag or command is added.

## Staying

**Today,** in `internal/command/checkrun/run.go`:

| Helper | What it does | Called by |
| --- | --- | --- |
| `RunQueued(…, enqueue=false)` | to the end: drives the check here where nobody leads it, follows whoever does | `check`, `check --baseline`, `retry`, `review`, `ship`, the configured baseline after a check |
| `RunQueued(…, enqueue=true)` | queues and returns; starts a worker where nobody leads | `check -d`, `check --baseline -d`, `retry -d` |
| `QueuedFor(…, bound)` | queues, starts a worker where nobody leads, follows for the bound | `check --for`, `check --baseline --for` |
| `WaitFor(…, bound)` | for a check that already runs: reports one that ended, else follows for the bound and refuses as still running | `wait --for`, `check --for` where a covering check runs |
| `Wait` | for a check that already runs: to the end, driving here where no serve runs | `check` and `review` where a covering check runs |

Ten call sites of the first four at this commit, in five files. Each command picks a helper by its own flags and by whether the check it found was new or already running.

**The change.**

```go
// Stay is how long a command stays with a check it asked for.
type Stay struct {
	Detach bool  // -d: queue it and return
	For    Bound // --for: follow it this long; zero is to its end
}

func See(ctx context.Context, e *engine.Engine, run model.Run, streams term.Streams, places config.Places, stay Stay, drain func() (int, error)) error
```

One rule, the one the helpers hold between them:

- To its end: drive it here where nobody leads it; follow whoever does.
- Detached or bounded: never drive it here. Start a worker where nobody leads. Return at once, or at the bound with the still-running refusal.
- A check that had already ended is reported, whatever the stay.

Whether the check is new or one already running that covers the request is what admission answered, and `See` reads it from the run. No command chooses a helper. `Follow` and `FollowFor` stay as `See`'s two halves.

`retry` and `review` don't gain `--for`. The value makes that a line each, and it is another issue's to ask.

## Selection

**Today.**

| | Nothing named | A check | One attempt, or a provider's run | Port | Phase |
| --- | --- | --- | --- | --- | --- |
| `logs` (runcmd/queue.go:134) | the branch's newest check in any state | yes | yes, through `RunOrExecution` (engine/status.go:895) | a port; else the failed build, then failed tests, then the latest | `--phase` |
| `artifacts` (runcmd/artifacts.go:19) | the same newest check | yes | accepted, and the attempt is ignored | none: every passed cell of the evidence | none |
| `shell` (engine/shell.go:43) | refused, with a hint of what is kept | yes, by `RunNamed` | not resolved | `--port`; else the first changed | none; `--on` picks the release |

Three resolvers. A name that one accepts another ignores or can't read.

**The change.** One engine function resolves once what was named (nothing, a check, one attempt, a provider's run) with the port, the release and the phase asked for, into one value. Each command then says what it serves:

- `logs` serves all of it, and keeps its own default for the port.
- `artifacts` serves nothing named, or a check. Given one attempt or a provider's run it refuses and names the check: `artifacts lists a check's passed builds; <name> is one attempt of check-N. Run dockhand artifacts check-N`. Today it answers for the whole check as though the attempt had been read.
- `shell` serves a check, and now one attempt: where a check kept several clones, naming the attempt chooses one, where today it refuses and `--on` is the only way. It still needs a kept clone or a running guest, and still refuses nothing named.

## Identity

**Today.** `Service.Identity` (checks/service.go:196) passes a read error through. `Service.Identities` (:206) leaves an environment out both when the read failed and when the provider can't say. Its readers then treat an unread environment as one with no identity: evidence takes its results as they were (`current`, evidence/evidence.go:993), and the driver's `anotherCanReuse` (checks/runner.go:394) doesn't count it. The driver itself already keeps them apart for an attempt (`unreadIdentity`, runner.go:1021).

**The change.** `Identities` answers, for each environment, known with its value, none, or unread with the cause.

| Reader | none | unread |
| --- | --- | --- |
| Evidence | results taken as they were, as today | taken as they were, and the words say the identity couldn't be read and why |
| A submission nobody attends | goes on, as today | choice 1 below |
| `anotherCanReuse` | not counted | not counted |
| The driver's attempt | builds without reuse | fails the attempt as the environment's and reads again, as today |

## Stopping

**Today.** `RequestCancel` (checks/runner.go:214) records the cancel. A live driver sees it and stops its own work. With no live driver the canceller takes the run's lease and calls `stopUnfinished` (:258), which asks each `buildenv.Stopper` to stop each recorded reference, and returns at the first failure without trying the rest.

| Provider | Reference saved | A cancel no driver attends |
| --- | --- | --- |
| GitHub Actions | the run's URL, once the run appears after the push (ghactions/provider.go:263) | cancels the run. A driver that died between the push and that point left a run with no reference, and nothing stops it |
| Tart | the clone's name, before the clone exists (tart/provider.go:852) | nothing: Tart is no `Stopper`. The guest runs on until `clean` removes it as a leftover (engine/clean.go:975) |
| Local | the check's directory, before the guest program starts (local/local.go:239) | nothing. `status` says how to put the prefix back |
| Script | after its result (script/script.go:202) | nothing, and nothing is left: it runs inside the driver |

**The change.** Providers keep their optional interfaces.

1. `stopUnfinished` asks every reference and reports each failure. The check stays unfinished with its cancel recorded, and `dockhand cancel` again asks again, as today.
2. Tart is a `Stopper`: it stops and deletes the clone its reference names, and a clone that isn't there is done. The canceller already holds the run's lease, which is what `RemoveLeftovers` waits for.
3. GitHub saves a reference before the push: the check branch and commit, which it derives before pushing. The run's URL replaces it when the run appears. `Stop` given the first form cancels the runs GitHub lists for that branch and commit in the fork.
4. Local: choice 3 below.

## The two smaller issues

- **#189.** `driver.earlier` (checks/reuse.go:31) is called before every attempt (runner.go:629), and its result is read only before the first attempt of a check that isn't fresh. It is called only there.
- **#38.** The minimum rises from 2.39.0 to 2.41.1 (`ASIFVersion`, tart/images.go:34; the person's "Yes on raising the Tart minimum version", 2026-11-11). The retry of a listing that raced a delete (`ErrListingRaced`, images.go:84) and trusting a delete only by the clone's absence (tart/host/machine.go:183) go. The floor is refused at setup and before a check, so the Tart on each Mac that runs checks is read before this lands.

## Choices the reviewer should push on

1. **An unread identity and an unattended submission.** A source that couldn't be read holds serve, and the rule beside it says what couldn't be checked holds (`sourceUnknownRule`, engine/servesubmit.go:321). By the same rule an identity that couldn't be read would hold serve too, where today it goes on. #190 found no case where the difference changes a verdict; this would be one. I lean to holding.
2. **`artifacts` given one attempt.** Refuse, as above, or list that attempt's environment alone. I lean to refusing: its list is of judged passes, which belong to the check.
3. **Local as a `Stopper`.** Its guest program runs in a process group of its own and records its owner's process (`start`, local/local.go:488). I haven't shown whether it outlives a driver that is killed. If it does, Local stops the group and then runs its own put-back; if it doesn't, the page says so and Local stays as it is.

## Tests

- **Staying:** one table: a new check, one already running, one already ended; to the end, detached, bounded; nobody leading, serve leading, another command leading. Each row names who drives, what is printed and how the command ends.
- **Selection:** each form of name against each of the three commands: served, or refused in the sentence above.
- **Identity:** the three answers against the four readers.
- **Stopping:** in the provider packages: several references with one that fails, a Tart clone already gone, a GitHub run with only the early reference, a second cancel after a failed stop.
- The fault sweep's check and cancel positions ([#129](https://github.com/herbygillot/dockhand/issues/129)). The sweep leaves the Local and Tart lifecycle outside, so the real acceptance rows on the Mac prove those.
- The earlier fixes' tests stay: the bounded baseline, the selected attempt's log, retry's cleanup, a detached cancel.

## How it is built

1. #189, which changes reads only.
2. `Stay` and `See`, then one command a commit onto it.
3. The selection, then `logs`, `artifacts`, `shell`.
4. Identity's three answers (#190).
5. `stopUnfinished` over every reference; Tart's `Stop`; GitHub's early reference; Local's, if choice 3 says so.
6. The Tart minimum (#38).

After the source comparison ([#192](https://github.com/herbygillot/dockhand/issues/192)), #129's foundation, and [#152](https://github.com/herbygillot/dockhand/issues/152)'s rename of `command/runcmd`. [#181](https://github.com/herbygillot/dockhand/issues/181) lands before the last queue cases.

## Not checked

- Whether a local build outlives its driver (choice 3).
- That GitHub lists a run by branch and commit soon enough after a push for step 3 of Stopping to find it.
- Which Tart the M1 and the M5 run.
- `Follow` and `FollowFor` beyond their signatures, and every caller of `RunNamed`.

## Usage docs

- `docs/usage/checks.md`: `cancel` stops a Tart guest whose driver is gone; `artifacts` takes a check, not an attempt; `shell` takes an attempt.
- The Tart minimum: no usage page names a Tart version today. `docs/usage/setup.md` gains the minimum, 2.41.1, where it describes installing Tart, since an older one is now refused.
- `docs/usage/unattended.md`, if choice 1 is taken: serve holds a branch whose environment it couldn't identify.
- `docs/architecture.md`: `Stay`, the selection, and which providers stop.
