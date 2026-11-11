# The publication record: what a submission set out to do, and what it still owes

A core item for v0.9.0, [#191](https://github.com/herbygillot/dockhand/issues/191). The issue says where it stands; this file is the design. Architecture merged it on 2026-11-11 from two proposals written apart from one problem statement, architecture's and Codex's (the person's "I'd like Codex to co-design and help review as well", 2026-11-11 05:48Z, in the architecture thread). Codex then reviewed the merge against the code and said to revise it before adoption, and gave that revision a short second pass. This is the page revised for both, read again at main 1fab1150b. "What the review changed" lists each point and what was taken. The person adopted it ("Adopt", 2026-11-11 08:53Z, on the architecture thread's card), while Codex's check of the last revision was still out: anything that check breaks comes back to the person as an amendment to this page. That check came back the same day and broke two points; **the amendment** is "The last look" under "What the review changed", and the person took it ("Take it", 2026-11-11 09:34Z, on the architecture thread's card). Codex's check of the amendment itself was still out then, and what it breaks comes back to the person the same way. It came back the same day, at main ab0e07431: it confirmed the order of the lock and the reading, found one case the count's new rule gets wrong at the upgrade, placed the reading of the process list inside the migration's transaction, and still recommends that a branch's removal free no place. **The second amendment** is "The check of the amendment" under "What the review changed". Codex then checked the second amendment itself: it confirms the upgrade's two points, and holds that the release of a place on a branch's removal is not proved, which the page now says in those words. The person took the second amendment as revised ("Take it", 2026-11-11 15:50Z, on the architecture thread's card), with the release of a place where a branch is gone from the fork, against Codex's recommendation that it free nothing. This page, both amendments included, is the design.

## In short

- Today a submission records nothing until its push and its pull-request write are both done. After a failure in between, the next run works out what happened from the fork, a search and the description's text. It gets three things wrong: serve's daily count ([#155](https://github.com/herbygillot/dockhand/issues/155)), whose text the description is ([#157](https://github.com/herbygillot/dockhand/issues/157)), and a reviewer request that failed.
- A branch gets one **publication**: the commit a submission set out to publish, where, what it meant to write, what the person asked of it, and the steps it still owes. It is recorded before the first remote effect and ended when nothing is owed.
- A step that is owed may have happened. Nothing is done again until the fork or GitHub has been read and shows it isn't done.
- Two calls can't be read back: a reviewer request, and a create whose answer was lost. Each is marked before it is sent. A reviewer request that may have been sent is never sent again. A create serve may have sent keeps its place, every day, until GitHub shows what became of it. No length of time settles either.
- There is no separate "resume". A later submit settles the old publication by reading, plans as always from what it found, and records a new one. For the same commit and destination, what the person asked the first time stands unless this run says otherwise.
- serve's count moves from events, which `clean --delete` removes with a branch, to a small table. A place is taken in the transaction that records the publication.
- One dockhand at a time submits to a branch on the fork. An operating-system lock says so, and it is never taken from a process that is still alive.
- What a person sees change: "ask them to review again?" is asked before the push, with the plan. A pull request is marked ready before its reviewers are asked, where today it is after. A submit that follows a failed one says what the failed one did. `status` names a submission that isn't finished. The upgrade waits until every dockhand from before it has stopped, and on that day serve opens nothing. No command or flag is added.

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
	Earlier  int        // the newest pull request the plan found for Head and Base that wasn't open; 0 where none
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
| `open` | no pull request is open for the head | `Find` by `Head` and `Base`, which looks at every state, prefers an open one, and with none open answers with the newest closed or merged: one numbered above `Earlier` is attached; one at or below it is the earlier pull request the plan already knew, and `open` is not done; none found, not done |
| `edit` | the title or the description is to change | `Observe`: see "The description" |
| `ready` | `--ready` was asked | `Inspect`: it isn't a draft |
| `rerequest` | the plan pushes to a pull request whose reviewers requested changes, and the answer is yes or not given yet | each login's own state: see "The reviewer request" |

A step leaves `Owes` only in a transaction that also writes its result:

- `draft`, `push`, and `open` or `edit` leave together, in today's one transaction after the pull request is written.
- `ready` and `rerequest` each leave in the transaction that follows them, as `Ready` and `RequestReview` write today.
- Where that transaction fails after the effect, the step stays owed and the next read finds it done. Nothing goes on to the next effect past a record that failed.
- Each of them is a transaction dockhand has seen recorded (`store.Recorded`), as the first one is. That holds for a mark's outcome and for the receipt too.

So a plain submission costs one store write more than today: the first one.

**The order changes.** Ready comes before the reviewers are asked. Today it is the other way round.

**What a found pull request is.** GitHub's reply to dockhand's own create, held by the process that sent it, is the only evidence that dockhand opened a pull request: `Provenance` is `opened`. Any pull request a read finds is `found`, whether or not a create was tried and whoever its author is. A person can have opened it in between, and an intent to open one is not a record of having done so.

**An earlier pull request is not this submission's.** `Find` asks for every state (internal/forge/github/pullrequests.go:39). With none open it answers with the newest closed or merged one, and the plan already treats that as an earlier branch's of the same name and opens a new one (`plan.Earlier`, submit.go:839-851). Settling must make the same distinction, or a pull request closed yesterday would settle a create sent today (the last look, P1). GitHub numbers a repository's pull requests upward. So one made by this submission's create, or by anyone after the plan, has a higher number than any the plan saw. `Earlier` is the number the plan saw, and only a higher one settles `open`. A pull request that was closed and has been opened again keeps its number, so it isn't this submission's either: the old publication's `open` is not done, and the next plan attaches the open one as it attaches any.

**A create GitHub refuses.** GitHub answering the create with a refusal means nothing was opened. `open` ends as refused: the push and anything else done are folded, the refusal is returned and said, and the publication ends with nothing owed. The next submit is a new attempt. One answer is not a refusal: "a pull request already exists for this head" says that one exists, so `open` stays owed until the read finds it. The forge names that answer apart from the others.

**One read before the pull request is written.** `unchangedSince` reads before the push, and a push can be long. Just before the edit dockhand reads the title and the description once more. Where either is no longer what the plan saw, the edit is not made: the step stays owed, the command says which of them changed on GitHub during the submission, and `dockhand submit` plans against the new text. The window that remains is one request wide.

**Follow-ups are for the commit.** `ready` and `rerequest` are made only while the head is at `Commit`, and each reads for that itself, just before its call. Both read the pull request anyway: marking ready reads its draft state (`setDraft`, internal/forge/github/pullrequests.go:199), and the reviewer step reads the pending requests. The push can be minutes earlier and isn't taken as that reading. In a continuation settling also reads the branch itself, not only the pull request's copy of it. Where the head is at another commit, the publication ends and those steps are dropped and said. GitHub has no write that is conditional on the head, so a push that lands between that read and the call isn't caught; the window is one request wide.

## The reviewer request

What the step promises: after it, each reviewer named has a review request pending on the pull request, or dockhand says who doesn't and why. It does not promise that a particular notification was delivered. A request already pending, whoever made it and whenever, satisfies it.

GitHub lists a pull request's pending review requests, and a reviewer leaves that list when they review or when someone removes the request. dockhand reads none of it today: `forge.PullRequestStatus` (internal/forge/pullrequest.go:63) has the reviews and no requests, and its `ReviewedAt` is the time of the review that sets the overall verdict, not of each person's. The status read gains the complete list of pending requests, with a sign of whether it is complete, and the time of each login's own latest review of any kind, a comment-only one included.

For an owed `rerequest` with a yes, each login apart, one call a login:

| What is read, and the mark | What happens |
| --- | --- |
| The read failed or was incomplete | nothing is sent; the step stays owed and is said |
| Their own latest review is newer than `Reviewed` | dropped for that login and said: the answer was for another review |
| A request is pending | satisfied; nothing is sent |
| None pending, not sent | the mark "may have been sent" is recorded, then the request is sent |
| None pending, may have been sent | **not sent again.** Said: a request may have been sent, GitHub shows none pending, and dockhand doesn't send one twice |
| None pending, sent | nothing is sent, and the mark stays. Said: they were asked, and the request is no longer pending |
| None pending, refused | nothing is sent, and the refusal stays, with GitHub's words |

And what the call's own outcome does to the mark:

| GitHub's answer | The mark |
| --- | --- |
| Acknowledged | sent |
| A rate limit, or the login is wanting | back to not sent; the step stays owed for the next run |
| Any other refusal GitHub answers with | refused, with GitHub's words. Not asked again by this publication, and shown in the result and the receipt |
| No answer, or a server error | stays "may have been sent" |

When no login is left to send to, the step leaves `Owes` and the receipt is written to `PullRequest.Rerequested`. `status` shows a reviewer who may have been asked and one GitHub refused, beside who requested changes. A person who wants to be sure asks on GitHub.

An absent request is not read as a request that failed. Once the mark is recorded, three histories look the same to a read and to the mark: the process died before it made the call; the call went out and never arrived; the call landed, its answer was lost, and someone removed the request. Nothing tells them apart, so none of them is sent again. The only history that is sent is the one with no mark: the process died before the mark was recorded.

A receipt is a record and never a list to work through. A later publication asks only what its own question and answer cover, with marks of its own.

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

One table, `serve_openings`, replaces the count of `serve.submit` events. A row is one pull request serve opened, may have opened, or holds a place to open: the base repository, the head, the number of the newest pull request already known for that head and base when the row was written (`after`, 0 where none), and five things that may be empty: when a create was first marked (`tried`), whether a create of it went unanswered, the pull request's number, GitHub's time for its creation (`opened`), and when its head was first seen gone from the fork (`gone`). It is not deleted with a branch.

| The row | Counts on | Leaves when |
| --- | --- | --- |
| A place held: no `tried`, no `opened` | every day, while its publication owes `open` | a person takes the branch over, the branch ends, or serve can no longer submit it |
| A create that may have landed: `tried`, no `opened` | every day | GitHub answers with a refusal the only create the row ever sent; or the head branch is seen gone from the fork at two looks ten minutes apart, with no pull request found. A pull request found for it, numbered above the row's `after`, makes it an opening |
| An opening: `opened` | the day GitHub says it was created | never; old rows are pruned |

- **The limit check and the place are one transaction**, the first one: the rows that count today must be fewer than the limit, or nothing is written and nothing is pushed. SQLite has one writer, so a second pass or a second process can't interleave.
- **`tried` is written before the create is sent**, and seen recorded. From then on the place can't be given up by anything dockhand reads, because a read that finds no pull request doesn't show that the create didn't land.
- **No length of time ends a tried row.** A request whose connection was lost may have been applied, and HTTP gives no time by which it has been or never will be (RFC 9110, 9.2.2). So nothing is concluded from a day passing. The earlier draft let the row go after two days; that is withdrawn. One release below does lean on a wait, and is not a proof: a row whose head is gone from the fork, at its second look. It is the person's choice, and it is said there as what it is.
- **It doesn't matter when the create goes out.** A process that pauses between the mark and the call, for a minute or for three days, finds its place still counted when it wakes. Nothing else can settle or release the row meanwhile, because every write of it is made under the head's lock, which that process holds ("One submission at a time").
- **A pull request found** for a row that was tried is charged to it only where its number is above the row's `after`: the row then takes its number and GitHub's creation time, and counts on that day alone. One at or below `after` was there before the create and says nothing of it, so the row stays as it is. Without that, a pull request closed yesterday would turn today's unanswered create into yesterday's opening, free today's place, and let a second pull request open today beside the first when it lands (the last look, P1). One found with no creation time leaves the row as it is. This may charge serve for a pull request a person opened in between. That errs toward opening fewer, but for one case: where that pull request was opened and closed while serve's own create was still on its way, it settles the row, and the create's own pull request, when it opens, is counted nowhere. That is the twin listed under "What this does not defend", and the number doesn't remove it (the check of the amendment, A1).
- **A refusal ends a row only where nothing else of it is unanswered.** A second try that GitHub refuses says nothing of a first try that got no answer. Such a row stays, though its publication ends as refused.
- **The head gone from the fork ends it, at the second look.** GitHub opens no pull request for a head branch that isn't there, so a create that arrives after the branch was deleted is refused. One GitHub had already taken when the branch was deleted can still open, and a reading made in that moment finds neither the branch nor the pull request. So one reading releases nothing. It writes down when the head was seen gone (`gone`). A later reading, at least ten minutes on, that still finds no head and no pull request numbered above `after` releases the row. A head found there again clears the mark, and a pull request found is charged as any other.
  - **This is an escape, and it accepts that a day can go one over the limit.** A request that arrives after the deletion finds no head and is refused. One GitHub had taken before it can have opened a pull request that GitHub's list doesn't show yet at either look: GitHub ends a request after ten seconds of work ("Troubleshooting the REST API", Timeouts), but documents neither that such a request is undone nor how soon a pull request it opened is listed. Ten minutes is room, not a bound. Where that happens the row is released, another branch takes the place, and the first pull request is found too late to count (Codex's second check, B2).
  - Codex recommends the other way: a tried row ends only on positive accounting, a pull request found or a refusal, and a branch's removal ends nothing. Then a create that never landed, with its branch removed, holds one of each day's places for good; Codex has no safe expiry to offer and says so. Ending such a row would need a command of the person's, which this design doesn't have. Both are put to the person as choice 8.
  - If a branch of that name is pushed again later, it has a publication and a place of its own, and a pull request that then appears is found and charged there.
- **Every try of a row shares its place.** GitHub keeps one open pull request for a head and base, so a first try and a second can open one between them.
- **A create is sent only for a row among the first that count today**, oldest first, as many as the limit. In the usual case that is every row, since a row is only written under the limit. It matters where the limit was lowered, and after the upgrade.
- **A person's takeover.** Of a place never tried: the place is released, and the person's opening isn't counted. Of a create that was tried: the row stays, whatever the person's submit then does.
- **The pass settles first.** Before any candidate, serve settles every publication it recorded, whatever the branches' order, and every tried row whose publication has ended: it reads for the pull request and for the head branch. One that still owes `open` goes first, under the place it holds.
- **The day** is worked out at each count with `dayStart`, as today. The limit is read from the settings at each admission.
- **What it costs.** A create with no answer, no pull request found and its branch still on the fork takes one of each day's places until one of those changes. `status` and serve's own lines name the branch, the day of the create, and what ends it: finding the pull request, or removing the branch from the fork (`dockhand clean --delete`), which frees the place at serve's second look ten minutes on. In the usual case the next pass ends it by trying again and opening the pull request.
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

- `ApplySubmit` takes it before the first transaction and holds it to the end, follow-ups included. The file is handed to each child that writes for the submission, as the branch lock's is to `git` (`withLock`, internal/git/branch.go:41): `git push`, and `gh pr ready` where dockhand marks a pull request ready or draft through the GitHub CLI (internal/github/cli.go:37, :46). A child still running after dockhand died keeps the lock. Every other write is a call from dockhand's own process.
- The key is the head in one spelling: the owner and repository in lower case, and the branch. The lock's file is never removed, so two waiters can't hold two files for one head.
- A second caller is refused with nothing done. The holder writes its process and its start time into the file, and the refusal names them.
- **It is never taken from a live process.** A dockhand that hangs keeps the lock until it is stopped, and the refusal says which process to stop. The system releases it when the process and any `git` it started have exited.
- Settling's writes take it too. Where a live process is submitting the branch, a plan is refused and `status` shows what is stored.
- **The lock comes before the reading.** Whatever settles or releases under it reads the fork and GitHub after it has the lock, in the same hold as its write. `status` and a refresh have a reading before they know there is anything to settle; they take the lock and read again, since a submission can have run in between. `Publication.ID` doesn't cover this for a tried row whose publication has ended.
- Between a plan and its `ApplySubmit`, another process may have settled or replaced the publication. Under the lock, `ApplySubmit` checks that the branch's publication is the one the plan saw, and otherwise refuses as it does for a head that moved.
- Every write of the publication checks `Publication.ID` in its own transaction. The first transaction writes one only where the branch has none, and that test is in the transaction itself.
- It is not the branch's history lock. The publication publishes the literal commit it recorded, whatever the local branch does afterwards; that is intended, and `status` then says how the branch stands to what was published: ahead of it, behind it, or diverged from it.

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

- The schema adds the column, the two fields on the pull request's record, and the table. No branch row changes. A branch recorded before has no publication and owes nothing, and its pull request's `Provenance` is empty.
- **An old record doesn't show who opened its pull request.** The old plan attached any open pull request it found for the head and recorded it like one it had opened (`Find`, submit.go:829-851). So an empty `Provenance` proves nothing, and the page doesn't say it does. Such a record keeps the rights it has today, with one thing added: where GitHub names another author than the account dockhand acts as, it is recorded as `found` at its next read. The author's login is only ever used to withdraw `Made`, never to state it.
- **The migration waits for quiet, and reads the Mac's process list for it.** It is refused while any other process on this Mac is running a program named dockhand, whatever that process is doing and whether or not it has a session. The sessions table can't answer this. A submit, an update and a ship open no session (`Processes`, internal/engine/processes.go:25), and such a submit, paused after its last read of the database and before its push, would wake after the migration, push, and open or edit its pull request with no record and no lock (the last look, P3). The list is the system's own (`kern.proc.all`, read as `procid` reads one process, internal/procid/start_darwin.go:15). The refusal names each process to stop, by its PID. Where dockhand can't read the list, as inside a sandbox, the migration is refused and says so. Nothing overrides either refusal: today's guard counts only sessions with a recent heartbeat (`refuseUnderLive`, internal/store/sqlite/sqlite.go:594, with `watchHeartbeats`, :267), "migrate anyway" lifts even that (`MigrateUnderLive`), and neither applies to this migration.
- **The list is read once, inside the migration's own transaction, where today's guard runs.** The reading comes after the transaction has taken the database's writer lock and before it changes anything (`BEGIN IMMEDIATE`, sqlite.go:718; `prepareSchema`, :553, at the call to `refuseUnderLive`, :565), and the lock is kept through the commit. Every dockhand's opening of the database passes through that same transaction before it has a database to use (`Open`, :246). So an earlier dockhand that starts after the reading can't finish opening until the migration has committed, and then finds a newer schema and refuses (`newerSchema`, :571). One that had the database open already was running at the reading, and the migration is refused; every later transaction of such a process would be refused too (:750-754). The list can't tell versions apart, so a dockhand of this version that is starting in the same moment refuses the migration as well, and the command is run again.
  - The amendment first read the list before the migration and again after its commit, and recorded the processes of the second reading. Codex's check showed that the first reading decides, and only where it is made: read before the writer lock, an earlier dockhand could start after it, open, make its last read, write to the fork or to GitHub and exit, all before the second reading, and be in neither (A4). The second reading had no record that survived a crash either. With the one reading under the lock there is nothing left for a second to find, so it and its record are gone, and no command waits on a list of processes after the upgrade.
- **What an old submit may have left on its way.** The old code sent its create only for a head that was already on the fork: it pushed first, or found the fork's branch at its commit and pushed nothing (submit.go:1311). It recorded neither until both were done. So a create of serve's whose answer was never recorded leaves one trace: a branch serve prepared that is on the fork with no pull request recorded. Only a submit pushes a branch under its own name (submit.go:1314; a check on GitHub pushes `dockhand-check/…`, provider.go:236). serve's first pass after the upgrade lists the fork's branches once. For each such branch it reads for a pull request and records a tried row with a create unanswered. An open one found is charged to the day GitHub made it. One that isn't open is read by when GitHub created it, against the day of the upgrade, which the migration writes into the table. The two are compared as days, by the count's own day (`dayStart`), not as two instants. Created on that day or before, it can't be told from an earlier branch's of the same name, so it settles nothing: its number becomes the row's `after`, and the row stays a tried row, as one with nothing found does, by the rules above. Created after that day, it is the row's opening, on the day GitHub made it: every earlier dockhand had stopped by the upgrade and serve opens nothing until this pass, so the only create of dockhand's that can have opened it is the one the row stands for. The amendment first took every pull request found not open as a cutoff. An old create that landed the day after the upgrade, and was closed before this pass, then left the row tried, and serve's own submit of the branch opened a second pull request that day under the one place (the check of the amendment, A1). A branch that already has a publication is not part of this pass. Nor is one the pass already wrote a row for: a pass cut short is run again and writes each branch's row once, and it is marked done in the table only when every such branch has been read. Until that pass has run, serve opens nothing; where its read fails, the next pass runs it again.
- **On the day of the upgrade serve opens nothing.** The pull requests serve opened earlier that day are counted only by events, which this design stops trusting. The migration writes its own time into the table, and for the rest of that day the count answers that the limit is reached, and says why. From the next midnight every opening is in the table. Nothing is imported from `serve.submit` events, whose times are when dockhand recorded and not when GitHub created.
- A branch whose last submit failed part-way before the upgrade takes the path it takes today: `PlanSubmit` finds its pull request by head (`destination`, submit.go:776). It is attached as `found`.
- **Old reviewer left-overs are said once and never sent.** The migration applies `RerequestDue`'s test one last time, in one statement: where a branch's pull request has changes requested, a push recorded after that review, and no request recorded since, it writes a receipt saying the reviewers may not have been asked. `status` shows it. Then `RerequestDue` goes. A left-over whose events were already pruned can't be found, and isn't guessed.
- `clean` refuses an open branch, as today. Archiving and forgetting end the publication first, by the rule above, and refuse if they can't.
- `undo` moves a branch's history and removes nothing remote (`Restore`, internal/engine/restore.go:23). Where it moves the head off `Commit`, the next submit treats the publication as one for another commit.
- The publication is on the branch's row and is never pruned. An opening's row older than two days may be. A tried row is never pruned.

## The gate

Each step states a basis to the gate (`internal/remotewrite`), built from evidence when the step is made. `By` says who recorded a publication. It is never a basis.

- Push: `OwnFork`, or `Invited` where the head repository is someone else's, as today. Open: `OwnFork`.
- Edit, draft and ready state `Made` where the record's `Provenance` is `opened`. They also state it where `Provenance` is empty on a record from before that isn't adopted, which is what `pullRequestBasis` answers today: that is today's right kept, not a proof that dockhand opened it ("Existing databases and branches"). `Made` is never stated from a publication's intent or from the author's login.
- A `found` pull request: a person's submit of that branch names the action, so its writes state `Named`, and the plan says the pull request was found. serve leaves it.
- Someone else's, or adopted: `Invited`, as today.
- A reviewer request states `Made` on a pull request dockhand opened, and otherwise `Named` where the person answered yes. The yes is recorded with what it covers: the pull request, the head, the logins with their reviews, and who answered. Today an adopted pull request's request states `Invited`, which the gate refuses (`AskReviewers` takes `Made` and `Named`, internal/remotewrite/remotewrite.go:91).
- `clean`'s test for deleting a head branch (`ownedHead`, internal/engine/clean.go:379) reads neither `Provenance` nor the publication, and gains nothing from them.

#144's adopted comment says a basis should become something only evidence can build if a new caller ever states `Made` with no record behind it. Here a new `Made` comes only from GitHub's answer to a create. An old record keeps the one it had, unproved, and loses it where GitHub names another author.

## What the review changed

Codex's review made seven objections, each with a counterexample, and six corrections of fact. All are taken.

| Its point | What was wrong in the merge | Taken |
| --- | --- | --- |
| R1. An absent pending request is not a failed notification | It sent again wherever GitHub showed no request pending, and took one review time for every reviewer | A mark for each login, recorded before the call. "May have been sent" is never sent again. Each login's own review time. A receipt on the pull request's record |
| R2. Ordering doesn't make a released place safe | A person's takeover, an ended branch or a later read released serve's place although its create could still land | `tried`, recorded before the create. A tried row counts every day and leaves only on evidence: see the second pass below |
| R3. A terminal state doesn't erase accounting | "Merged or closed: the publication is cleared", and a third commit ended it at once | "Ending a publication": folding first, in the same transaction, for every way it ends |
| R4. A stale edit doesn't write the same text | A lease, which is taken from a live process that is silent | The operating-system lock, never taken from a live process. The lease goes |
| R5. An intent and the author's login are not evidence of creation | "An open one by `By` is the one this publication opened", and `Found` only where there was no publication | `Provenance`: `opened` only from GitHub's answer to the create. Everything read is `found`. `ownedHead` unchanged |
| R6. The upgrade kept #155 | No account of an opening lost before the upgrade, and an import from events | The migration waits for quiet; the first pass records what an old submit may have left; serve opens nothing that day; nothing is imported |
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

**The second pass** (Codex, 2026-11-11, at main 1fab1150b) found most of that met and left these. All are taken but the last, which is put to the person.

| Its point | What was wrong in the revision | Taken |
| --- | --- | --- |
| No two days | A tried row stopped counting after the day it was tried and the next. HTTP gives no time by which a lost request has been applied or dropped | No age ends a tried row. It counts every day until a pull request is found, its only create is refused, or its head is gone from the fork |
| A create marked, then sent days later | Once the row stopped counting, another branch took the limit, and the first then opened | Covered by the same rule. The row is also written only under the head's lock, which the paused process holds |
| A later refusal doesn't answer an earlier try | A second try's refusal released the place | A row with a create unanswered stays through any later refusal |
| A dockhand paused across the upgrade | The page admitted it, and the day's pause didn't confine it. The store's guard leaves out silent sessions and can be overridden | The migration waits until no session's process is running, silent or not, with no override. Replaced by the last look's P3, below |
| An old create still on its way | Midnight was taken to settle it | The first pass records a tried row for each of serve's branches on the fork with no pull request recorded |
| An old record's `Made` proves no creation | "Read as today" was presented as if it lost nothing | Said plainly, and the old plan's `Find` cited. Withdrawn where GitHub names another author. Whether to go further is choice 7 |
| A kill after the mark and before the call | Case G called it "not sent, so sent now", and the page said the mark tells the histories apart | It reads "may have been sent" and is not sent. Three histories, not told apart |
| A `sent` or `refused` mark with none pending | No row | Two rows: neither is sent again, and neither becomes "not sent" |
| The `gh` child | Only `git push` was handed the lock | `gh pr ready` is handed it too. It is the one other child that writes |
| The title | The read before the edit named the description alone | The title and the description |
| A reading before `ready` and the reviewers | The push was taken as that reading | Each reads the head itself, just before its call |
| `status` says "ahead" | The branch can also be behind or diverged | Says which |
| A comment-only review | Not named | Each login's latest review of any kind |
| Case D | Said the earlier submit opened the pull request | Says it was found, and may have been opened by it |
| The migration's statements | To be proved | Tested against a database made at schema 059 |

**The last look** (Codex, 2026-11-11, at main 1fab1150b, after the person had adopted the page) confirmed three of five points and broke two. Both are taken. This is the amendment.

| Its point | What was wrong in the adopted page | Taken |
| --- | --- | --- |
| P1. An earlier pull request releases today's place | Any pull request `Find` returned settled a tried row. With none open, `Find` returns the newest closed or merged one, which can be yesterday's | The row keeps the newest number known when it was written, and only a higher one settles it. The publication keeps the same number for `open`. The first pass after the upgrade does the same |
| P1. The head gone from the fork | Said GitHub refuses a create for a branch that isn't there, as if that covered one it was already working on | Said as not shown, and listed under "What this does not defend" |
| P2. The lock holds, if the reading comes after it | The page didn't say when a settler reads | The lock first, then the reading, then the write, in one hold. `status` and a refresh read again under it |
| P3. The sessions table doesn't list every old writer | A submit opens no session. Paused before its push, it would wake after the migration and write with no record and no lock | The migration reads the Mac's process list, and no session is asked. Where the list can't be read, it is refused. It first read the list before and after its commit; the check below moved the one reading inside the transaction |
| P4. An old submit can create without pushing | "Only after its push" | "Only for a head already on the fork" (submit.go:1311) |
| P5. An old record's `Made` | Confirmed as the page has it | |

**The check of the amendment** (Codex, 2026-11-11, at main ab0e07431, the page read at 45df2864b) confirmed one of four points outright, found two wanting and disagrees on one. Each was read against the code again. This is **the second amendment**, which the person took ("Take it", 2026-11-11 15:50Z).

| Check | What it found in the amendment | Taken |
| --- | --- | --- |
| A1. Only a higher number settles a create | Confirmed for a create this design records. A higher number can still be a person's twin, so "errs toward opening fewer" was said too widely | Said, with the twin named where the page lists what it doesn't defend |
| A1. The first pass after the upgrade | A pull request found not open was taken as there before the old create. It can be the old create's own: landed the day after the upgrade, closed before the pass. serve then opened a second that day under one place | The pass reads when GitHub created it. After the upgrade's day, it is the row's opening. On that day or before, it is the cutoff, as before |
| A2. The head gone from the fork | Naming the gap doesn't close it. Codex recommends that a tried row survive its branch's removal | A second look ten minutes on, before the row is released, which narrows the gap and doesn't close it (the second check, B2, below). Codex's way is put to the person beside it (choice 8) |
| A3. The lock comes before the reading | Confirmed | Nothing changes |
| A4. The process list, read before and after the commit | Holds only where the first reading is made under the migration's writer lock. Read before it, an earlier dockhand could start, open, write remotely and exit between the two readings. Codex held that lock in a second process and saw an opener wait, then refuse the newer schema | One reading, inside the transaction. The second reading and its record go. A `git` or `gh` an earlier dockhand left running is named as not covered |

Codex checked these answers the same day (at checkout fce0515a, the page read at b9905719a).

| Second check | What it found | Taken |
| --- | --- | --- |
| B1. The first pass, by the day of creation | Confirmed for the case reported. Compare days as the count does, make the pass safe to run again, and say that a retry can make a twin too | Each said |
| B2. The release at a second look | Not proved. GitHub's ten seconds bound its work on a request, not whether a pull request it opened is listed by then. Codex still recommends keeping the row, and names its cost: such a row may never end | The release is said as an escape that can put a day one over the limit, and "no length of time" is qualified. The choice is the person's |
| B3. One reading under the writer lock | Confirmed. The second phase has nothing left to find | Nothing changes |

One thing is still taken in a smaller form than the review's, and it should say if it is too small: a refused reviewer isn't sorted into "can never be asked" and "can't be asked now", beyond the rate limit and the login. Every other refusal ends that login for this publication and is shown.

## Where the two proposals differed

| | Architecture | Codex | Taken, and what decided it |
| --- | --- | --- | --- |
| 1. Where the record lives | one value on the branch | a table of publications, with predecessors and versions | The branch's value. What must outlive it is written down as it ends |
| 2. A step's states | owed or not; a read decides | seven states, with a mark saved before every call | Owed or not for the four steps a read decides. A mark for the two it can't: each reviewer, and serve's create |
| 3. A reviewer request of unknown outcome | read the pending requests; ask if it isn't there | it can't be observed; never sent again | Codex's. The read satisfies a request found pending; it never licenses a second send |
| 4. Resuming | settle and plan afresh; only `ready` and a yes carry | no flags resumes the saved choices; other flags make a fresh acceptance | One path, with Codex's rule on choices, for the same commit and destination |
| 5. A no to the reviewers | not kept | kept for the head and the review | Codex's |
| 6. serve's count | `serve.submit` events and reservations | a ledger with states, limits, days and zones | A small table of its own; the limit and the day read at each count |
| 7. Giving up a place | released when a read shows no pull request | one read isn't proof | Codex's. A place whose create was tried is never released by a read that finds nothing, nor by time |
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
| 18. Existing databases | add a column | import associations and charges, a marker and a pass for possible old openings | No import from events. A quiet migration, a first pass for what an old submit may have left, and the day's pause |
| 19. Old reviewer left-overs | dropped | classified once as an old uncertainty | Said once by the migration, never sent |
| 20. Commits | six | one cut-over | Pieces, then one activation |

## The cases

"Settle" is rule 3. "Fold" is writing a step found done into `PullRequest` and taking it out of `Owes`.

| | The store then | The next command, and what it does | The person is told |
| --- | --- | --- | --- |
| A. The create's reply is lost | owes `push`, `open`; serve's row is tried | submit: settling finds the push and the pull request, folds, attaches it as found, and charges serve's row | #N was found open for this branch; the earlier submit may have opened it |
| B. GitHub refuses the create | `open` ends as refused; serve's place is released | the next submit, or serve's next pass, is a new attempt | GitHub's refusal |
| C. The edit failed after the push | owes `push`, `edit`, and `rerequest` if asked | submit: folds the push, edits, asks the reviewers as answered | what the earlier submit did, then this one |
| D. All done, the record failed | owes all it did | submit: settling finds all of it and folds; a pull request it set out to open is attached as found | #N was found open for this branch; the earlier submit may have opened it |
| E. Killed between two steps | owes from that step on | as A, C, D, F or G by where it stopped. After the return to draft: settling finds the draft, and the plan pushes | what was found, what was done |
| F. `--ready` asked, lost before it | owes `ready` | (i) submit and (ii) ship mark it ready, saying it was asked earlier; `--draft` drops it. (iii) serve never acts on a person's | "marking #N ready, as the earlier submit asked" |
| G. The reviewer request | owes `rerequest`, a mark for each login | (i) died before the mark was recorded: not sent, so sent now. (ii) died after the mark, before or during the call, or the answer was lost: may have been sent; pending, satisfied; not pending, not sent again. (iii) a no: not owed, not asked again. (iv) no answer: an attended run asks, an unattended one leaves it. (v) a yes: asked by the next run of that commit | who was asked, who may have been, who GitHub refused |
| H. An old-dated commit pushed after the review | `Review` in the publication | owed because the plan pushed to a pull request with changes requested. No commit date and no event is read | |
| I. #155 | serve's row is tried | the pass settles first, finds the pull request, charges the row; the limit of one is reached | today's limit is reached |
| J. A second ready branch sorts first | as I, unsettled | settling comes before candidates; a tried row counts whether or not it is settled | the second branch waits |
| K. A place at midnight | serve's row | (i) opened before midnight: counted yesterday, today is free. (ii) held, not tried: it goes first today. (iii) opened after midnight: counted today. (iv) tried, nothing found: counted today and every day until it is settled | |
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
| AI. An opening lost before the upgrade (R6) | no row | the first pass finds the branch on the fork with no pull request recorded, records a tried row and reads for it; serve opens nothing that day | serve counts afresh from tomorrow |
| AJ. A create marked, and its process then sleeps three days | serve's row is tried; the process holds the lock | the row counts each of those days and nobody else can settle it. Awake, it sends; the opening is charged to the day GitHub made it | |
| AK. A first create unanswered, a second refused | tried, one unanswered | the publication ends as refused. The row stays until a pull request is found or the head is gone from the fork | one place is held for that branch, and what ends it |
| AL. A dockhand from before the upgrade is paused | it is in the Mac's process list, with a session or without | the migration is refused | which process to stop |
| AM. The limit is lowered below the rows that count | three tried rows, a limit of one | only the oldest row's create is sent today | the others wait |
| AN. An earlier pull request for the head was closed yesterday, and today's create gets no answer (the last look, P1) | tried, with `after` the earlier one's number | the read finds the earlier one, which settles nothing; the row counts. The new one, when it shows, has a higher number and is charged to its day | one place is held for that branch |
| AO. An old submit past its last read of the database as the upgrade comes (P3) | no session | it is in the process list, and the migration is refused. One that starts later can't open the database | which process to stop |
| AP. dockhand can't read the process list | | the migration is refused | that it couldn't read the list, and to run dockhand once outside the sandbox |
| AQ. An old create lands the day after the upgrade and is closed before serve's first pass (the check, A1) | no row | the pass finds it, not open, created after the upgrade's day: the row is its opening and counts on that day. serve's own submit of the branch then needs a place of its own | |
| AR. A tried row's branch is removed from the fork (the check, A2) | tried | the first look writes down when. A look ten minutes on that finds no head and no pull request releases the row; a pull request that shows meanwhile, open or not, is charged; a head found again clears the mark | the place is freed at serve's second look |
| AS. An earlier dockhand starts as the upgrade runs (the check, A4) | the migration holds the writer lock | it can't open the database until the commit, then finds a newer schema and refuses. One already running at the reading refuses the migration | which process to stop, or that the database is newer |

## Choices the person should see

The person took all eight as written, with the page ("Adopt", 2026-11-11 08:53Z). The last look amends the fourth, in how an earlier dockhand is found and in one thing the person can meet; the person took the amendment ("Take it", 2026-11-11 09:34Z). Codex's check of that amendment changes the eighth, as marked there; the person took that too ("Take it", 2026-11-11 15:50Z).

1. **A reviewer request that may have been sent is never sent again.** After a lost answer dockhand says who may have been asked and stops. The other way sends twice when someone removed a request that had landed.
2. **A dockhand that hangs while submitting blocks that branch until it is stopped.** A second one is refused and names the process. The other way lets the second take over, and the first can then write over it when it wakes.
3. **A pull request found after a lost answer is not treated as dockhand's own.** A person's submit goes on with it, saying it was found. serve charges it to the day and writes nothing more to it.
4. **The upgrade waits until every dockhand from before it has stopped, and on that day serve opens nothing.** A dockhand that is paused has to be stopped by hand first; the refusal names it. The other way lets a paused one wake after the upgrade and open a pull request nothing counts. *Amended:* an earlier dockhand is found in the Mac's list of running processes, since a plain submit leaves no session to find it by. Where dockhand can't read that list, as inside a sandbox, the upgrade is refused there and has to be run once outside it.
5. **`--rewrite-description` is not carried** to a continuation, where every other choice of the same commit is.
6. **A reviewer request on an adopted pull request is made** after a yes, where today the gate refuses it.
7. **A pull request recorded before the upgrade keeps the rights it has today**, though the old record doesn't show that dockhand opened it. It loses them only where GitHub names another author. The other way treats every old one as found: serve then writes to none of the pull requests it opened before the upgrade until the person has submitted each once.
8. **A create whose answer was lost keeps one of each day's places** until GitHub shows its pull request or its branch is gone from the fork. Usually the next pass ends that by opening the pull request. The other way lets the place go after two days, and a create that lands later than that puts a day one over the limit. *Amended a second time:* a branch removed from the fork frees its place only at serve's second look, ten minutes after the first, with no pull request found. That is a way out and not a proof: where GitHub had already opened the pull request and doesn't list it by then, that day goes one over the limit. Codex recommended that a branch's removal free nothing. A create that never landed would then hold a place every day until the person ended it by hand, with a command this design would have to add. The person took the release.

## What this does not defend

- A request that had left the process when it died. The push is guarded by `Expected`. GitHub refuses a second open pull request for one head, and serve's row stays charged for as long as it takes. An edit could land late; the window is one request.
- A create that lands after its pull request's twin was opened and closed, or after its branch was removed and a person pushed one of the same name by hand. Each needs a request to be on its way for longer than a branch's life. In the first, the twin is charged to serve's row in the create's place, and the create's own pull request then goes uncounted on its day. A row's own second try can make a twin as well: GitHub keeps one open pull request for a head and base, not one ever, so where the first try's pull request is opened and closed before the second try lands, both open on one place.
- Another machine, or another database, submitting to the same branch.
- A person acting on GitHub between dockhand's read and its call. GitHub offers no write that is conditional on what was read.
- A branch forgotten before the upgrade while its create was still on its way. Nothing records it, and it isn't guessed.
- A create GitHub had taken when its head branch was deleted, whose pull request GitHub doesn't list by serve's second look. The row is released, and that pull request goes uncounted on its day. Nothing GitHub documents rules this out; the release is an escape the person chose, not a proof.
- A dockhand from before the upgrade that runs under another program name, or on another machine or in a virtual machine against this database. The process list shows this Mac's processes named dockhand. Nor a `git` or a `gh` that an earlier dockhand started and left running: it has another name, and it can still finish a push or mark a pull request ready after the upgrade, with no record. Neither opens a pull request.

## Tests

- **Settling**, one table: a publication and what each read answers, complete and cut short, against what is folded, what stays owed and what is said.
- **The reviewer request**, one table over the two tables above: each reading and mark, and each answer. It includes AC and AD, and a request to several logins where one is acknowledged, one refused and one unanswered.
- **The description**: a table in `prdescription` for the three parts, each landed, not landed, and edited by a person; the whole text landed, for a rewrite, serve's note and a part left out; with #157's failing case written first.
- **The count**: two engines on one database at a limit of one; midnight in each of K's four forms; AE, AF, AJ, AK, AM and AN; a create marked and sent three days later while a second branch asks for the place each day; a row whose head left the fork, kept at the first look and ended at the second, and one whose head is back, or whose pull request shows, between the two; AQ, with the same pull request created on the upgrade's day, which is a cutoff; the first pass cut short and run again, which writes each row once; the twin, by a person and by the row's own second try, each shown and each listed as not defended; a branch forgotten on the day its pull request opened; a database at schema 059 with `serve.submit` events, upgraded, on that day and the next.
- **The lock**: a second process refused while the first sleeps before its edit; a `git push` child and a `gh pr ready` child that each outlive their parent; two spellings of one head; the refusal's words.
- **The upgrade**, against a database made by the code at schema 059, not written by hand: a submit killed after its create, then upgraded, the first pass run, and submitted again; a process named dockhand that has no session, which refuses the migration; the process list unreadable, which refuses it; a process that starts while the migration holds the writer lock, which can't open the database until the commit and then refuses; the reading shown to be made under that lock, by a process that starts just before it and is refused; an old reviewer left-over, said once; an old record whose pull request GitHub gives another author.
- **The cases** A to AI against the GitHub stand-in. It keeps a pull request's draft state and pending review requests, lets a request be removed, keeps each person's reviews apart, answers a request for some logins and not others, and can delay a call past its caller's death. dockhand's own rule is asserted by count: no login is sent two requests by one publication.
- **The fault sweep** ([#129](https://github.com/herbygillot/dockhand/issues/129)): a failpoint after the first transaction, after each mark, and after each effect and each record. Where every outcome can be read back, a continuation is the same command under the same attendance and clean-run equality holds. Where one can't, the scenario declares it: the expected end state has the mark, the row and the texts exactly, and no second send. Where a read is made to fail, it asserts no remote write and the sentence that names the next command. `serve_openings` is compared exactly; no event of a submission is read as state.
- The regressions of #76, #118 and #127 stay.

## How it is built

1. The forge's status read gains the pending review requests, complete or said not to be, and each login's own latest review. The create's "already exists" gets its own name. The stand-in gains what "Tests" lists.
2. The question moves before the push. The engine marks ready and then asks the reviewers. The command says which flags were given. `RerequestDue` still serves left-overs. The usage pages change here.
3. The pieces, each with its tests and no caller: the publication's type and settling as a function from readings to folds; the reviewer table; the description settled whole and by part; `serve_openings` and its count; the lock; `pending`'s reasons.
4. **One activation.** The schema with its two statements for old databases, the migration's wait for quiet, and serve's first pass; the first transaction; the marks; the steps and the reads before them; settling at the plan, serve's pass, `status`, a refresh, archive and forget; serve's place; the lock; the bases from `Provenance`. `RerequestDue`, `servedToday`'s event count, cleanup's rule for it, and `followUp`'s left-over branch go in the same commit. **#157 and #155 close here**, each with its failing case from step 3.
5. The acceptance rows on the test accounts, and `docs/architecture.md`.

After #129's foundation, [#144](https://github.com/herbygillot/dockhand/issues/144), [#159](https://github.com/herbygillot/dockhand/issues/159), [#94](https://github.com/herbygillot/dockhand/issues/94) and [#178](https://github.com/herbygillot/dockhand/issues/178). [#183](https://github.com/herbygillot/dockhand/issues/183) lands before its last failure cases.

**For the change request ([#195](https://github.com/herbygillot/dockhand/issues/195)).** `Asked`, the destination, `ready` and the reviewer policy are the request's: #195 fills them and says which were given. The publication keeps what they resolved to for one commit, its steps and its settling. Whether the type, draft and `--skip-notification` are kept on the branch across commits, as the title and note are, is #195's flag check to decide.

## Not checked

- GitHub's behaviour is from its documentation, not from a run: that a reviewer asked again is among the pending requests until they review or the request is removed; whether asking one already pending notifies again; how soon after a request the list shows it. The design is right without any of the three; an acceptance row on the test accounts records what GitHub does.
- Whether listing pull requests by head can lag behind a create whose reply was lost. A lag only delays the row's number, except at the release of a row whose head is gone, which takes the listing to show within ten minutes a pull request GitHub opened.
- Two things GitHub does that the count leans on, neither from a run here: it opens no second pull request for a head and base while one is open (the stand-in does the same, internal/forge/forgetest/github.go:274), and none for a head branch that isn't on the fork.
- That `kern.proc.all` names every process on the Mac for a caller that isn't root, and what a sandbox answers it. Where it can't be read the migration is refused, so a wrong guess here costs an upgrade that has to be run elsewhere, not a missed process.
- What GitHub does with a create it is working on when the head branch is deleted. That it ends a request after ten seconds of work is from its documentation, which doesn't say whether such a request can still have taken effect, nor how soon a pull request it opened is listed; not from a run.
- That the status read already carries the pull request's author for every record (`forge.PullRequest.Author`, internal/forge/pullrequest.go:49).
- That the migration's one statement can read a review's time out of the stored observation. If it can't, the same test runs once in the engine on a migrated database.
- `Archive`'s code (internal/engine/verbs.go:353) beyond its signature.
- Every caller of `pullRequestBasis` and `editBasis`, which `Provenance` changes.
- The lock under a `git push` that prompts for a credential, and how long `ship` may hold it with nobody there.

## Usage docs

- `docs/usage/publishing.md`: the question about reviewers comes with the plan, before the push; ready comes before the reviewers; a submit after a failed one says what the failed one did and finishes it, with the first one's flags standing for the same commit; `--rewrite-description` is asked again; a reviewer request that may have been sent is not sent again, and `status` shows it; a second submit of a branch is refused while one is running.
- `docs/usage/recovery.md`: the messages for a lost create, edit or record, `dockhand submit` as the one continuation, and what to do when a submission hangs.
- `docs/usage/unattended.md`: serve settles its own unfinished submissions before it opens anything; a place whose create got no answer is held every day until the pull request is found, or the branch is removed from the fork and serve has looked twice, ten minutes apart, and serve says which branch; serve never finishes a person's submission or writes to a pull request that was found; the upgrade to this version is refused while an earlier dockhand is still running on the Mac, and where dockhand can't read the Mac's process list, and on its day serve opens nothing.
- `docs/usage/branches.md`: `status` names a submission that isn't finished; archiving and forgetting settle it first.
- `docs/usage/agents.md`: `pending` in submit's and ship's JSON, with its reasons.
- `docs/architecture.md`: the publication, settling, the lock, and `serve_openings`.
- No flag or command is added or removed.
