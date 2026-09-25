# 2026-09-25: submit --check, --passing, and --ready

Design v3 §9 and §6.12: the three ways `submit` binds a person's decision beyond a plain submit.

## What changed

- **`submit --check`** previews the submission of the committed head and binds that commit.
  - It checks the commit, in the foreground or through `serve` as `check` does, then submits exactly that commit once the check passes. Running the command is the decision, so it asks nothing after the check.
  - On a terminal, the template's two questions are asked up front, so the person can walk away.
  - A failed check exits 2 and says nothing was submitted.
  - If the branch moved while the check ran, nothing is submitted.
  - A commit whose files a check already passed skips straight to the submission.
  - `PlanSubmit` gained `PendingCheck`, under which a missing check sets `CheckNeeded` instead of blocking. The preview's Checks line then says the check runs now.
- **`submit --passing`** finds every open branch that:
  - has commits;
  - has its latest check passed for exactly its files as they are, with nothing uncommitted;
  - is not yet on its pull request.

  It counts the ones that didn't pass and points to `status --attention`. For each branch that passed, it shows a one-line preview (title, check, PR) and asks `y`, `n`, or `d` (the diff). The template's two questions are asked once for the batch, unless the flags answer them. Without a terminal, it lists the branches and says how to submit one, since each is a person's decision.
- **`submit --ready`** submits as usual, then takes the pull request out of draft. Draft checks don't satisfy it, so the publication rule for a non-draft applies.
  - GitHub's REST API can't do this, so the forge client's new `MarkReady` uses GraphQL's `markPullRequestReadyForReview`, and leaves a pull request that is already ready alone.
  - `Engine.Ready` records the change and journals `branch.ready`.
- **Fixes found on the way:**
  - A resubmit no longer drops GitHub's last reading of the pull request from the record.
  - `plural` writes "branches".

## Tests

- **The forge client:** `MarkReady` sends the mutation with the PR's node ID once, and not for a PR that is already ready.
- **Command:**
  - `submit --check` with a failing script exits 2 and submits nothing;
  - after a passing check, `submit --passing` refuses without a terminal, then on one shows the diff and opens the pull request with the batch's answers;
  - a second `--passing` finds nothing;
  - `--ready` marks #34901 ready;
  - a new commit with `submit --check` is checked and pushed.

**Not yet:** `--passing`'s release notes (`r`) and the upstream archive comparison, which are step 11's.
