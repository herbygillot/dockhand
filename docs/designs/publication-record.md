# The publication record: what a submission set out to do, and what it still owes

A core item for v0.9.0, [#191](https://github.com/herbygillot/dockhand/issues/191). The issue says where it stands; this file is the design. Architecture merged it on 2026-11-11 from two proposals written apart from one problem statement, architecture's and Codex's (the person's "I'd like Codex to co-design and help review as well", 2026-11-11 05:48Z, in the architecture thread). Both read the submission code, which is the same at main 0095ce9b2. Where the two differed, the section "Where the two proposals differed" says which was taken and what decided it. It is a draft: Codex reviews it against the code before the person sees it for adoption.

## In short

- Today a submission records nothing until everything it does on GitHub is done. After a failure in between, the next run works out what happened from the fork, a search and the description's text. It gets three things wrong: serve's daily count ([#155](https://github.com/herbygillot/dockhand/issues/155)), whose text the description is ([#157](https://github.com/herbygillot/dockhand/issues/157)), and a reviewer request that failed.
- A branch gets one **publication**: the commit a submission set out to publish, what it meant to write, what the person asked of it, and the steps it still owes. It is recorded before the first remote effect and cleared when nothing is owed.
- A step that is owed may have happened. Nothing is done again until the fork or GitHub has been read and shows it isn't done.
- There is no separate "resume". A later submit settles the old publication by reading, plans as always from what it found, and records a new one. For the same commit, what the person asked the first time stands unless this run says otherwise.
- serve's place in the day's count is taken in the transaction that records the publication. The count moves from events, which `clean --delete` removes with a branch, to a small record of its own.
- What a person sees change: "ask them to review again?" is asked before the push, with the plan. A pull request is marked ready before its reviewers are asked, where today it is after. A submit that follows a failed one says what the failed one did. `status` names a submission that isn't finished. No command or flag is added.

## Today

`ApplySubmit` (internal/engine/submit.go:1269), in order:

1. Refusals with nothing done: the gate, the description's length, the local head still being the planned commit, `unchangedSince` (:338) and `masterUnchanged`.
2. `draftFirst` (:1456), where `--draft` is asked of an open pull request. It is the first remote effect of such a submission.
3. The push, guarded by the remote head the plan saw. Failpoint `submit.pushed` (:1323).
4. Open, or edit the title and description. A create that failed is read back by head (`createdAnyway`, :1444). Failpoint `submit.created` (:1350).
5. One transaction: the pull request's record, the title and note, the tested statements, the accepted failures and standing decisions, and the events `branch.submit`, `branch.push`, and `serve.submit` where serve submitted.

Then, in the command, `finishSubmit` (internal/command/submit/submit.go:253) calls `Apply`, whose `followUp` asks the reviewers, and only then `Engine.Ready`. A reviewer request left over from an earlier submit is inferred from events (`RerequestDue`, submit.go:1588).

serve counts `serve.submit` events since midnight (`servedToday`, internal/engine/serve.go:1363). `ForgetBranch` (internal/store/sqlite/prune.go:243) deletes every event of an ended branch, the day's among them. So a pull request that serve opened, and that was merged and cleaned the same day, leaves the count.

## The record

```go
// Publication is a submission of one commit that dockhand has begun and
// has not yet seen finished.
type Publication struct {
	ID       string     // random; a late process tells its own from a successor's
	Commit   ObjectID   // the commit being published
	At       time.Time  // when it was recorded
	By       string     // the GitHub login it was recorded under
	Served   bool       // serve recorded it: it holds a place in serve's count while open is owed
	Head     string     // owner/repo:branch it is pushed to
	Expected ObjectID   // the head branch as the plan saw it; empty where it didn't exist
	Title    string     // the title it set out to write; empty where it writes none
	Written  string     // dockhand's parts of the description, as it set out to write them
	Asked    Asked      // what the person asked of this submission
	Review   *ReviewAsk // the reviewers to ask again, where the push calls for it
	Owes     []Step     // draft, push, open, edit, ready, rerequest: still owed, in order
}

// Asked is what a person chose for a submission, kept so that finishing
// it needs none of it typed again.
type Asked struct {
	Draft, NoCheck, SkipNotification, Rewrite bool
	Types []string
}

// ReviewAsk is "ask them to review again?" and its answer.
type ReviewAsk struct {
	Logins   []string
	Answer   Answer    // yes, no, or not asked yet
	Reviewed time.Time // GitHub's time of the review the answer was given for
}
```

- It is a nullable value on the branch beside `PullRequest`, stored as one JSON column on `branches`, as `pr_observed` is.
- One at a time. A new one is recorded only after the old one is settled.
- `PullRequest` keeps its meaning, sharpened: what dockhand knows GitHub has from it (`Number`, `Head`, `Pushed`, `Body`, `Draft`). The publication is what it set out to do. The two can't disagree on a fact, because a step leaves `Owes` in the same transaction that writes its result into `PullRequest`.
- Not in it, because a record already holds each and the first transaction now writes them there: the accepted failures and standing decisions (`AddDecision`, with the commit as their evidence), and the title, the note and the tested statements (the branch's own fields).
- Not in it: the basis each write states to the gate. It is built from records at each step. A stored basis would be a claim, not evidence.

## Three rules

1. **Observe, then act.** A step still owed may have happened. Nothing is done again until a read shows it isn't done. "Unknown" is therefore not a stored state: it is an owed step whose read failed.
2. **Settling only reads.** Accounting for an unfinished publication reads the fork and GitHub and writes dockhand's own record. It never writes to GitHub.
3. **One path.** A submission of a branch that has a publication settles it, then plans, then records a new publication. Push, open and edit are worked out by the plan from what settling found. Nothing runs from the old record without a plan.

## The steps

| Step | Owed when | The read that settles it |
| --- | --- | --- |
| `draft` | `--draft` is asked of an open pull request | `Inspect`: it is a draft |
| `push` | the fork's branch isn't at `Commit` | the head branch: at `Commit`, done; at `Expected`, not done; anything else, someone else pushed, and the publication ends there |
| `open` | no pull request is open for the head | `Find` by head: an open one by `By` is the one this publication opened; one closed or merged that GitHub says was opened after `At` was opened and is over; otherwise not done |
| `edit` | the title or the description is to change | `Observe`: see "The description" |
| `ready` | `--ready` was asked | `Inspect`: it isn't a draft |
| `rerequest` | the plan pushes to a pull request whose reviewers requested changes, and the answer is yes or not given yet | the pull request's pending review requests, a new read: see below |

A step leaves `Owes` only in a transaction that also writes its result:

- `draft`, `push`, and `open` or `edit` leave together, in today's one transaction after the pull request is written.
- `ready` and `rerequest` each leave in the transaction that follows them, as `Ready` and `RequestReview` write today.
- Where that transaction fails after the effect, the step stays owed and the next read finds it done. Nothing goes on to the next effect past a record that failed.

So a plain submission costs one store write more than today: the first one.

**The order changes.** Ready comes before the reviewers are asked. Today it is the other way round.

**The reviewer request.** GitHub lists a pull request's pending review requests, and a reviewer leaves that list when they review. dockhand reads none of it today: `forge.PullRequestStatus` (internal/forge/pullrequest.go:63) has the reviews and no requests. The status read gains them. Then, for an owed `rerequest` with a yes:

- GitHub's review time is no longer `Reviewed`: someone reviewed since, the answer was for another review, and the step is dropped and said;
- a login with a pending request has been asked;
- a login without one hasn't, and is asked;
- the read failed: the step stays owed, is said, and nothing is sent;
- GitHub refuses a request outright, as for a login that can't be asked: the refusal is said and that login leaves the step, so it isn't owed for ever.

**When settling runs.** When a submission of the branch is planned (`PlanSubmit`, submit.go:445), so the preview is of the settled record. At the start of serve's pass, for every publication serve recorded. Before a branch is archived or forgotten. And where `status` reads a pull request anyway, on `--refresh` or when its reading is more than five minutes old, that reading settles what it can. `status` makes no read of its own for a publication.

## Who continues what

- A person's `submit` or `ship` of a branch with a publication settles it and shows its plan as always. The plan says what the earlier submit did and what it carries. No flag.
- **The same commit.** Each thing the person asked the first time stands where this run doesn't say otherwise: the destination, `--draft`, `--no-check`, the types, `--skip-notification`, a `ready` still owed, and the answer about the reviewers. A flag given on this run replaces its own: `--draft` drops a carried `ready`, and `--ready` a carried draft. For this the command tells the engine which flags were given, since a flag left out and a flag set to false are different.
- `--rewrite-description` is the exception. It was asked of a description the person was looking at. A continuation doesn't act on it; it says the earlier submit asked for it and how to ask again.
- **A recorded answer** is good for the commit and the review it was given for, whoever runs next. A yes is acted on with nobody there. A no is not asked again. With no answer, an attended run asks before the push and an unattended one leaves the step owed and says so. `submit.rerequest_review = "always"` is the person's standing word, as today. Nobody's yes is invented.
- **Another commit.** `ready` and `rerequest` were for the old head and are dropped, with a word. A push of the new head to a pull request with changes requested makes a new question.
- **serve** settles and continues only publications it recorded, on branches it prepared. It never acts on a person's.
- **A person's submit of a branch serve was publishing** takes it over. If settling finds serve's pull request, it is recorded and counted as serve's. If not, serve's place is released and the person's opening isn't counted.
- **Another account.** A publication recorded under another login than the one dockhand acts for now is settled by reading and replaced. Nothing it asked carries.

## A new submit replaces an old publication

"Accounted for" means every owed step of the old publication has been read and found done or not done. A `rerequest` for an old head is the exception: it is dropped, not read. If a read fails, the new submit is refused with nothing done and says which read.

The new publication replaces the old in the first transaction. From the old one it takes what settling wrote into `PullRequest`, serve's counted opening if one was found, and for the same commit what "Who continues what" lists. The old record is not kept; the events say what it did.

## serve's count

- **A reservation** is a publication with `Served` that still owes `open`. Until it is settled it counts in every day.
- **An opening** is a row in a new table, `serve_openings`: the pull request and the time GitHub says it was opened. One row for a pull request, by a uniqueness constraint. It is not deleted with a branch.
- **The limit check and the reservation are one transaction**, the first one: the day's openings plus every other branch's reservation must be under the limit, or nothing is written. SQLite has one writer, so a second pass or a second process can't interleave.
- **A reservation becomes an opening** in the transaction that takes `open` out of `Owes`. The time is GitHub's `CreatedAt`, which `Find` and the create's reply both carry. A found pull request with no creation time isn't counted yet, and its reservation stands.
- **The pass settles first.** Before any candidate, serve settles every publication it recorded, whatever the branches' order.
- **A publication that still owes `open` goes first**, under the place it already holds. If GitHub answers that the pull request exists, `createdAnyway` finds it and it is counted. So a search that lagged behind a lost reply can't lead to a second pull request, and no place is given up on one read.
- **A place is released** when a person takes the branch over, when the branch ends, or when serve can no longer submit the branch and a read on a later pass shows no pull request.
- **The day** is worked out from the opening's time at each count, with `dayStart` as today, so a pull request opened before midnight frees today, and one opened after it counts today.
- `serve.submit` stays as a line in the journal. Nothing reads it as state, and cleanup's rule that keeps the day's events for the count goes.
- Nothing but serve opening a pull request counts, as today.

## The description

The first transaction comes after `unchangedSince` and `masterUnchanged`, the last reads that can change what is written, and before `draftFirst`. `Title` and `Written` in it are final.

`prdescription.Merge` (internal/macports/prdescription/prdescription.go:916) rewrites each of dockhand's three parts, the Description, the Type(s) and everything from Tested on down, only while GitHub's part is still what dockhand last wrote there. #157 is a part GitHub took whose record was lost: it matches neither the last record nor, after a new commit, the fresh text, so it reads as a person's.

Settling makes "last written" true, part by part. For each of the three parts: where GitHub's part reads as the publication's `Written` part, that write landed, and `PullRequest.Body` takes that part. A part that reads otherwise is left as recorded. The merge then runs as it does today. A part a person edited matches no record and is kept, whether they edited the old text or the new. The title needs no record: where GitHub's is the one the publication set out to write, it landed, and otherwise today's rule for an edited title decides.

For someone else's pull request dockhand writes no title or description, as today, and `Title` and `Written` are empty.

## Two processes, and a late one

- A lease from `internal/coord` on `publication/<branch id>`, held from before the first transaction to the end. `submit` and `ship` open a foreground session for it, as a check run in the foreground does (internal/checks/session.go:106); serve has its own.
- A live holder: the second caller is refused with nothing done and told who holds it. A dead holder's lease is taken, the generation rises, and the taker settles.
- Every write of the publication is `Fenced` and checks `Publication.ID`, so a process that was paused can't record on a claim it lost. Settling's writes are fenced too; where a live process is submitting the branch, a plan is refused and `status` shows what is stored.
- Between a plan and its `ApplySubmit`, another process may have settled or replaced the publication. `ApplySubmit` checks that the branch's publication is the one the plan saw, and otherwise refuses as it does for a head that moved.
- Before each remote write the process checks its lease. One that lost it stops and says another dockhand took the submission over.
- The window between that check and the call can't be closed, and a call already sent can land after its process died. Per effect: the push is guarded by `Expected`; GitHub refuses a second open pull request for one head; an edit writes the same text; marking ready twice is harmless; a reviewer request could repeat. This design states that and builds no more. It is `coord`'s own rule: "External calls it makes after resuming are guarded by the providers' idempotency and reconciliation, not by leases."

## The operation, and what callers see

`ApplySubmit` stays the one operation and keeps its name. It records the publication, makes each owed step, and returns what `Submitted` has today plus the ready outcome, who was asked, and what is still owed: each step with its reason (it failed, with the error; it awaits an answer; it couldn't be read). A result comes back with an error too, as today. A second function settles and only reads.

- **Lines.** What was done, as today, then a line for each owed step and the command that finishes it. `--json` gains `pending`, a list of step and reason.
- **Failures.** Today's sentences, now true by the record: "pushed … but could not open the pull request: …; dockhand submit finishes it."
- **A step that failed** leaves the command unsuccessful, as today. One that only awaits an answer doesn't.
- **`status`.** A branch with a publication reads as a submission that isn't finished, with what is owed, and its next step is `dockhand submit`.
- **Into the engine:** who is owed a re-request, which the plan gains; the request and marking ready, as steps. `followUp`'s left-over branch, `RerequestDue` and `finishSubmit`'s call to `Ready` go.
- **Stays in the command:** asking, before the push; the wording; the JSON. `ship` asks with its own confirmation at its start, and the answer rides with its submission; a ship that ends before its check does loses it with the rest of what it holds, which is #195's to keep.

No message names `Owes` or a step's internal name.

## Existing databases and branches

- The schema adds the column and the table. The table is filled once from the `serve.submit` events still kept, each with its branch's pull request, so the day's count doesn't start again at an upgrade. No branch row changes. A branch recorded before has no publication and owes nothing.
- A branch whose last submit failed part-way before the upgrade takes the path it takes today: `PlanSubmit` finds its pull request by head (`destination`, submit.go:776).
- `RerequestDue` goes with nothing in its place for old branches. A re-request that failed or went unanswered before the upgrade, and was never retried, is not offered again. `status` still shows who requested changes.
- `clean` refuses an open branch, as today. Archiving and forgetting settle first, and refuse if they can't. A branch observed merged or closed loses its publication in the transaction that records that state.
- `undo` moves a branch's history and removes nothing remote (`Restore`, internal/engine/restore.go:23). Where it moves the head off `Commit`, the next submit treats the publication as one for another commit.
- The publication is on the branch's row and is never pruned. Rows of `serve_openings` older than yesterday may be.

## The gate

Each step states a basis to the gate (`internal/outward`, to be `internal/remotewrite` under [#144](https://github.com/herbygillot/dockhand/issues/144)), built from records when the step is made.

- Push: `OwnFork`, or `Invited` for someone else's pull request, as today. Open: `OwnFork`.
- Edit, draft and ready on a pull request dockhand opened: `Made`. The record behind it is the pull request's own, written with its opening, or a publication that recorded before any effect that it would open one for this head, with the account as the pull request's author.
- A pull request found open for the head with no publication behind it gets a mark on its record, `Found`. dockhand didn't open it as far as any record says. A person's submit of that branch names the action, so its writes state `Named`, and the plan says the pull request was found. serve leaves such a branch to the person and says so.
- An adopted pull request: `Invited` for draft and ready, as today, and no title or description.
- A reviewer request on a pull request dockhand didn't open states `Named` where the person answered yes to it. Today it states `Invited` for an adopted one, which the gate refuses (`AskReviewers` takes `Made` and `Named`, internal/outward/outward.go:75).

#144's adopted comment says a basis should become something only evidence can build if a new caller ever states `Made` with no record behind it. Here none does, so the basis stays a word.

## Where the two proposals differed

| | Architecture | Codex | Taken, and what decided it |
| --- | --- | --- | --- |
| 1. Where the record lives | one value on the branch | a table of publications, with predecessors and versions | Architecture's. Nothing reads a predecessor: settling folds what it did into `PullRequest`, and the events tell the story. |
| 2. A step's states | owed or not; a read decides | seven states, with a mark saved before every call | Architecture's. Codex's own table settles five of the six steps by a read; the mark matters only for the reviewers, which row 3 makes readable. Two of Codex's points are kept: an outright refusal of a reviewer request ends the step, and nothing goes on past a record that failed. |
| 3. A reviewer request of unknown outcome | read the pending requests; ask if it isn't there | it can't be observed; never sent again | Architecture's read, and Codex's rule where the read fails. GitHub's documentation: "Once a requested reviewer submits a review, they are no longer considered a requested reviewer." Not sending again would leave a person's yes undone on a failure that a read can tell apart. |
| 4. Resuming | settle and plan afresh; only `ready` and a yes carry | no flags resumes the saved choices; other flags make a fresh acceptance | One path, with Codex's rule on choices. Architecture's dropped `--type`, `--draft` and `--skip-notification`, which only the first process held (for ship, internal/command/ship/ship.go:795). |
| 5. A no to the reviewers | not kept | kept for the head and the review | Codex's. A push that failed after a no would have asked again. |
| 6. serve's count | `serve.submit` events and reservations | a ledger with states, limits, days and zones | Codex's own record, in its smallest form: the pull request and when it opened. `ForgetBranch` deletes the day's events with a branch. The reservation stays architecture's, and the limit is read at each admission, as the settled text has serve's settings. |
| 7. Giving up a place | released when a read shows no pull request | one read isn't proof | Codex's caution, by an order: the publication that owes `open` goes first under its own place. |
| 8. Settling the description | the whole description compared | each owned part | Codex's. `Merge` judges part by part, so a person's edit of one part mustn't hide that the other two landed. |
| 9. Texts kept | title, body, written | the observed text, the base, the fresh text, the text sent and the written form, for each revision | `Title` and `Written`. Settling needs dockhand's parts; the plan reads GitHub again and writes its text again. |
| 10. A second read just before the edit | no | yes | Not taken. `unchangedSince` already reads again before the push, and its comment states the window that remains (submit.go:327-337). This item doesn't widen or narrow it. |
| 11. Locks | a `coord` lease, fenced writes | the branch's file lock and a lock on the remote target, then a lease and versions | Architecture's. `coord` knows a dead process by its PID, which is what a publication needs. The file lock never expires, so a hung submit would block the branch's local commands, and it is taken by history alone today (internal/history/history.go:49). A publication is about a commit, not the branch's head. |
| 12. The operation | `ApplySubmit` stays | a new `Engine.Publish` with four entries | Architecture's, with a second function that settles. Three callers already call the one. |
| 13. The order of ready and reviewers | ready first, unsaid | ready first, and said to be a change | Codex's correction. `finishSubmit` asks the reviewers first today (internal/command/submit/submit.go:255-261). |
| 14. A private ref pinning the commit | no | yes | Not taken. Nothing pushes a commit that isn't the branch's head (submit.go:1291), and settling compares ids. |
| 15. The account | not recorded | recorded; another login inherits nothing | Codex's: `By`. |
| 16. Evidence for the gate | `Made` from the record and the publication | grants built from stored receipts | A mark on the record, `Found`, and no grant type. See "The gate". |
| 17. Accepted failures and standing decisions | recorded with the publication | held pending, written when it is confirmed | Architecture's. The person accepted them for that commit, and the decision records its commit. |
| 18. Existing databases | add a column | import associations and charges, a marker and a pass for possible old openings, and an upgrade with nothing else running | Architecture's, with Codex's import of the count. The marker isn't taken: its cost is the second item under "Choices". |
| 19. Old reviewer left-overs | dropped | classified once as an old uncertainty | Architecture's. The inference reads events, and cleanup prunes every event older than `cleanup.after`. |
| 20. Commits | six | one cut-over | Architecture's. Each of the six leaves one path. |

Codex's corrections to the problem statement are taken whole: `undo` moves history and removes no branch; the draft state comes from `Inspect`, not `Observe`; a new pull request is opened as a draft only with `--draft`; and the cases it added are in the table below.

## The cases

"Settle" is rule 2. "Fold" is writing a step found done into `PullRequest` and taking it out of `Owes`.

| | The store then | The next command, and what it does | The person is told |
| --- | --- | --- | --- |
| A. The create's reply is lost | owes `push`, `open` | submit: settling finds the push and the pull request, folds, and counts it if serve's | the earlier submit opened #N |
| B. GitHub refuses the create for good | owes `push`, `open` | submit: folds the push, tries again. serve tries first on each pass, under its place | GitHub's refusal, each time |
| C. The edit failed after the push | owes `push`, `edit`, and `rerequest` if asked | submit: folds the push, edits, asks the reviewers as answered | what the earlier submit did, then this one |
| D. All done, the record failed | owes all it did | submit: settling finds all of it and folds | the earlier submit opened or updated #N |
| E. Killed between two steps | owes from that step on | as A, C, D, F or G by where it stopped. After the return to draft: settling finds the draft, and the plan pushes | what was found, what was done |
| F. `--ready` asked, lost before it | owes `ready` | (i) submit and (ii) ship mark it ready, saying it was asked earlier; `--draft` drops it. (iii) serve never acts on a person's | "marking #N ready, as the earlier submit asked" |
| G. The reviewer request | owes `rerequest` | (i) failed, (ii) record failed: read the pending requests; there, folded; not there, asked; unread, left. (iii) a no: not owed, not asked again. (iv) no answer: an attended run asks, an unattended one leaves it. (v) a yes: asked by the next run of that commit | who was asked, or that it couldn't be read and nothing was sent |
| H. An old-dated commit pushed after the review | `Review` in the publication | owed because the plan pushed to a pull request with changes requested. No commit date and no event is read | |
| I. #155 | serve's publication owes `open` | the pass settles first, finds the pull request, counts it; the limit of one is reached | today's limit is reached |
| J. A second ready branch sorts first | as I, unsettled | settling comes before candidates; an unsettled reservation counts | the second branch waits |
| K. A reservation at midnight | serve's publication | (i) opened before midnight: counted yesterday, today is free. (ii) not opened: it goes first today. (iii) opened after midnight: counted today. (iv) found with no creation time: the reservation stands | |
| L. #157 | `PullRequest.Body` old; `Written` is what GitHub took | settling folds the parts that landed; the plan then rewrites them for the new commit | nothing special |
| M. The person edits on GitHub in between | owes `edit` | their part matches no record and the merge keeps it | the description changed on GitHub and was kept |
| N. A new head over an unknown write | old publication | settle by reads; drop its `ready` and `rerequest` with a word; record the new one. A read that fails refuses the submit | what the earlier submit did, then this one |
| O. Someone moves the fork's branch | owes `push` | (i) the guarded push refuses, as today. (ii) settling finds neither commit; the publication ends, and the plan refuses as it does today for another's push | the branch changed on the fork |
| P. A late process | the other's record | it checks its lease before each remote write and each record, and stops | another dockhand took it over |
| Q. Someone else's pull request | owes `push` only | no title, description or opening exists to settle. D and N as above | as today |
| R. Merged or closed in between; `undo`; `clean` | owes anything | merged or closed: the publication is cleared with that state's record. `undo`: the next submit sees another commit. `clean` refuses an open branch | #N was merged or closed; nothing is left to do |
| S. Returned to draft, then the push failed | owes `push` and the write | submit pushes. It stays a draft; `submit --ready` is there | #N was returned to draft and the push failed |
| T. An existing database | no publication | (i) nothing. (ii) today's path. (iii) not carried | |
| U. GitHub can't be read | owed steps unread | (i) a person's submit is refused, nothing done. (ii) serve's reservation stands, and candidates are admitted only under the limit | which read failed |
| V. Marked ready by the GitHub CLI | owes `ready` | `Inspect` reads the draft state whichever app changed it | marked ready with the GitHub CLI |
| W. A person and serve at once | one holds the lease | the other is refused, nothing done | who is submitting it |
| X. The same commit, other flags | publication | a flag given replaces its own; the rest stands | what was kept from the earlier submit |
| Y. A flag set to false or empty | publication | `--draft=false` and an empty `--note` are given, and replace | |
| Z. A new review, or another login, in between | `Review.Reviewed`, `By` | a newer review drops the request, with a word. Another login: settled, nothing carried | |
| AA. A call lands after its process was killed | owed | the next read finds it done. A child `git push` still running when the next submit pushes is caught by the guard | |
| AB. A pull request found with no publication | none | a person's submit attaches it as `Found`; serve leaves it | #N was found open for this branch |

## Choices the reviewer should push on

1. **The reviewer request is sent again where GitHub shows no pending request** (row 3). If someone removed a request that had landed, the reviewer is notified twice. I take that over leaving a yes undone.
2. **No mark and no pass for an opening lost before the upgrade** (row 18). Such a pull request is found only when its branch is planned, it is then `Found`, and serve leaves it to the person. On the day of an upgrade serve could open one pull request more than its limit.
3. **No file lock** (row 11).
4. **`--rewrite-description` is not carried** to a continuation, where every other choice of the same commit is.
5. **A reviewer request on an adopted pull request states `Named`** after a yes, where today the gate refuses it.

## Tests

- **Settling**, one table: a publication and what each read answers, against what is folded, what stays owed and what is said.
- **The description**: a table in `prdescription` for the three parts, each landed, not landed, and edited by a person, with #157's failing case written first.
- **The count**: two engines on one database at a limit of one; midnight in each of K's four forms; a branch forgotten on the day its pull request opened; a database at schema 059 with `serve.submit` events, upgraded.
- **The cases** A to AB against the GitHub stand-in, which keeps a pull request's draft state and pending review requests. A reviewer asked while their request is pending fails the run.
- **The fault sweep** ([#129](https://github.com/herbygillot/dockhand/issues/129)): a failpoint after the first transaction and after each effect and each record. A continuation is the same command under the same attendance, so clean-run equality holds. Where a read is made to fail, the scenario asserts no remote write and the sentence that names the next command. `serve_openings` is compared exactly; no event of a submission is read as state any more.
- The regressions of #76, #118 and #127 stay.

## How it is built

1. The forge's status read gains pending review requests. The stand-in keeps draft state and requests for each pull request.
2. The question moves before the push. The engine marks ready and then asks the reviewers. The command says which flags were given. `RerequestDue` still serves left-overs. The usage pages change here.
3. The publication: the type, the column, the first transaction with the decisions and tested statements in it, settling at the plan, the description settled part by part, the same commit's choices, `status`, and `pending` in the JSON. `RerequestDue` and the left-over branch go. **#157 closes here**, its failing case written first.
4. serve: `serve_openings` with its one-time fill, the reservation in the first transaction, the pass settling first and continuing its own first. `servedToday`'s event count and cleanup's rule for it go. **#155 closes here.**
5. The lease, and the checks of a late process.
6. `Found` and the bases built from it; archiving and forgetting settle first; the merged or closed transition; `docs/architecture.md` and the acceptance rows.

After #129's foundation, #144, [#159](https://github.com/herbygillot/dockhand/issues/159), [#94](https://github.com/herbygillot/dockhand/issues/94) and [#178](https://github.com/herbygillot/dockhand/issues/178). [#183](https://github.com/herbygillot/dockhand/issues/183) lands before its last failure cases.

**For the change request ([#195](https://github.com/herbygillot/dockhand/issues/195)).** `Asked`, the destination, `ready` and the reviewer policy are the request's: #195 fills them and says which were given. The publication keeps what they resolved to for one commit, its steps and its settling. Whether the type, draft and `--skip-notification` are kept on the branch across commits, as the title and note are, is #195's flag check to decide.

## Not checked

- GitHub's behaviour is from its documentation, not from a run: that a reviewer asked again is among the pending requests until they review, and whether asking one already there notifies again, which the documentation doesn't say. An acceptance row on the test accounts settles both.
- Whether listing pull requests by head can lag behind a create whose reply was lost.
- How long a foreground session suits `ship` with nobody there.
- `Archive`'s code (internal/engine/verbs.go:353) beyond its signature.
- Every caller of `pullRequestBasis` and `editBasis`, which `Found` changes.

## Usage docs

- `docs/usage/publishing.md`: the question about reviewers comes with the plan, before the push; ready comes before the reviewers; a submit after a failed one says what the failed one did and finishes it, with the first one's flags standing for the same commit; `--rewrite-description` is asked again; a reviewer request that couldn't be read is not sent again.
- `docs/usage/recovery.md`: the messages for a lost create, edit or record, and `dockhand submit` as the one continuation.
- `docs/usage/unattended.md`: serve settles its own unfinished submissions before it opens anything, a place is held across midnight until it is settled, and serve never finishes a person's submission or takes up a pull request it has no record of opening.
- `docs/usage/branches.md`: `status` names a submission that isn't finished; archiving and forgetting settle it first.
- `docs/usage/agents.md`: `pending` in submit's and ship's JSON.
- `docs/architecture.md`: the publication, settling, and `serve_openings`.
- No flag or command is added or removed.
