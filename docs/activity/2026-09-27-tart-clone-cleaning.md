# 2026-09-27: clean removes the Tart clones dead checks left

The [Tart provider's note](2026-09-26-tart-provider.md) left one gap: "removing a clone left by a driver that died when no later attempt comes, with `clean`". A check's clone, `dockhand-check-<run>-<release>-<attempt>`, is deleted by the attempt when it ends, and an earlier attempt's by the next attempt of the same run and release. A foreground `check` that is killed, or a check canceled while nothing drives it, gets no next attempt, and its clone stayed in dockhand's Tart home for good.

## Proven on the Mac first

In the scratch clone and database, a revision bump of `tree` was checked on Tahoe, and the dockhand process was killed with SIGKILL once its clone was up. The clone stayed, **still running**, with nothing driving it. It held one of the two VMs macOS runs at once, so every later check would have waited on it. This is the worst case, and the one the design had to handle.

## What clean does now

- **The engine** (`engine/clean.go`):
  - A provider whose environments can outlast their process implements `LeftoverProvider`, listing them by the reference each execution recorded and removing one when asked.
  - `PlanLeftovers` judges each by its check. One is removable only when an execution of this checkout names it and no live process holds its check's lease. One whose check is running is kept. One no check of this checkout made is also kept: the store is bound to one checkout, and another checkout's database may be using it.
  - `RemoveLeftovers` takes each check's lease before removing its environment, so no process can take the check up meanwhile. A check taken up since the plan was made keeps its environment. Each removal is journaled on its check.
- **The Tart provider** lists only `dockhand-check-` clones in dockhand's own Tart home. It stops a clone before deleting it, and refuses to remove any other name. The images, `dockhand-base-`, `dockhand-xcode-`, `dockhand-golden-`, and setup's disposable `<image>-check` clones, can't be removed this way.
- **The clone is named on the execution before it is cloned.** Before, a process that died between `tart clone` and recording the name left a clone no check could claim.
- **`dockhand clean`** lists what checks left beside the branches, whichever branches it was asked to clean, and removes it with the rest on `--yes` or a terminal's yes. `--json` has a `leftovers` list.
- **serve's daily cleanup** removes the same, and says what it removed.

## Measured

The killed check's clone was check-18's, in the scratch database:

```
$ dockhand clean
Left by checks
  remove   Tart clone dockhand-check-run-wmrzyhqc6tpz6gaz-tahoe-1, left by check-18
Nothing was removed; --yes removes these.
$ dockhand clean --yes
…
  removed  Tart clone dockhand-check-run-wmrzyhqc6tpz6gaz-tahoe-1, left by check-18
```

It was stopped and deleted in 4.4 seconds. Afterwards no check clones were left, and all 20 base, Xcode, and golden images were in place and stopped.

## Tests

- **Engine:**
  - A finished check's leftover is removable; one no check of this checkout made is kept.
  - A process that takes the check up between the plan and the removal keeps it, "check-1 is running".
  - Once the lease is free, only the check's own is removed, and the removal is journaled on the check.
  - serve's cleanup removes it too.
- **Tart provider:**
  - Only `dockhand-check-` clones are listed.
  - Removing one stops it first.
  - The base, golden, Xcode, and setup-check images are each refused, and nothing else was stopped or deleted.
  - A clone whose cloning fails was still named on the execution first.
- **Command:** `clean` shows, keeps, and removes a check's leftover, in text and JSON. A test hook registers a provider with leftovers, as the other hooks in `settings.go` do.

## Found, not done

- **A killed foreground check stays "running".** After the kill, `status` showed check-18 as running and `queue` counted it, though nothing drove it. That is how the runner leaves a run whose driver died: the next `serve` resumes it as a new attempt, and `cancel` ends it, as it did here. Without serve, nothing says so.
- **Clones another checkout's database made** are kept, and are only reported. Removing one by hand is `TART_HOME=~/.dockhand/tart tart delete <name>`.
