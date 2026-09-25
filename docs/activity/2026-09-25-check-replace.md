# 2026-09-25: check --replace

Decision 29, as Design v3 §7 words it: "Switching a queued or running check to another provider is explicit: `check --on github --replace`."

## What changed

- **One check of a branch at a time.** While one of the branch's checks is queued or running, `check` refuses. If that check covers these same files, it points to `dockhand wait`. Otherwise it names the check, what it is checking, and `--replace`. Before this, a second check simply queued beside the first.
- **`check --replace`** stops the branch's queued or running checks and keeps what they finished. It then checks the files now, on whatever `--on` names. The new run says which checks it replaces.
  - A running check is stopped only after asking, on a terminal. Without one, `--replace` is the consent, as the decision has it.
  - A check that serve is running is stopped by serve at its next step, as `cancel` does.
- **Scope.** The rule applies whatever the provider, not only when it changes. Two checks of one branch side by side would compete for the same results, and the older one is stale once the files change. Baselines are separate runs, and neither count nor are replaced.

## Tests

- **`check` after a queued check** is refused, naming the queued check. `--replace` then stops it and runs the new check, which fails and exits 2.
- **`check -d` twice** refuses the second. `check -d --replace` replaces the first. `cancel`, `wait`, and `logs` then act on the later checks.
- **`serve --drain`** drains the one queued check.

**Not tested here:** the question before stopping a running check, which needs a check held running by another process.
