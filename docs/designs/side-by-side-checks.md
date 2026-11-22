# Side-by-side checks of one commit on different environments

A feature for v0.10.0, [#260](https://github.com/herbygillot/dockhand/issues/260). The issue says where it stands; this file is its design. Adopted 2026-11-20, after its one review pass; not yet built.

From field testing: while a GitHub-provider check of a commit ran, a Tart check of the same commit on another macOS could only be started with `--replace`, which canceled and threw away the running GitHub check. Separately, in review builds of 2026-11-19, running a second check of the same files—such as a variant check—deleted the first check's `--keep` clone, forcing the person to inspect one clone before starting the next check.

## What exists today

- **One active check per branch** (the code's comments call it decision 29; its record is the entry "Queueing a check beside a running check of the same files that doesn't cover it" in decisions.md): `checks.Service.queue` and `checks.Service.ActiveCheck` refuse any new branch check (`Run.BranchCheck()`, i.e. non-baseline and no cancel requested) while any other branch check on the same branch is queued or running, whatever files or environments it builds.
- **Admission (`BesideFollow` / `BesideReplace`):** `checks.Service.beside` follows an active check only when it is for the same revision, is not stopped, and `PlanCovers` holds; otherwise it returns `ActiveRunError` (whose wording for requesting another environment is being updated in [#259](https://github.com/herbygillot/dockhand/issues/259)), or stops every active run under `--replace`.
- **Per-run concurrency already exists underneath:**
  - Leases are per run (`RunResource`), and `checks.Service.Candidates` passes over a run whose own lease is held, or whose cancel was asked again lately, and nothing else—`serve` does **not** deduplicate active runs by branch, and its loop launches every candidate it can reserve a slot for.
  - Provider slot reservation (`checks.Service.Reserve` / `fullProviders`) counts live run holders per provider against `Capacity(provider)` (`TartCapacity` is 2, GitHub's default is 20, local/command default is 1).
  - Tart enforces the Mac's 2-VM limit inside each environment attempt (`tart.Provider.slot`), counting running VMs machine-wide and naming holders by check via `build.Holding`.
  - `evidence` already combines target results across every check of the same tree, and `evidence.Judge` receives the required environments from its caller.
  - `BranchStatus.Active`, `branchstate.Check.Active`, and `BranchJSON.Active` are already slices.
  - [#193](check-lifecycle.md) names GitHub check branches per check (`dockhand-check/<commit>-<check>`), so two concurrent GitHub checks on one commit do not collide on the fork branch or cancel each other's runs.
- **Single-active-check assumptions that remain:**
  1. `ForgetKept` removes every kept clone of the branch whenever any new check or retry is queued, even for the same files.
  2. `branchstate.Check.Stopped` and `Canceling` are single pointers overwritten in a loop by `JudgeStopped`.
  3. `branchstate.Check.Checking` returns a single run, so `view.CheckState` and `branchstate.Working` show only one active check.
  4. `branchflags.NamedOrLatest`, `engine.SelectBuild`, and `console`'s `l` key fall back to `LatestCheck` (newest run by number) when no check is named, which would silently pick one of two running checks for `cancel`, `wait`, or `logs`.
  5. `engine.PlanSubmit` checks for pending runs of the tree only when `EvidenceFor` found no evidence yet, so a fast sibling that already passed would let `submit` proceed while a slower sibling is still running.
  6. D17's governing ask (`evidence.AskOf`) takes only the single newest non-narrowed check of the tree, which makes side-by-side runs order-dependent when an older sibling ends without results.

## In short

```
dockhand check --on github -d              # queues check-42 (github: macOS 15)
dockhand check --on tart:26                # queues check-43 (tart: macOS 26) beside check-42
```

Two or more checks of the same branch may be queued or running at once when they check **the exact same files** (same captured `Source.Tree`) and build **disjoint environments** (`model.Environment`). Each request remains its own immutable run with its own lease, stay policy, and cancellation handle; their combined environments form the branch's required ask; and a kept clone survives for as long as the branch's files match what it was built from.

## Rules

### 1. Separate immutable runs, never mutating a running plan
A side-by-side request is admitted as a **new `model.Run` with its own immutable `model.Plan`**, not merged into an already-running check's plan.

Why: `model.Plan` is immutable from admission, each run already has its own lease and `Stay` policy ([#193](check-lifecycle.md)), `evidence` already merges across runs of the same tree, and separate runs let the person cancel, wait on, or inspect logs for the Tart run without disturbing the GitHub run.

### 2. Admission: same files and disjoint environments (amending the one-check rule)
Inside `checks.Service.queue` (under the store write transaction, so concurrent `dockhand check` processes cannot race) and `checks.Service.ActiveCheck`:

- A new branch check `want` for revision `wantRev` is checked against every active `BranchCheck()` run `have` on the same branch, by these cases in this order; the first that matches decides:
  1. **Different files (`haveRev.Source.Tree != wantRev.Source.Tree`):** **Conflict.** A branch does not run checks for two versions of its files side by side; the older run(s) must finish, be canceled, or be stopped with `--replace`.
  2. **Same files, covered (`PlanCovers(havePlan, wantPlan)`):** Under `BesideFollow` and while `have` is not stopped, **follow `have`** as today—do not queue a duplicate. A covering run that is stopped is not followed: it falls to case 3, whose refusal names `dockhand wait check-N`.
  3. **Same files, overlapping environment:** If any `model.Environment` in `wantPlan.Environments` equals any `model.Environment` in `havePlan.Environments` (exact equality on provider, platform, tools, and prefix), **conflict**—even when `--variants`, `--tests`, `--only`, `--fresh`, `--keep`, or `--set` ([#50](canary-checks.md)) differ. A `--variants` check still builds changed dependencies with default variants, so two concurrent checks on one environment would double-build shared dependencies and race on reuse. For multiple variant sets in one check, `--variants each` is the one-check path; a later sequential variant check reuses the shared builds.
  4. **Same files, disjoint environments:** **Admit side by side.** Every `model.Environment` in `wantPlan` is distinct from every `model.Environment` in all active `BranchCheck()` runs on the branch.
- **Baselines and canceling runs:** Unchanged—neither is a `BranchCheck()`, so neither blocks admission.
- **Stopped runs (driver died):** A stopped check of the same files on a *disjoint* environment does not block a new check on another environment (it stays visible as `stopped` in `status` so the person can `wait` or `cancel` it). A stopped check of different files or an overlapping environment conflicts as today (`ActiveRunError` with `Stopped: true`, naming `dockhand wait check-N`).
- **Refusal wording (starting from [#259](https://github.com/herbygillot/dockhand/issues/259)):** `ActiveRunError` and `besideWords` take [#259](https://github.com/herbygillot/dockhand/issues/259)'s wording as their base rather than rewriting it from scratch, extending `ActiveRunError` to carry all conflicting runs (`Runs`) and name the overlapping environment(s) when environments overlap on the same files.
- **No mixed cover-and-conflict state:** Because active runs on the same tree already have mutually disjoint environments, at most one active run can match any given environment of `want`.

### 3. `--replace` (`BesideReplace`) stops only conflicts
With multiple active runs on a branch, `--replace` replaces **only the conflicting runs**, never unrelated side-by-side runs:

- **Different files:** Every active branch check on the branch conflicts, so `--replace` stops all of them (asking once per running check on a terminal, as today).
- **Same files, overlapping some active runs:** `--replace` stops only the active run(s) whose environments overlap `want`, leaving disjoint sibling runs untouched.
- **Same files, overlapping nothing:** Nothing conflicts. `--replace` stops nothing, prints one line naming the untouched sibling(s) (`check-42 builds other environments and keeps running.`), and queues the new run beside them—consistent with [#195](https://github.com/herbygillot/dockhand/issues/195)'s rule that a flag with nothing to do is said and the command goes on.
- Stopping a specific non-conflicting run remains `dockhand cancel check-N`.

### 4. Kept clones: an asked clone is the person's, a failure clone is a cache (amended 2026-11-22)
The rule on keeping clones is [#291](https://github.com/herbygillot/dockhand/issues/291)'s, decided by the person on 2026-11-22 and built in v0.9.0 ahead of this page. It replaces this section's first form, where every kept clone lasted while the branch's files matched its tree. What this design adds to it is only how side-by-side checks treat the two kinds.

- **A clone kept by `--keep`** is not removed by any later check of the branch: not a sibling on another environment, not a check of changed files, not a retry. It goes when the person removes it, when its branch is removed, or when another `--keep` check of the same branch and release replaces it. A run stopped by `--replace` keeps its asked clone too; the replacing check says so.
- **A clone kept for a failure** follows the branch's files (`Source.Tree`):
  - built from a **different tree**, it is removed when a new check is queued, as today;
  - belonging to a **replaced run** (`--replace`), it is removed when that run is replaced;
  - built from the **same tree** by another check, a sibling on another environment or an earlier variant check on the same release, it **stays**, within #291's cap of four and seven days.
- `Admission.ForgetKept` takes the new check's tree and replaced run IDs, `(ctx, branch, tree, replaced)`, and considers failure clones only (the interface, its one implementation and its two callers change). A kept clone's tree is its run's: a leftover already names the run it was made for, and the run its revision.
- `Retry` passes the retried run's tree and `[previous.ID]`, so retrying a failed check removes only that failed run's own failure clone (plus any older-tree failure clones).
- **Visibility when several clones are kept:**
  - A branch can hold several clones at once (35–65 GB each).
  - `status <branch>` detail lists every kept clone of the branch individually (its check name, release, whether it was asked for with `--keep` or kept on failure, its size and age, and for a failure clone when it goes), with the `dockhand shell` and removal commands.
  - `clean` and `clean --plan` list each kept clone of the branch on its own line (check name, release, reason) rather than collapsing to one, and remove all of them when the branch is cleaned. `dockhand shell` already requires a check name ([#193](check-lifecycle.md)) and `--on` when one check kept several releases.

### 5. Sibling cohort and D17's governing ask (amending D17)
Under literal D17 ("newest ask"), starting GitHub then Tart vs. Tart then GitHub gives different answers if the older sibling ends in `attention` (no results): whichever started second becomes the sole ask and silently drops the older sibling's environments from what submission requires.

- **Recorded at admission, no time inference:** A check admitted beside one or more active `BranchCheck()` runs on the same tree records the IDs of those active siblings on its run record (`Beside []model.RunID` on `model.Run`, persisted in the store). A check started with no active siblings (or after `--replace` stopped all conflicts) records only the remaining non-replaced active siblings, if any; a check that replaces an older run does not inherit the replaced run's environments, but takes its place in the cohort: it records the replaced run's siblings as its own, finished ones included, so replacing one run never splits the others from the ask.
- **Union of the sibling cohort:** When computing the required environments handed to `evidence.Judge`, the caller starts from the newest non-narrowed check of the tree (as `evidence.AskOf` does today) and unions in the environments of every non-narrowed check in its cohort on the same tree. The cohort is the runs joined by `Beside` links read in either direction, through any number of steps, leaving out every run that was replaced or that the person canceled. Nothing inside `evidence.Judge` itself needs to change.
- **Order-independent outcomes for two siblings (`check-42` on GitHub + `check-43` on Tart):**
  1. While either is still queued or running: `Standing()` is `Checking`, and `submit` / `serve` wait.
  2. When both pass: `evidence` merges both into `Tested on`, and the union ask is satisfied.
  3. If either fails a build: D17's failed-cell rule blocks submission until rebuilt or explicitly accepted.
  4. If either (older or newer) ends in `attention`: its environments stay required by the union ask until the person rebuilds them, replaces the cohort with a new ask (`--replace`), or submits with `--no-check`.
  5. If the person cancels one (`dockhand cancel check-N`): that run leaves the ask, since canceling it is the person withdrawing that request, and the remaining siblings' environments are what is required. What the canceled run had already recorded still counts, as D17 has it for any result outside the ask: a pass stands, and a failure blocks submission until it is rebuilt or accepted.

### 6. Two-VM limit, provider capacity, and `serve`
No new capacity mechanism is needed; existing guards compose cleanly:

- **Disjoint providers (GitHub + Tart):** `checks.Service.Reserve` checks each provider independently (`providers.github.capacity` = 20, `TartCapacity` = 2). Both runs acquire their own leases and run concurrently with zero contention.
- **Two Tart checks on different macOS releases (e.g. `tart:15` + `tart:26`):** `TartCapacity` is 2, so both pass `Reserve` and run 1 VM each in parallel (2 VMs total).
- **More than 2 Tart VMs across two Tart checks (e.g. `check-42` on `tart:15,26` + `check-43` on `tart:14`):** Both pass `Reserve` (2 Tart checks ≤ `TartCapacity`). `check-42` boots 2 VMs; `check-43` waits inside `tart.Provider.slot`, names `check-42`'s VMs via `build.Holding`, and starts as soon as either VM exits. No deadlock is possible because each environment builds independently and releases its VM on completion without waiting on another VM.
- **A third Tart check on the same branch (`tart:13` while `check-42` and `check-43` hold Tart leases):** Admitted to the queue (disjoint environment, same tree), then waits in `checks.Service.Reserve` naming both `check-42` and `check-43` until a Tart slot frees.
- **`serve` and background drain:** `checks.Service.Candidates` never filters by branch, and `serve`'s loop launches each candidate it reserves a slot for without waiting for the last, so it drives side-by-side runs of one branch concurrently whenever provider slots allow. A test holds this.

### 7. `status`, `console`, `branchstate`, `submit`, and command ambiguity

- **`branchstate.Check` and `BranchStatus`:**
  - `Stopped` and `Canceling` become slices (`[]model.Run`, oldest first) instead of single pointers, so `JudgeStopped` never drops a second stopped or unfinished-cancel run.
  - Add `Check.CheckingAll` returning all active `BranchCheck()` runs (oldest first). `Standing()` is `CheckStopped` if `len(Stopped) > 0`, whatever siblings are still live, since a stopped check wants the person first; else `Checking` if `len(CheckingAll()) > 0`; else it evaluates `Latest`/`Evidence` as today. `status` lists the live siblings beside the stopped one either way.
  - `branchstate.Attend` emits one `CancelUnfinished` row per run in `Canceling` and one `StoppedCheck` row per run in `Stopped` (each naming its own `check-N`).
- **`status` overview & `console` row (`view.CheckState`, `branchstate.Working`):**
  - 1 active check: unchanged (`running (check-42)`, Next: `dockhand wait check-42`).
  - 2+ active checks: `Check` column groups by state (`running (check-42, check-43)` or `running (check-42), queued (check-43)`); `Next` in the `Working` bucket points to `dockhand status <branch>` so the person sees every active run and its individual commands.
- **`status <branch>` detail & `console` detail pane:** Lists every active check on its own line (name, state, provider/environments), plus `Latest` / partial `Evidence` already finished for the current tree when present, and every kept clone (§4).
- **`status --json` (`BranchJSON`):** `active_checks` is already a slice (`[]runJSON`); all active runs appear with no breaking schema change.
- **`submit` and `serve` gating:** `PlanSubmit` checks for active `BranchCheck()` runs of the current tree **even when `EvidenceFor` already found results** from a faster sibling check, blocking with `BlockCheckPending` naming all pending checks until they finish. `serve` already gates on `branchstate.Publishable()`, which is false while `Standing() == Checking`.
- **Ambiguous bare commands (`cancel`, `wait`, `logs`, `artifacts`, `shell`, `console`):**
  - **0 or 1 active `BranchCheck()`:** If 1 is active, bare commands select that active check (preventing a newer canceled run from shadowing a still-running check); if 0 are active, they select `LatestCheck` as today.
  - **2+ active `BranchCheck()` runs on the branch:**
    - Bare `dockhand cancel` and `dockhand wait`: **refuse as ambiguous**, listing each active check with its state and environments and the exact `dockhand cancel check-N` / `dockhand wait check-N` commands.
    - Bare `dockhand logs` and `dockhand artifacts`: if `--on <release>` is given on `logs` and matches exactly one active check, select it; otherwise **refuse as ambiguous**, naming each active check and its environments.
    - `dockhand shell`: already requires a check name ([#193](check-lifecycle.md)).
    - `dockhand console` (`l` key): with 2+ active checks on the selected branch, show `2 checks active: run dockhand logs check-42 or check-43` in the status line rather than guessing; `Enter` lists all active checks in the detail pane.

## Not taken

- **Merging a second request into a running check's `model.Plan`:** Breaks plan immutability, per-run `Stay`, and independent cancellation for no benefit since `evidence` already merges across runs.
- **Admitting two checks on the same environment when variants differ:** Both build changed dependencies with default variants and would duplicate that work; `--variants each` handles multiple variant sets in one check, and sequential variant checks reuse shared builds while keeping earlier clones (§4).
- **Running checks of two different commits of one branch side by side:** Confuses `status`, `evidence`, and kept clones.
- **`dockhand cancel --all`:** Naming `dockhand cancel check-N` is explicit and safe; can be added later if needed.
- **`dockhand wait` following multiple checks at once:** Kept single-run to match [#193](check-lifecycle.md)'s `checkrun.See`; `status` and `console` follow multiple runs.

## Tests

- **Admission & atomicity:** Same tree + disjoint environments (GitHub + Tart; Tart 15 + Tart 26) admits and runs concurrently; same tree + overlapping environment (including different `--variants` / `--tests` / `--only` / `--fresh`) refuses with `#259`-based `ActiveRunError` naming the overlapping run and environment; same tree + covered (`PlanCovers`) still follows under `BesideFollow` and refuses under `check -d`; different tree conflicts with all active runs; stopped run on disjoint environment does not block, stopped run on overlapping environment returns `ActiveRunError{Stopped: true}`.
- **`--replace`:** Stops only overlapping run(s) on the same tree and leaves disjoint siblings running; stops nothing and says so when nothing conflicts; stops all active runs on a newer tree.
- **Kept clones:** No check, sibling, retry or `--replace` removes a clone kept by `--keep`. For failure clones: a second check on the same tree preserves earlier ones; `--replace` removes the replaced run's; a check on a newer tree removes older-tree ones; `Retry` removes only the retried run's own. `status` and `clean` list several kept clones on one branch individually.
- **Union ask (D17):** GitHub-then-Tart and Tart-then-GitHub produce the exact same required environments; an older or newer sibling ending in `attention` keeps its environments required; a sibling the person canceled leaves the ask, and a failure it recorded first still blocks; `--replace` starts a fresh cohort without the replaced run's environments.
- **Capacity & `serve`:** GitHub + Tart run concurrently; two Tart checks both reserve slots (`TartCapacity = 2`) and a third waits in `Reserve` naming both; a check blocked on the 2-VM limit waits in `tart.Provider.slot` naming the holding check's VMs; `serve` drives two queued runs on the same branch concurrently.
- **Status, submit & ambiguity:** `JudgeStopped` preserves 2 stopped or 2 canceling runs and `Attend` emits a row for each; `status` overview, branch detail, `status --json`, and `console` show both active checks; `submit` blocks with `BlockCheckPending` when sibling A passed and sibling B is still running; bare `cancel`, `wait`, `logs`, and `artifacts` refuse as ambiguous with 2+ active checks (while `logs --on <release>` disambiguates when unique).
- **Acceptance:** One row starting a GitHub check (`-d`), starting a Tart check on the same commit beside it without `--replace`, confirming `status` shows both, bare `cancel` refuses naming both, and both results appear together in `Tested on` once finished.

## Usage docs

Updated in the same commit:
- `docs/usage/checks.md` (running checks on different environments of the same commit side by side; kept clones surviving across checks of the same files; `--replace`, `cancel`, `wait`, `logs`, and `--variants each`).
- `docs/usage/branches.md` (`status` and `console` with multiple active checks and multiple kept clones).
- `docs/decisions.md` (done at adoption: the one-check entry reopened for the same files on other environments, and D17's ask the union of checks started beside each other; the code's "decision 29" comments are corrected when this is built).
- Command help of `check`, `cancel`, `wait`, `logs`, and `clean`.

## Order

- **Kept-clone survival (§4)** follows [#291](https://github.com/herbygillot/dockhand/issues/291), which lands in v0.9.0; what remains here is `ForgetKept`'s tree and replaced-run arguments.
- **Side-by-side checks** build after [#193](https://github.com/herbygillot/dockhand/issues/193) (v0.9.0, check lifecycle) and [#259](https://github.com/herbygillot/dockhand/issues/259) (v0.9.0, active-check refusal wording). Composes cleanly with [#50](canary-checks.md) (canary checks) in v0.10.0.
