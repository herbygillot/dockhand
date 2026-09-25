# Instance Coordination

> **Settled, 2026-09-25, in [Design v3 §11](design-v3.md#11-serve-the-queue-and-instance-coordination)**: sessions with pid-and-start-time liveness, fenced leases, one `serve` leader with standbys, controls applied by whoever can, and an event journal in the database. The socket service (option 1) stays unbuilt. The discussion below is kept as its record.


A design discussion from 2026-09-18, recorded before any of it is built. Nothing here is implemented; the [roadmap](roadmap.md) lists it under work that needs design.

## The idea

Several `dockhand` processes can run against one database at once: a `serve` driver in one terminal, a live `status` table in another, a `bump` in a third. Claims in state keep them from doing a job's work twice, but they all drive. The idea is that one process, the **leader**, does the driving and maintains a heartbeat for the others to watch, and every other process, a **standard** process, defers to it.

The first framing was that a command finding a live leader would queue its work and detach. The discussion moved it to a different rule: the command submits its work, lets the leader advance it, and **stays attached as an observer**, printing the same progress, verdict, and pull request URL it prints today, with the same exit code. Detaching on the presence of a leader would make one command mean two things depending on invisible ambient state, would break `--json` consumers that expect a final status, and would leave work queued behind a leader that had died. Observing keeps every command's meaning fixed; `--detach`, and possibly a `--queue` flag or configured default, remain the explicit ways to return early.

A **second `serve`** does not exit and does not compete. It stands by: it reports the active leader, waits, and takes over when the heartbeat lapses. That turns a stale leader into the standby's problem and makes `serve` safe to run under a login item without checking first. The live `status` table is a driver too and takes the same role: leader when none exists, standby otherwise.

## What the current code already gives

- **Attachment and driving are separate.** The process manager's attach loop reads status, reports, checks the milestone, then runs one cycle. Observer mode is that loop without the cycle call, so the change lands in one place and `wait` and `cancel --wait` inherit it.
- **Log following does not need the driver.** The Tart log reader opens `build.log` in the artifact directory beside the database; the GitHub reader uses its cache under a request lock. `--trace` works from an observer.
- **A driver's death is already survivable.** The VM outlives the process that launched it, claims expire, and a later cycle settles the result.

## Gotchas found in the walk-through

- **Claim owners are anonymous.** Every cycle generates a `cycle_<random>` owner unless the engine is given one, and the app never sets one. A standby cannot tell a dead leader's claim from a live one and so cannot break it early. The leader's identity has to become the claim owner, for every driver including the status table.
- **Takeover is gated by step timeouts.** Claim leases equal the step deadlines, ten minutes for preparation and fifteen for provisioning, plus grace. A leader dying mid-build leaves the job unclaimable until then regardless of how fast the heartbeat lapse is seen, unless takeover may void the claims of a session known to be dead.
- **Cancellation is applied inside the cycle.** `cancel` writes a control request that the next cycle applies. An observer never cycles, so with no live leader a cancel would sit unapplied; the fallback must cover controls, and quickly.
- **Half the progress lines belong to whoever works.** Job detail reaches an observer through state. "Cloning image", "starting VM", "waiting for the guest agent", and capacity waits are progress calls in the Tart provider and the cycle, printed on the driving process's stderr. Under a leader they go to its terminal, and the observer's terminal is silent for the minutes a VM takes to boot. They need to become recorded detail or events.
- **Cycle problems are driver-local too.** Claim-lost and conflict details ride on the cycle result; an observer sees none of them, so a leader failing a step with a non-durable error is invisible from the observer's side.
- **Repository scoping.** An engine is bound to one registered repository and `serve` advances one checkout; only `gc` reaches across all. A heartbeat is per database and per repository.
- **The manager's contract says otherwise today.** "Claims in state allow multiple callers; there is no resident singleton lock", and the competing-driver tests pin it. Coordination is a policy over that guarantee, and the tests should keep proving that two drivers stay correct when the policy is ignored.

## Concerns beyond the code

- **Sleep and clocks.** A closed laptop stalls the leader's heartbeat; on wake the standby sees a lapse while the leader also resumes. Claims arbitrate, so it is safe, but leadership flaps. Compare against the writer's recorded time, require several missed beats, and carry a start token in the identity so a restarted process cannot inherit a dead one's claims.
- **Writer pressure.** SQLite has one writer with a five-second busy timeout. Heartbeats every few seconds plus observers polling once a second fit easily, provided heartbeat writes stay out of long transactions and observers only read.
- **Version skew.** Two builds on one database are possible now and become normal once one process is resident. The observer's build may be older than the leader's; status rendering must tolerate what it does not understand, or refuse naming both versions.
- **Scripting.** The JSON envelope should say whether the command waited to completion or returned a receipt.
- **The leader's terminal.** It becomes where provider errors and capacity waits show for jobs submitted elsewhere; each line should name its job and submitting session.

## Groups, databases, and the disk

The database is the coordination domain. Two processes on two database files cannot see each other: claims, capacity, contribution identity, and a heartbeat live in the database. Some of what they touch is still shared, split along one line:

- **Coordinated through files, safe across databases:** branch locks in the checkout, PortIndex caches under the user cache directory, request locks in each provider's directory.
- **Coordinated through the database, blind across databases:** which contribution owns a port, and the publication locks beside each database. Two databases on one checkout could both bump the same port and both push the same fork branch.

Tart capacity belongs in neither list, which a first draft of this note got wrong. The admission check unions two sources before comparing against the limit: `machine.Running`, which asks Tart itself which VMs are running on that Tart home, and `Occupied` from the database. A second database sees the first one's VMs, because they are real VMs with names in `tart list`, and it sees one a person started by hand too. What it cannot see is a reservation: a VM is recorded occupied, then cloned, then started, and cloning a macOS image takes minutes. In that window the reservation is in neither the other database's records nor `tart list`, so two databases sharing one Tart home can each overshoot the limit by one. A lease file under Tart's home, taken when the reservation is recorded and released with the environment, would close that window; it is worth doing only for someone who actually runs two databases against one Tart home, which `--db` and `DOCKHAND_DB` permit.

One cheap guard for the rest: write the database path into the checkout under `.git` at registration, so a command can notice a checkout another database already owns.

On disk, nearly everything is either beside its database (the database and journal, `artifacts/tart`, the GitHub log cache, publication locks, backup staging) or named so two groups cannot collide (VM clones are `dockhand2-` plus a digest of pool and random request ID; branches carry a random job suffix; every capture, rebase, correction, and helper run uses a fresh temporary directory). The Tart base images are shared by everyone on the machine and are already guarded, which a first draft of this note also got wrong: `internal/tart/lock.go` keeps three locks per image name under Tart's home. `Adopt`, the single step that replaces an image, takes an exclusive write lock; `Machine.Clone` takes a shared read lock before cloning a base image into a per-attempt VM; and `provision.Run` takes a third, exclusive lock before it lists anything, so two `setup` runs on one image serialize end to end. A rebuild racing a clone is excluded already.

## Redesigns that would make it robust by construction

Three separable changes, in order of how much they alter the program:

1. **A client and a service.** A resident service owns the database, runs every cycle, drives providers, and writes state; the client submits over a local socket and subscribes to what happens. One driver by construction removes every takeover and visibility problem above. The cost is a lifecycle: an on-demand service the first client starts, that exits when idle, that `launchd` can keep resident; a versioned protocol between builds; and a fallback where the client embeds the engine when no socket exists, which is today's mode kept as the degraded case.
2. **An event journal as the source of truth for observation.** An append-only event table written by whichever process produces a transition, provider milestone, capacity wait, or problem, tagged with job, session, and time. Status becomes a projection, `--trace` a tail, the reporter a subscriber. It makes observers first-class and gives an audit trail the activity notes now approximate by hand.
3. **Sessions and fenced leases instead of per-cycle claims.** A row per running process with identity, start token, and heartbeat; every claim, control application, and provider reservation references its session; a session's death voids its leases at once. Leader, standby, and observer fall out of which lease a session holds. This is the least disruptive and is needed under either of the others.

Supporting all three: host state kept apart from repository state under a host-level lease directory; a stated compatibility contract between builds; and planner kept apart from executor in every phase, extending the intent checkpoint that publication already has, so takeover is a resume rather than a repair. Not worth reaching for: consensus libraries, a network database, or coordination across machines. The domain is one Mac, one person, a few terminals.

## Recommendation

Do the session and lease model first, since it is the foundation for anything else. Then decide between the service split and the journal by which pain shows up: terminals going quiet argues for the journal; driver contention and lifecycle confusion argue for the service. The two smallest pieces that unlock all of it are a stable per-process claim owner and a heartbeat record keyed by database and repository, and they are worth doing on their own because the owner identity alone lets a driver distinguish a dead peer's claims from live ones.
