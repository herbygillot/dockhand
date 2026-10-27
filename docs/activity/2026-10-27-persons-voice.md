# The person's voice: a decision on one concern

2026-10-27, Then's item 13 (the direction synthesis's B; field testing's
outdated-ports session asked for a reviewed hold acknowledged with a note
submit carries into the pull request). The defaults were taken with
prioritization's correction and addition, 2026-10-27.

- **`dockhand decide <rule> [<port>] (--because "…" | --waiting "…")`**,
  on the branch -b, -p, or --pr names. The rule is one of `HoldRules`.
  The port is the second argument, or -p's, or the branch's one port.
- **The record**: a `FindingConcern` decision, its subject "jq
  license-changed" (`model.ConcernSubject`), its words the reason, and
  its answer `Accepted` or the new `Waiting`. The evidence is the tree of
  the port's own directory (`portEvidence`), so a change to its Portfile
  or files/ lapses it, another port's change doesn't, and tidy's rewrites
  keep it. Deciding again on the same evidence replaces it (the store's
  upsert). No migration: words have been kept since schema 40.
- **What it does**:
  - An acceptance lets its concern go for a submission nobody looks over
    (`undecided`, in `SubmitPlan.Held`, `ApplySubmit`, and status's
    Held). It is still said: under "Decided" in status and the preview,
    and in the description as a quoted "Reviewed:" block after the
    Author's note (prdescription `reviewedWords`).
  - Waiting is a submit block (`BlockAwaiting`) and the verdict's
    `awaiting` row, whose Next is the decide that answers it.
- **Where a hold shows, its answer is named**: the held row's Next is
  `dockhand decide <rule> <port> --because "…"`, and submit's preview
  gives "answer it: dockhand decide license-changed -p jq --because …"
  under each of serve's holds, which now include undeclared links (the
  link hold's preview words, missing since 96af4d95).
- Test: TestADecisionOnAConcernCarriesThePersonsReason (waiting blocks;
  acceptable releases serve and gives Reviewed; a change to the port's
  files lapses it).
