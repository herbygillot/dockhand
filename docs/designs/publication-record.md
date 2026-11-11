# The publication record: what a submission set out to do, and what it still owes

A core item for v0.9.0, [#191](https://github.com/herbygillot/dockhand/issues/191). The issue says where it stands; this file is the design. Architecture merged it on 2026-11-11 from two proposals written apart from one problem statement, architecture's and Codex's (the person's "I'd like Codex to co-design and help review as well", 2026-11-11 05:48Z, in the architecture thread). Codex then reviewed the merge against the code and said to revise it before adoption. This is that revision, read again at main 3cb1dc8d7. "What the review changed" lists each of its points and what was taken. It is built once the person adopts it.

## In short

- Today a submission records nothing until its push and its pull-request write are both done. After a failure in between, the next run works out what happened from the fork, a search and the description's text. It gets three things wrong: serve's daily count ([#155](https://github.com/herbygillot/dockhand/issues/155)), whose text the description is ([#157](https://github.com/herbygillot/dockhand/issues/157)), and a reviewer request that failed.
- A branch gets one **publication**: the commit a submission set out to publish, where, what it meant to write, what the person asked of it, and the steps it still owes. It is recorded before the first remote effect and ended when nothing is owed.
- A step that is owed may have happened. Nothing is done again until the fork or GitHub has been read and shows it isn't done.
- Two calls can't be read back: a reviewer request, and a create whose answer was lost. Each is marked before it is sent. A reviewer request that may have been sent is never sent again. A create serve may have sent stays counted.
- There is no separate "resume". A later submit settles the old publication by reading, plans as always from what it found, and records a new one. For the same commit and destination, what the person asked the first time stands unless this run says otherwise.
- serve's count moves from events, which `clean --delete` removes with a branch, to a small table. A place is taken in the transaction that records the publication.
- One dockhand at a time submits to a branch on the fork. An operating-system lock says so, and it is never taken from a process that is still alive.
- What a person sees change: "ask them to review again?" is asked before the push, with the plan. A pull request is marked ready before its reviewers are asked, where today it is after. A submit that follows a failed one says what the failed one did. `status` names a submission that isn't finished. On the day of the upgrade serve opens nothing. No command or flag is added.

## Today

`ApplySubmit` (internal/engine/submit.go:1269), in order:

1. Refusals with nothing done: the gate, the description's length, the local head still being the planned commit, `unchangedSince` (:338) and `masterUnchanged`.
2. `draftFirst` (:1456), where `--draft` is asked of an open pull request. It is the first remote effect of such a submission.
3. The push, guarded by the remote head the plan saw. Failpoint `submit.pushed` (:1323).
4. Open, or edit the title and description. A create that failed is read back by head (`createdAnyway`, :1444). Failpoint `submit.created` (:1350).
5. One transaction: the pull request's record, the title and note, the tested statements, the accepted failures and standing decisions, and the events `branch.submit`, `branch.push`, and `serve.submit` where serve submitted.

Then, in the command, `finishSubmit` (internal/command/submit/submit.go:253) calls `Apply`, whose `followUp` asks the reviewers, and only then `Engine.Ready`. Each of those two has its own record. A reviewer request left over from an earlier submit is inferred from events (`RerequestDue`, submit.go:1588).

Three facts the rest leans on:

- The local head is checked once (step 1). The push then sends the literal commit, guarded by the fork's branch and not by the local one.
- No title or description is written where the head repository is someone else's (`plan.Theirs`, from `theirRepository`, submit.go:891). `PullRequest.Adopted` is another fact: it decides retitling, trust and `pullRequestBasis` (:398). The two are independent.
- A fork branch that moved is not always refused: `destination` accepts some advances and amendments (`builtOn`, `BranchWasAt`, `Amends`, submit.go:867-881).

serve counts `serve.submit` events since midnight (`servedToday`, internal/engine/serve.go:1363). `ForgetBranch` (internal/store/sqlite/prune.go:243) deletes every event of an ended branch, the day's among them. So a pull request that serve opened, and that was merged and cleaned the same day, leaves the count.

## The record

```go
// Publication is a submission of one commit that dockhand has begun and
// has not yet seen finished.
type Publication struct {
	ID       string     // random; every write of it checks it is still the branch's
	Commit   ObjectID   // the commit being published; every step is for this commit
	At       time.Time  // when it was recorded
	By       string     // the GitHub login it was recorded under: who, never a permission
	Served   bool       // serve recorded it
	Head     string     // owner/repo:branch it pushes to
	Base     string     // owner/repo:branch the pull request is asked of
	Number   int        // the pull request it updates; 0 where it sets out to open one
	Expected ObjectID   // the head branch as the plan saw it; empty where it didn't exist
	Title    string     // the title it set out to write; empty where it writes none
	Body     string     // the description it sends; empty where it writes none
	Written  string     // dockhand's parts of Body, as prdescription.Written gives them
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

// ReviewAsk is "ask them to review again?", its answer, and what became
// of each request.
type ReviewAsk struct {
	Answer    Answer // yes, no, or not asked yet
	Reviewers []Reviewer
}

// Reviewer is one login to ask, and how far asking got.
type Reviewer struct {
	Login    string
	Reviewed time.Time // GitHub's time of this login's own latest review when the question was put
	Sent     Sent      // not sent; may have been sent; sent; refused
	Why      string    // GitHub's words, where it refused
}
```

- It is a nullable value on the branch beside `PullRequest`, stored as one JSON column on `branches`, as `pr_observed` is.
- One at a time. A new one is recorded only after the old one has ended ("Ending a publication").
- **It names its whole destination.** `Head`, `Base` and `Number` are what every read and write of it uses. Nothing is resolved again from settings or from `Branch.PullRequest`, which can be absent in the very window being repaired.
- `PullRequest` keeps its meaning, sharpened: what dockhand knows GitHub has from it (`Number`, `Head`, `Pushed`, `Body`, `Draft`). The publication is what it set out to do. A step leaves `Owes` in the same transaction that writes its result into `PullRequest`.
- `PullRequest` gains two fields:
  - `Provenance`: `opened` where GitHub answered dockhand's own create with it; `found` where a read found it for the head; empty on a record from before this design, which is read as it is today.
  - `Rerequested`: the last reviewer request dockhand set out to make, as a receipt: the head it was for, and each login with what became of it. It outlives the publication and is replaced by the next one.
- Not in it, because a record already holds each and the first transaction writes them there: the title, the note and the tested statements (the branch's own fields), and the person's decisions. An accepted failure is recorded for the commit. A standing decision keeps the evidence it has today, `EnvironmentEvidence(dependency, identity)`, and is no branch's (`branchstate.EnvironmentFailures`, publication.go:169). Both were given at the plan and stand even where the publication then fails; `AddDecision`'s key keeps a second run from recording one twice.
- Not in it: the basis each write states to the gate. It is built from evidence at each step.

## Four rules

1. **Observe, then act.** A step still owed may have happened. Nothing is done again until a read shows it isn't done. A read counts only where it is complete and fresh: a list that was cut short, a field that is missing, or a reading taken from the cache settles nothing.
2. **Mark what can't be read back.** A reviewer request and serve's create are marked "may have been sent" before the call, in a transaction dockhand has seen recorded (`store.Recorded`, with its witness). The mark is cleared only by GitHub's own answer.
3. **Settling only reads.** Accounting for an unfinished publication reads the fork and GitHub and writes dockhand's own record. It never writes to GitHub.
4. **One path.** A submission of a branch that has a publication settles it, then plans, then records a new publication. Push, open and edit are worked out by the plan from what settling found. Nothing runs from the old record without a plan.

No remote effect follows a record dockhand hasn't seen recorded. That holds for the first transaction and for each mark.

## The steps

| Step | Owed when | The read that settles it |
| --- | --- | --- |
| `draft` | `--draft` is asked of an open pull request | `Inspect`: it is a draft |
| `push` | the fork's branch isn't at `Commit` | the head branch itself: at `Commit`, done; at `Expected`, not done; anything else, someone else pushed, and the publication ends there |
| `open` | no pull request is open for the head | `Find` by `Head` and `Base`, which looks at every state and prefers an open one: one found is attached; none found, not done |
| `edit` | the title or the description is to change | `Observe`: see "The description" |
| `ready` | `--ready` was asked | `Inspect`: it isn't a draft |
| `rerequest` | the plan pushes to a pull request whose reviewers requested changes, and the answer is yes or not given yet | each login's own state: see "The reviewer request" |

A step leaves `Owes` only in a transaction that also writes its result:

- `draft`, `push`, and `open` or `edit` leave together, in today's one transaction after the pull request is written.
- `ready` and `rerequest` each leave in the transaction that follows them, as `Ready` and `RequestReview` write today.
- Where that transaction fails after the effect, the step stays owed and the next read finds it done. Nothing goes on to the next effect past a record that failed.

So a plain submission costs one store write more than today: the first one.

**The order changes.** Ready comes before the reviewers are asked. Today it is the other way round.

**What a found pull request is.** GitHub's reply to dockhand's own create, held by the process that sent it, is the only evidence that dockhand opened a pull request: `Provenance` is `opened`. Any pull request a read finds is `found`, whether or not a create was tried and whoever its author is. A person can have opened it in between, and an intent to open one is not a record of having done so.

**A create GitHub refuses.** GitHub answering the create with a refusal means nothing was opened. `open` ends as refused: the push and anything else done are folded, the refusal is returned and said, and the publication ends with nothing owed. The next submit is a new attempt. One answer is not a refusal: "a pull request already exists for this head" says that one exists, so `open` stays owed until the read finds it. The forge names that answer apart from the others.

**One read before the pull request is written.** `unchangedSince` reads before the push, and a push can be long. Just before the edit dockhand reads the description once more. Where it is no longer what the plan merged against, the edit is not made: the step stays owed, the command says the description changed on GitHub during the submission, and `dockhand submit` plans against the new text. The window that remains is one request wide.

**Follow-ups are for the commit.** `ready` and `rerequest` are made only while the head branch is at `Commit`. In a straight run the guarded push is that reading. In a continuation settling reads the branch itself, not the pull request's copy of it. Where the branch is at another commit, the publication ends and those steps are dropped and said.

## The reviewer request

What the step promises: after it, each reviewer named has a review request pending on the pull request, or dockhand says who doesn't and why. It does not promise that a particular notification was delivered. A request already pending, whoever made it and whenever, satisfies it.

GitHub lists a pull request's pending review requests, and a reviewer leaves that list when they review or when someone removes the request. dockhand reads none of it today: `forge.PullRequestStatus` (internal/forge/pullrequest.go:63) has the reviews and no requests, and its `ReviewedAt` is the time of the review that sets the overall verdict, not of each person's. The status read gains the complete list of pending requests, with a sign of whether it is complete, and each login's own latest review time.

For an owed `rerequest` with a yes, each login apart, one call a login:

| What is read, and the mark | What happens |
| --- | --- |
| The read failed or was incomplete | nothing is sent; the step stays owed and is said |
| Their own latest review is newer than `Reviewed` | dropped for that login and said: the answer was for another review |
| A request is pending | satisfied; nothing is sent |
| None pending, not sent | the mark "may have been sent" is recorded, then the request is sent |
| None pending, may have been sent | **not sent again.** Said: a request may have been sent, GitHub shows none pending, and dockhand doesn't send one twice |

And what the call's own outcome does to the mark:

| GitHub's answer | The mark |
| --- | --- |
| Acknowledged | sent |
| A rate limit, or the login is wanting | back to not sent; the step stays owed for the next run |
| Any other refusal GitHub answers with | refused, with GitHub's words. Not asked again by this publication, and shown in the result and the receipt |
| No answer, or a server error | stays "may have been sent" |

When no login is left to send to, the step leaves `Owes` and the receipt is written to `PullRequest.Rerequested`. `status` shows a reviewer who may have been asked and one GitHub refused, beside who requested changes. A person who wants to be sure asks on GitHub.

An absent request is not read as a request that failed. Two histories look the same to a read: the process died before the call; or the call landed, its answer was lost, and someone removed the request. Only the mark tells them apart.

## When settling runs

When a submission of the branch is planned (`PlanSubmit`, submit.go:445), so the preview is of the settled record. At the start of serve's pass, for every publication serve recorded. Before a branch is archived or forgotten. And where `status` reads a pull request anyway, on `--refresh` or when its reading is more than five minutes old, that reading settles what it can. `status` makes no read of its own for a publication.

## Who continues what

- A person's `submit` or `ship` of a branch with a publication settles it and shows its plan as always. The plan says what the earlier submit did and what it carries. No flag.
- **The same commit and destination.** Each thing the person asked the first time stands where this run doesn't say otherwise: `--draft`, `--no-check`, the types, `--skip-notification`, a `ready` still owed, and the answer about the reviewers. A flag given on this run replaces its own: `--draft` drops a carried `ready`, and `--ready` a carried draft. For this the command tells the engine which flags were given, since a flag left out and a flag set to false are different.
- **Another destination.** A run whose head, base or pull request resolves otherwise than the publication recorded doesn't continue it. The old one is settled and ended, nothing it asked carries, and the plan says so.
- `--rewrite-description` is the exception. It was asked of a description the person was looking at. Where the rewrite landed, settling finds it. Where it didn't, a continuation doesn't act on it: it says the earlier submit was asked to rewrite the description and didn't get to it, and that `--rewrite-description` asks again.
- **A recorded answer** is good for the commit, the pull request, and each reviewer's review it was given for, whoever runs next. A yes is acted on with nobody there. A no is not asked again. With no answer, an attended run asks before the push and an unattended one leaves the step owed and says so. `submit.rerequest_review = "always"` is the person's standing word, as today. Nobody's yes is invented.
- **Another commit.** `ready` and `rerequest` were for the old head and are dropped, with a word. What the old request's marks say goes into the receipt first. A push of the new head to a pull request with changes requested makes a new question.
- **serve** settles and continues only publications it recorded, on branches it prepared. It never acts on a person's, and it never writes to a pull request that was found.
- **A person's submit of a branch serve was publishing** takes it over. See "serve's count" for what happens to serve's place.
- **Another account.** A publication recorded under another login than the one dockhand acts for now is settled by reading and ended. Nothing it asked carries.

## Ending a publication

A publication leaves the branch only in a transaction that first writes down what it knows:

1. each of `draft`, `push`, `open`, `edit` and `ready` read and folded into `PullRequest`, done or not;
2. the parts of the description that landed, into `PullRequest.Body`;
3. serve's row in `serve_openings`, with the pull request and GitHub's creation time where one was found;
4. every reviewer mark other than "not sent", into `PullRequest.Rerequested`.

That is one rule for every way a publication ends: it finished; a new submit replaces it; the fork's branch is at a third commit; the pull request was merged or closed; a person takes serve's over; the branch is archived or forgotten.

- Where a read it needs fails, the publication stays. A submit, an archive or a forget that needed it gone is refused with nothing done, and says which read. A refresh that sees the pull request merged or closed records that and leaves the publication for a later settle.
- Folding comes before ending. A branch found at a third commit ends the publication's remaining steps, which were for `Commit`, but not before its landed text and its opening are written down. The fresh plan then judges the moved branch by today's rules in `destination`.
- A pull request found merged or closed was still opened. Its row is written before the publication goes.
- The old record is not kept. What must outlive it is in the four places above.

## serve's count

One table, `serve_openings`, replaces the count of `serve.submit` events. A row is one pull request serve opened, may have opened, or holds a place to open: the base repository, the head, and three things that may be empty: when a create was sent (`tried`), the pull request's number, and GitHub's time for its creation (`opened`). It is not deleted with a branch.

| The row | Counts on | Leaves when |
| --- | --- | --- |
| A place held: no `tried`, no `opened` | every day, while its publication owes `open` | a person takes the branch over, the branch ends, or serve can no longer submit it |
| A create that may have landed: `tried`, no `opened` | the day it was tried and the day after | GitHub answers that create with a refusal. Otherwise never |
| An opening: `opened` | the day GitHub says it was created | never; old rows are pruned |

- **The limit check and the place are one transaction**, the first one: the day's count must be under the limit, or nothing is written and nothing is pushed. SQLite has one writer, so a second pass or a second process can't interleave.
- **`tried` is written before the create is sent**, and seen recorded. From then on the place can't be given up by anything dockhand reads, because a read that finds no pull request doesn't show that the create didn't land.
- **A pull request found** for a row that was tried is charged to it: the row takes its number and GitHub's creation time. One found with no creation time leaves the row as it is. This may charge serve for a pull request a person opened in between, which errs toward opening fewer.
- **A person's takeover.** Of a place never tried: the place is released, and the person's opening isn't counted. Of a create that was tried: the row stays, whatever the person's submit then does.
- **The pass settles first.** Before any candidate, serve settles every publication it recorded, whatever the branches' order. One that still owes `open` goes first, under the place it holds. A second try is a new `tried`, and where its row no longer counts today it passes the limit check again.
- **The day** is worked out at each count with `dayStart`, as today. The limit is read from the settings at each admission.
- **Two days for a create with no answer.** A create is one request: GitHub has either made the pull request by the time the connection is gone, or it never will. The day after is there for a create sent just before midnight. This is the one place the design takes time as evidence, and it only ever stops counting something, never sends something.
- `serve.submit` stays as a line in the journal. Nothing reads it as state, and cleanup's rule that keeps the day's events for the count goes.
- Nothing but serve opening a pull request counts, as today.

## The description

The first transaction comes after `unchangedSince` and `masterUnchanged`, the last reads that can change what is written, and before `draftFirst`. `Title`, `Body` and `Written` in it are final.

`prdescription.Merge` (internal/macports/prdescription/prdescription.go:916) rewrites each of dockhand's three parts, the Description, the Type(s) and everything from Tested on down, only while GitHub's part is still what dockhand last wrote there. #157 is a part GitHub took whose record was lost: it matches neither the last record nor, after a new commit, the fresh text, so it reads as a person's.

Settling makes "last written" true:

- Where GitHub's whole description is `Body`, the write landed whole, and `PullRequest.Body` takes `Written`, as a submit that finished records it today. That covers a rewrite, serve's added note, and a description with a part left out.
- Otherwise part by part: where GitHub's part reads as `Written`'s part, that part landed and `PullRequest.Body` takes it. A part that reads otherwise is left as recorded.
- The merge then runs as it does today. A part a person edited matches no record and is kept, whether they edited the old text or the new.
- The title needs no record: where GitHub's is the one the publication set out to write, it landed, and otherwise today's rule for an edited title decides.

Where the head repository is someone else's, dockhand writes no title or description, as today, and `Title`, `Body` and `Written` are empty.

## One submission at a time

An operating-system lock, from `internal/filelock`, keyed by the publication's `Head`: the repository and branch on the fork, so two local branches aimed at one remote branch share it. It lives beside the branch locks under the repository's common directory.

- `ApplySubmit` takes it before the first transaction and holds it to the end, follow-ups included. The file is handed to `git push` as the branch lock's is (`withLock`, internal/git/branch.go:41), so a push still running after dockhand died keeps it.
- A second caller is refused with nothing done. The holder writes its process and its start time into the file, and the refusal names them.
- **It is never taken from a live process.** A dockhand that hangs keeps the lock until it is stopped, and the refusal says which process to stop. The system releases it when the process and any `git` it started have exited.
- Settling's writes take it too. Where a live process is submitting the branch, a plan is refused and `status` shows what is stored.
- Between a plan and its `ApplySubmit`, another process may have settled or replaced the publication. Under the lock, `ApplySubmit` checks that the branch's publication is the one the plan saw, and otherwise refuses as it does for a head that moved.
- Every write of the publication checks `Publication.ID` in its own transaction.
- It is not the branch's history lock. The publication publishes the literal commit it recorded, whatever the local branch does afterwards; that is intended, and `status` then shows the branch ahead of what was published.

The first draft used a lease from `internal/coord`. A lease is taken from a holder that is alive but silent for two minutes (`Judge`, internal/coord/coord.go:282), and `Fenced` guards a store write, not a call made after a lease was read. A process paused just before its edit would wake and write its old text over a successor's.

## The operation, and what callers see

`ApplySubmit` stays the one operation and keeps its name. It records the publication, makes each owed step, and returns what `Submitted` has today plus the ready outcome, what became of each reviewer, and what is still owed. A result comes back with an error too, as today. A second function settles and only reads.

- **Lines.** What was done, as today, then a line for each owed step and the command that finishes it.
- **`--json`** gains `pending`, a list of step and reason. The reasons are told apart: it failed, with the error; it awaits the person's answer; it may have been sent; GitHub refused it; it couldn't be read.
- **Failures.** Today's sentences, now true by the record: "pushed … but could not open the pull request: …; dockhand submit finishes it."
- **What fails the command.** A step that failed, a `ready` that was asked and not done, and a refusal. A reviewer question still unanswered doesn't: the publication is reported as done with the reviewers not asked.
- **`status`.** A branch with a publication reads as a submission that isn't finished, with what is owed, and its next step is `dockhand submit`.
- **Into the engine:** who is owed a re-request, which the plan gains; the request and marking ready, as steps. `followUp`'s left-over branch, `RerequestDue` and `finishSubmit`'s call to `Ready` go.
- **Stays in the command:** asking, before the push; the wording; the JSON. `ship` asks with its own confirmation at its start, and the answer rides with its submission; a ship that ends before its check does loses it with the rest of what it holds, which is #195's to keep.

No message names `Owes` or a step's internal name.

## Existing databases and branches

- The schema adds the column, the two fields on the pull request's record, and the table. No branch row changes. A branch recorded before has no publication and owes nothing, and its pull request's `Provenance` is empty, which is read as today: no right is gained or lost by the upgrade.
- **On the day of the upgrade serve opens nothing.** A pull request serve opened before the upgrade whose record was lost has no event and no row, and nothing in the old database can count it. The migration writes its own time into the table, and for the rest of that day the count answers that the limit is reached, and says why. From the next midnight every opening is in the table. Nothing is imported from `serve.submit` events, whose times are when dockhand recorded and not when GitHub created.
- A branch whose last submit failed part-way before the upgrade takes the path it takes today: `PlanSubmit` finds its pull request by head (`destination`, submit.go:776). It is attached as `found`.
- **Old reviewer left-overs are said once and never sent.** The migration applies `RerequestDue`'s test one last time, in one statement: where a branch's pull request has changes requested, a push recorded after that review, and no request recorded since, it writes a receipt saying the reviewers may not have been asked. `status` shows it. Then `RerequestDue` goes. A left-over whose events were already pruned can't be found, and isn't guessed.
- The migration runs under the store's existing guard, which refuses while another dockhand's session is live (`refuseUnderLive`, internal/store/sqlite/sqlite.go:565).
- `clean` refuses an open branch, as today. Archiving and forgetting end the publication first, by the rule above, and refuse if they can't.
- `undo` moves a branch's history and removes nothing remote (`Restore`, internal/engine/restore.go:23). Where it moves the head off `Commit`, the next submit treats the publication as one for another commit.
- The publication is on the branch's row and is never pruned. Rows of `serve_openings` older than two days may be.

## The gate

Each step states a basis to the gate (`internal/remotewrite`), built from evidence when the step is made. `By` says who recorded a publication. It is never a basis.

- Push: `OwnFork`, or `Invited` where the head repository is someone else's, as today. Open: `OwnFork`.
- Edit, draft and ready state `Made` where the record's `Provenance` is `opened`, or is empty on a record from before that isn't adopted, which is what `pullRequestBasis` answers today. `Made` is never stated from a publication's intent or from the author's login.
- A `found` pull request: a person's submit of that branch names the action, so its writes state `Named`, and the plan says the pull request was found. serve leaves it.
- Someone else's, or adopted: `Invited`, as today.
- A reviewer request states `Made` on a pull request dockhand opened, and otherwise `Named` where the person answered yes. The yes is recorded with what it covers: the pull request, the head, the logins with their reviews, and who answered. Today an adopted pull request's request states `Invited`, which the gate refuses (`AskReviewers` takes `Made` and `Named`, internal/remotewrite/remotewrite.go:91).
- `clean`'s test for deleting a head branch (`ownedHead`, internal/engine/clean.go:379) reads neither `Provenance` nor the publication, and gains nothing from them.

#144's adopted comment says a basis should become something only evidence can build if a new caller ever states `Made` with no record behind it. Here `Made` comes only from GitHub's answer to a create, or from a record that already had it.

## What the review changed

Codex's review made seven objections, each with a counterexample, and six corrections of fact. All are taken.

| Its point | What was wrong in the merge | Taken |
| --- | --- | --- |
| R1. An absent pending request is not a failed notification | It sent again wherever GitHub showed no request pending, and took one review time for every reviewer | A mark for each login, recorded before the call. "May have been sent" is never sent again. Each login's own review time. A receipt on the pull request's record |
| R2. Ordering doesn't make a released place safe | A person's takeover, an ended branch or a later read released serve's place although its create could still land | `tried`, recorded before the create. A tried row leaves only on GitHub's refusal of that create |
| R3. A terminal state doesn't erase accounting | "Merged or closed: the publication is cleared", and a third commit ended it at once | "Ending a publication": folding first, in the same transaction, for every way it ends |
| R4. A stale edit doesn't write the same text | A lease, which is taken from a live process that is silent | The operating-system lock, never taken from a live process. The lease goes |
| R5. An intent and the author's login are not evidence of creation | "An open one by `By` is the one this publication opened", and `Found` only where there was no publication | `Provenance`: `opened` only from GitHub's answer to the create. Everything read is `found`. `ownedHead` unchanged |
| R6. The upgrade kept #155 | No account of an opening lost before the upgrade, and an import from events | serve opens nothing on the day of the upgrade; nothing is imported |
| R7. A permanent refusal needs an end | A refused create was tried first on every pass under its place; a refused reviewer just left the step | A refused create ends `open` and releases the place. A refused reviewer is recorded as refused, shown, and not asked again |
| `coord` knows a dead process by its PID | It also judges a live, silent one dead | See R4 |
| A changed `ReviewedAt` shows someone reviewed since | It is the verdict's review, not each person's | Each login's own review time |
| An adopted pull request has no text written | The switch is `plan.Theirs`, not `Adopted` | Stated under "Today", in "The description", the gate and case Q |
| A third commit on the fork is refused | `destination` accepts some advances and amendments | Case O: fold, end, and the plan judges by today's rules |
| Decisions have the commit as their evidence | Standing decisions use `EnvironmentEvidence` and are no branch's | Stated under "The record" |
| Nothing pushes a commit that isn't the branch's head | The local head is checked once; the push sends the literal commit | Stated as intended, under "Today" and the lock |
| The whole destination | No base, and no pull request number | `Base` and `Number`; another destination doesn't continue |
| Complete readings | Only a failed read was named | Rule 1 |
| Follow-ups after the head moved | Only the push was guarded | "Follow-ups are for the commit" |
| A record whose outcome isn't known | Not named | Rule 2, with `store.Recorded` |
| Texts kept | `Written` alone can't tell a rewrite or serve's note | `Body` beside `Written` |
| A read just before the edit | Declined | Taken, as a refusal to edit and no re-merge |
| What callers are told | One list of reasons | Five reasons told apart, and what fails the command |
| Six commits, each activating a part | The record was live before its lock, its cleanup and its gate | Pieces first, with tests and no caller. Then one activation |

Two things were taken in a smaller form than the review's, and it should say if either is too small:

- For R6 it offered a pass over old branches, or holding serve until old state is resolved. The day's pause is the second, resolved by midnight, with no read and no import.
- A refused reviewer isn't sorted into "can never be asked" and "can't be asked now", beyond the rate limit and the login. Every other refusal ends that login for this publication and is shown.

## Where the two proposals differed

| | Architecture | Codex | Taken, and what decided it |
| --- | --- | --- | --- |
| 1. Where the record lives | one value on the branch | a table of publications, with predecessors and versions | The branch's value. What must outlive it is written down as it ends |
| 2. A step's states | owed or not; a read decides | seven states, with a mark saved before every call | Owed or not for the four steps a read decides. A mark for the two it can't: each reviewer, and serve's create |
| 3. A reviewer request of unknown outcome | read the pending requests; ask if it isn't there | it can't be observed; never sent again | Codex's. The read satisfies a request found pending; it never licenses a second send |
| 4. Resuming | settle and plan afresh; only `ready` and a yes carry | no flags resumes the saved choices; other flags make a fresh acceptance | One path, with Codex's rule on choices, for the same commit and destination |
| 5. A no to the reviewers | not kept | kept for the head and the review | Codex's |
| 6. serve's count | `serve.submit` events and reservations | a ledger with states, limits, days and zones | A small table of its own; the limit and the day read at each count |
| 7. Giving up a place | released when a read shows no pull request | one read isn't proof | Codex's. A place whose create was tried is never released by a read |
| 8. Settling the description | the whole description compared | each owned part | Both: the whole first, then each part |
| 9. Texts kept | title, body, written | every text of every revision | `Title`, `Body` and `Written` of the one publication |
| 10. A second read just before the edit | no | yes | Yes |
| 11. Locks | a `coord` lease, fenced writes | the branch's file lock and a lock on the remote target, then a lease and versions | A lock on the remote target alone. No lease, and not the history lock |
| 12. The operation | `ApplySubmit` stays | a new `Engine.Publish` with four entries | `ApplySubmit`, with a second function that settles |
| 13. The order of ready and reviewers | ready first, unsaid | ready first, and said to be a change | Said |
| 14. A private ref pinning the commit | no | yes | No. Settling compares ids and never pushes or rebuilds the old commit |
| 15. The account | not recorded | recorded; another login inherits nothing | `By` |
| 16. Evidence for the gate | `Made` from the record and the publication | grants built from stored receipts | `Provenance`, set only from GitHub's answer. No grant type |
| 17. Accepted failures and standing decisions | recorded with the publication | held pending, written when it is confirmed | Recorded with the first transaction, each with its own evidence |
| 18. Existing databases | add a column | import associations and charges, a marker and a pass for possible old openings | No import. serve waits out the day of the upgrade |
| 19. Old reviewer left-overs | dropped | classified once as an old uncertainty | Said once by the migration, never sent |
| 20. Commits | six | one cut-over | Pieces, then one activation |

## The cases

"Settle" is rule 3. "Fold" is writing a step found done into `PullRequest` and taking it out of `Owes`.

| | The store then | The next command, and what it does | The person is told |
| --- | --- | --- | --- |
| A. The create's reply is lost | owes `push`, `open`; serve's row is tried | submit: settling finds the push and the pull request, folds, attaches it as found, and charges serve's row | #N was found open for this branch; the earlier submit may have opened it |
| B. GitHub refuses the create | `open` ends as refused; serve's place is released | the next submit, or serve's next pass, is a new attempt | GitHub's refusal |
| C. The edit failed after the push | owes `push`, `edit`, and `rerequest` if asked | submit: folds the push, edits, asks the reviewers as answered | what the earlier submit did, then this one |
| D. All done, the record failed | owes all it did | submit: settling finds all of it and folds | the earlier submit opened or updated #N |
| E. Killed between two steps | owes from that step on | as A, C, D, F or G by where it stopped. After the return to draft: settling finds the draft, and the plan pushes | what was found, what was done |
| F. `--ready` asked, lost before it | owes `ready` | (i) submit and (ii) ship mark it ready, saying it was asked earlier; `--draft` drops it. (iii) serve never acts on a person's | "marking #N ready, as the earlier submit asked" |
| G. The reviewer request | owes `rerequest`, a mark for each login | (i) died before the call: not sent, so sent now. (ii) the answer was lost: may have been sent; pending, satisfied; not pending, not sent again. (iii) a no: not owed, not asked again. (iv) no answer: an attended run asks, an unattended one leaves it. (v) a yes: asked by the next run of that commit | who was asked, who may have been, who GitHub refused |
| H. An old-dated commit pushed after the review | `Review` in the publication | owed because the plan pushed to a pull request with changes requested. No commit date and no event is read | |
| I. #155 | serve's row is tried | the pass settles first, finds the pull request, charges the row; the limit of one is reached | today's limit is reached |
| J. A second ready branch sorts first | as I, unsettled | settling comes before candidates; a tried row counts whether or not it is settled | the second branch waits |
| K. A place at midnight | serve's row | (i) opened before midnight: counted yesterday, today is free. (ii) held, not tried: it goes first today. (iii) opened after midnight: counted today. (iv) tried, nothing found: counted today, free tomorrow | |
| L. #157 | `PullRequest.Body` old; `Body` is what GitHub took | settling folds the text that landed; the plan then rewrites it for the new commit | nothing special |
| M. The person edits on GitHub in between | owes `edit` | their part matches no record and the merge keeps it. During the submission: the read before the edit sees it and the edit isn't made | the description changed on GitHub and was kept |
| N. A new head over an unknown write | old publication | settle by reads; drop its `ready` and `rerequest` with a word, its marks into the receipt; record the new one. A read that fails refuses the submit | what the earlier submit did, then this one |
| O. Someone moves the fork's branch | owes `push` | (i) the guarded push refuses, as today. (ii) settling finds a third commit: fold what landed, end the publication, and the plan judges the branch by `destination`'s rules | the branch changed on the fork |
| P. A second dockhand while one hangs | the first holds the lock | refused, nothing done | which process is submitting it, and to stop it if it hangs |
| Q. Someone else's head repository | owes `push` only | no title, description or opening exists to settle. D and N as above. An adopted pull request on the person's own fork is not this case | as today |
| R. Merged or closed in between; `undo`; `clean` | owes anything | merged or closed: folded, serve's row written, then ended; unread, it stays. `undo`: the next submit sees another commit. `clean` refuses an open branch | #N was merged or closed; nothing is left to do |
| S. Returned to draft, then the push failed | owes `push` and the write | submit pushes. It stays a draft; `submit --ready` is there | #N was returned to draft and the push failed |
| T. An existing database | no publication | (i) nothing owed. (ii) a half-done old submit: today's path, attached as found. (iii) an old reviewer left-over: said once, not sent. (iv) serve: nothing opened that day | |
| U. GitHub can't be read | owed steps unread | (i) a person's submit is refused, nothing done. (ii) serve's rows stand, and candidates are admitted only under the limit | which read failed |
| V. Marked ready by the GitHub CLI | owes `ready` | `Inspect` reads the draft state whichever app changed it | marked ready with the GitHub CLI |
| W. A person and serve at once | one holds the lock | the other is refused, nothing done | who is submitting it |
| X. The same commit, other flags | publication | a flag given replaces its own; the rest stands | what was kept from the earlier submit |
| Y. A flag set to false or empty | publication | `--draft=false` and an empty `--note` are given, and replace | |
| Z. A new review, or another login, in between | each `Reviewer.Reviewed`, `By` | a reviewer who reviewed since is dropped, the others stand. Another login: settled and ended, nothing carried | |
| AA. A call lands after its process was killed | owed | the next read finds it done. A `git push` still running holds the lock, so the next submit is refused until it exits | |
| AB. A pull request found with no publication | none | a person's submit attaches it as found; serve leaves it | #N was found open for this branch |
| AC. A request landed, its answer was lost, and someone removed it (R1) | may have been sent | not sent again | a request to them may have been sent |
| AD. Alice approves while Bob's review still sets the verdict (R1) | both marked | Alice's own review is newer: dropped. Bob stands | |
| AE. A person takes over serve's tried create, then stops (R2) | serve's row is tried | the row stays and counts; serve admits nothing over the limit. If the create lands late, it is the row's | |
| AF. A tried create found already merged (R3) | owes `open` | the row takes its number and time, the text that landed is folded, then the publication ends | |
| AG. Paused just before its edit (R4) | holds the lock | nobody else submits to that branch until it is stopped or wakes; awake, it reads once more before the edit | |
| AH. A person opened the pull request between the plan and the create (R5) | owes `open` | GitHub answers that one exists; the read finds it; found, not opened | #N was found open for this branch |
| AI. An opening lost before the upgrade (R6) | no row | serve opens nothing that day | serve counts afresh from tomorrow |

## Choices the person should see

1. **A reviewer request that may have been sent is never sent again.** After a lost answer dockhand says who may have been asked and stops. The other way sends twice when someone removed a request that had landed.
2. **A dockhand that hangs while submitting blocks that branch until it is stopped.** A second one is refused and names the process. The other way lets the second take over, and the first can then write over it when it wakes.
3. **A pull request found after a lost answer is not treated as dockhand's own.** A person's submit goes on with it, saying it was found. serve charges it to the day and writes nothing more to it.
4. **On the day of the upgrade serve opens nothing.** The other way is a pass that reads every branch serve prepared before it opens anything.
5. **`--rewrite-description` is not carried** to a continuation, where every other choice of the same commit is.
6. **A reviewer request on an adopted pull request is made** after a yes, where today the gate refuses it.

## What this does not defend

- A request that had left the process when it died. The push is guarded by `Expected`. GitHub refuses a second open pull request for one head, and serve's row stays charged. An edit could land late; the window is one request.
- Another machine, or another database, submitting to the same branch.
- A person acting on GitHub between dockhand's read and its call. GitHub offers no write that is conditional on what was read.
- A dockhand from before the upgrade that was paused across it and wakes. Its remote calls go out, and its record lands as a record from before. The day's pause covers serve's count for that day.

## Tests

- **Settling**, one table: a publication and what each read answers, complete and cut short, against what is folded, what stays owed and what is said.
- **The reviewer request**, one table over the two tables above: each reading and mark, and each answer. It includes AC and AD, and a request to several logins where one is acknowledged, one refused and one unanswered.
- **The description**: a table in `prdescription` for the three parts, each landed, not landed, and edited by a person; the whole text landed, for a rewrite, serve's note and a part left out; with #157's failing case written first.
- **The count**: two engines on one database at a limit of one; midnight in each of K's four forms; AE and AF; a branch forgotten on the day its pull request opened; a database at schema 059 with `serve.submit` events, upgraded, on that day and the next.
- **The lock**: a second process refused while the first sleeps before its edit; a `git push` child that outlives its parent; the refusal's words.
- **The upgrade**: a submit under schema 059 killed after its create, then upgraded and submitted again; an old reviewer left-over, said once.
- **The cases** A to AI against the GitHub stand-in. It keeps a pull request's draft state and pending review requests, lets a request be removed, keeps each person's reviews apart, answers a request for some logins and not others, and can delay a call past its caller's death. dockhand's own rule is asserted by count: no login is sent two requests by one publication.
- **The fault sweep** ([#129](https://github.com/herbygillot/dockhand/issues/129)): a failpoint after the first transaction, after each mark, and after each effect and each record. Where every outcome can be read back, a continuation is the same command under the same attendance and clean-run equality holds. Where one can't, the scenario declares it: the expected end state has the mark, the row and the texts exactly, and no second send. Where a read is made to fail, it asserts no remote write and the sentence that names the next command. `serve_openings` is compared exactly; no event of a submission is read as state.
- The regressions of #76, #118 and #127 stay.

## How it is built

1. The forge's status read gains the pending review requests, complete or said not to be, and each login's own latest review. The create's "already exists" gets its own name. The stand-in gains what "Tests" lists.
2. The question moves before the push. The engine marks ready and then asks the reviewers. The command says which flags were given. `RerequestDue` still serves left-overs. The usage pages change here.
3. The pieces, each with its tests and no caller: the publication's type and settling as a function from readings to folds; the reviewer table; the description settled whole and by part; `serve_openings` and its count; the lock; `pending`'s reasons.
4. **One activation.** The schema with its two statements for old databases; the first transaction; the marks; the steps and the reads before them; settling at the plan, serve's pass, `status`, a refresh, archive and forget; serve's place; the lock; the bases from `Provenance`. `RerequestDue`, `servedToday`'s event count, cleanup's rule for it, and `followUp`'s left-over branch go in the same commit. **#157 and #155 close here**, each with its failing case from step 3.
5. The acceptance rows on the test accounts, and `docs/architecture.md`.

After #129's foundation, [#144](https://github.com/herbygillot/dockhand/issues/144), [#159](https://github.com/herbygillot/dockhand/issues/159), [#94](https://github.com/herbygillot/dockhand/issues/94) and [#178](https://github.com/herbygillot/dockhand/issues/178). [#183](https://github.com/herbygillot/dockhand/issues/183) lands before its last failure cases.

**For the change request ([#195](https://github.com/herbygillot/dockhand/issues/195)).** `Asked`, the destination, `ready` and the reviewer policy are the request's: #195 fills them and says which were given. The publication keeps what they resolved to for one commit, its steps and its settling. Whether the type, draft and `--skip-notification` are kept on the branch across commits, as the title and note are, is #195's flag check to decide.

## Not checked

- GitHub's behaviour is from its documentation, not from a run: that a reviewer asked again is among the pending requests until they review or the request is removed; whether asking one already pending notifies again; how soon after a request the list shows it. The design is right without any of the three; an acceptance row on the test accounts records what GitHub does.
- Whether listing pull requests by head can lag behind a create whose reply was lost. A lag only delays the row's number.
- That GitHub can't complete a create long after the connection is gone, which the two days rest on.
- That the migration's one statement can read a review's time out of the stored observation. If it can't, the same test runs once in the engine on a migrated database.
- `Archive`'s code (internal/engine/verbs.go:353) beyond its signature.
- Every caller of `pullRequestBasis` and `editBasis`, which `Provenance` changes.
- The lock under a `git push` that prompts for a credential, and how long `ship` may hold it with nobody there.

## Usage docs

- `docs/usage/publishing.md`: the question about reviewers comes with the plan, before the push; ready comes before the reviewers; a submit after a failed one says what the failed one did and finishes it, with the first one's flags standing for the same commit; `--rewrite-description` is asked again; a reviewer request that may have been sent is not sent again, and `status` shows it; a second submit of a branch is refused while one is running.
- `docs/usage/recovery.md`: the messages for a lost create, edit or record, `dockhand submit` as the one continuation, and what to do when a submission hangs.
- `docs/usage/unattended.md`: serve settles its own unfinished submissions before it opens anything; a place is held until GitHub answers; serve never finishes a person's submission or writes to a pull request that was found; on the day of an upgrade to this version it opens nothing.
- `docs/usage/branches.md`: `status` names a submission that isn't finished; archiving and forgetting settle it first.
- `docs/usage/agents.md`: `pending` in submit's and ship's JSON, with its reasons.
- `docs/architecture.md`: the publication, settling, the lock, and `serve_openings`.
- No flag or command is added or removed.
