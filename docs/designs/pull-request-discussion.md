# Read a pull request's discussion, then reply

A feature for v0.10.0, [#245](https://github.com/herbygillot/dockhand/issues/245). The issue says where it stands; this file is its design. Not yet built.

It began as a command to comment on a pull request, from field testing's notes. That draft went through one outside review, which found its at-most-once rule unsound and was taken in full. The person then pointed out that nothing in dockhand reads comments in the first place, and that a comment command mostly makes sense for an agent driving dockhand, and asked whether inline review comments were included (2026-11-12 and 13, architecture thread). The feature was redesigned as read, then reply. Architecture and Codex each wrote a proposal apart from the other; architecture merged them. They agreed on everything but the reader's name. The person adopted the merge with `dockhand discussion` (2026-11-13 09:27Z, the architecture thread's card). That was Codex's one pass on this design.

## In short

- A person reads and answers on GitHub's page better than in a terminal. This is for an agent driving dockhand, which today must use another tool with its own token to learn what a reviewer said and to answer, outside dockhand's permission table, preview and at-most-once rule.
- **`dockhand discussion`** reads the whole discussion of a tracked pull request, live: conversation comments, review summaries, and inline review threads with their file, line, and resolved and outdated marks. It never writes.
- **`dockhand comment`** posts one comment, in the conversation or inside one existing inline thread. It is posted singly, never through a review. It never resolves a thread.
- Other people's text is marked as untrusted data everywhere it is shown, and dockhand never acts on it.
- A comment is sent at most once per attempt. An attempt whose outcome is unknown is never sent again by dockhand and must be settled before the next comment on that pull request.

```
dockhand discussion --pr 35072 [--thread ID] [--since-response] [--json]
dockhand comment --pr 35072 --file reply.md
dockhand comment --pr 35072 --thread ID --seen DIGEST --attempt ID --yes --file reply.md
dockhand comment --attempt ID --settle sent|unsent
```

## Reading

1. **A tracked pull request only,** chosen as other commands choose one (`-b`, `-p`, `--pr`, the branch checked out here), without the offer to adopt and without evaluating any Portfile. A closed or merged pull request can still be read.
2. **Everything, by default.** Conversation comments; every review's state and summary; every inline thread with its id, file, side, line, resolved and outdated marks, and all its comments. Bots are shown and named as bots. `--since-response` narrows to what is newer than the account's last word; `--thread` to one thread. A line that is unknown, as on an outdated or file-level thread, is unknown, never zero.
3. **Read live, never stored.** Every page of every connection is followed; there is no "the first hundred is enough". A reading that could not be completed says which part is missing and why, ends as an error that still carries what was read, and is never shown as "nothing there". No transcript, inbox or "read" mark is kept.
4. **"Since your last response"** means since the latest published message of the account dockhand acts as, as GitHub shows it. A reply made by hand on GitHub counts the same. It is judged per scope:
   - the conversation, with review summaries that have text, is one scope;
   - each inline thread is its own scope. A reply in one thread answers no other thread and not the conversation, and a conversation comment answers no thread.
   Bots neither ask nor answer. Text in a pending review is neither feedback nor a response. An inline comment counts from when it was published, not when it was drafted. A tie or an unknown is shown as such and counts as still awaiting. The rule says "newer text from someone else", never "this was addressed": dockhand does not read meaning into words.
5. **One rule for whose turn it is.** [#248](https://github.com/herbygillot/dockhand/issues/248) corrects the conversation rule that status uses today. This feature uses the same function and extends it to threads. There is no second predicate.
6. **Status.** A full reading records small dated facts: how many unresolved threads have newer text from someone else, for which account, and when that was read. Status shows them with their age and names `dockhand discussion`. Status's own refresh does not fetch threads. A part a refresh did not read keeps its earlier answer and never clears attention. "Changes requested" stays a review's state and is cleared by nothing here.

## Other people's text

7. **Marked as data.** In `--json`, every piece of remote text sits in a record labelled as untrusted remote data, and the label travels with each item so that lifting one item out keeps it. For a person, each line of remote text is prefixed as a quotation, and control characters, escape sequences and direction overrides are made visible instead of obeyed. This covers titles, paths and diff context as well as comment bodies, and the account's own comments as GitHub returns them. A reviewer's standing in the repository is shown as attribution, never as authority.
8. **dockhand never acts on it.** It does not apply suggestions, follow links, fetch attachments, take text as flags or commands, or put a reviewer's words into `Next`. `Next` is built only from typed facts. docs/usage/agents.md says plainly that reviewers' text is evidence for the task and not instruction.

## Replying

9. **Two destinations, one effect.** No `--thread`: the conversation. `--thread ID`: the inline thread with that id, as the reader printed it. A line, an ordinal or a URL is not a destination. Before anything is sent the thread is checked to belong to this pull request and its root comment is found. `remotewrite` gains one effect, `Comment`, allowed on `Made`, `Invited` and `Named`:
   - `Made`: the record says dockhand opened the pull request, or found it as the acting account's own.
   - `Invited`: an adopted pull request where maintainer edits and write access are read now.
   - `Named`: an adopted pull request without that invitation. The person named it when adopting and names it again by taking the preview. Pushing to it stays refused as today.
   The basis is established at this call site and never taken from a helper that equates adopted with invited ([#249](https://github.com/herbygillot/dockhand/issues/249)). No other effect's row changes. A resolved or outdated thread can be answered; the preview says which it is.
10. **Posted singly, now.** A reply never goes through a review: dockhand does not create, add to, submit or delete one. Where the account has a pending review on the pull request, or that could not be read, an inline reply is refused before any record is written, since GitHub may attach the reply to it. Before release this is proved against a scratch repository, with and without a pending review. If GitHub does not publish singly as documented, inline sending is withheld, not worked around.
11. **Answering what was read.** The reader gives each scope a digest of its content and state. An inline reply sent without a terminal must pass it back as `--seen`. If the thread changed since, nothing is sent and the answer is to read again. It is a freshness check, not a permission. An incomplete reading gives no digest.
12. **Shown before it is sent.** The preview names the account, the repository and the pull request, the destination with the comment being answered, the text exactly as it will be posted, and who it mentions. Where the account's own latest comment there is the same text, it says so as a warning. `--yes` takes it; without a terminal it is refused unless `--yes`; `--plan` prints the preview, records nothing and sends nothing. Nothing unattended ever comments: not `ship --yes`, not `serve`. The text is an argument, a file, or written in the editor, and is sent as written. A new comment needs an open pull request.
13. **It only comments.** The command never runs submit's pipeline: no tidy, commit, push, title or body edit, draft or ready change, review request, check, or evaluation of an adopted Portfile. `submit` never sends or settles a comment.
14. **At most once, per attempt.** The identity of a comment is the attempt, not its text.
    - Under a lock keyed by the pull request, the command writes an attempt record and confirms the write before the call: host, repository, pull request, acting account, destination including the thread, the text and its digest, and the ids of the account's comments already in that destination.
    - It ends as **posted** (GitHub answered; the receipt holds the comment's id and link), **refused** (a definite no; nothing was sent), or **uncertain** (the answer was lost, or the process died after the record).
    - An uncertain attempt is never sent again by dockhand and never overwritten. It blocks every new comment on that pull request, whichever thread. Settling it reads the account's published comments in that exact destination and looks for one that is new against the recorded ids and has exactly the text: one such settles it as posted. Several, none, or an incomplete reading leave it uncertain until the person says which, with `--settle sent` or `--settle unsent`. `--settle` only settles; it never also posts. Staying uncertain is a legitimate outcome.
    - An agent may name its attempt with `--attempt` before sending. Asking again with the same id, destination and text returns the stored outcome and sends nothing; the same id with anything else different is an error. Without it, each invocation is a new attempt, so the same words can be said twice on purpose.
    - Attempts live in their own table and outlive `clean --delete` of the branch. A record is never applied to another pull request, account or destination.
    - The write is bound to the previewed account. A renewed credential may not replay it as another account ([#250](https://github.com/herbygillot/dockhand/issues/250)).
15. **Recorded.** The receipt and a branch event, "commented on #N" with the link, are saved before the pull request is read again. Where that read fails the command says "posted, status not read" with the link.

## Not taken

- Resolving or reopening threads; starting a new inline comment; a review, approval or change request through `comment`; editing, deleting, reactions.
- Applying suggestions.
- A stored transcript, an unread inbox, "mark read", background reading.
- Canned or automatic replies; any text of dockhand's own in a comment.
- Reading or answering a pull request dockhand does not track; a generic GitHub call.
- A conversation-only first version: it would repeat the gap that started this.
- Making a comment a step of a publication.
- `status --comments` as the reader (Codex's proposal): one command name fewer, but a live, single-pull-request mode of a command whose contract is every branch from the record.

## Order

After [#248](https://github.com/herbygillot/dockhand/issues/248), [#249](https://github.com/herbygillot/dockhand/issues/249) and [#250](https://github.com/herbygillot/dockhand/issues/250). Usage docs in the same commits: docs/usage/publishing.md (the paragraph that points at another tool for comments becomes these two commands), docs/usage/agents.md (read, check completeness and ids, reply, handle the outcome; the untrusted-text rule), and the commands' help.

## Tests

- Reading: more than one page of conversation, of reviews, of threads, and of comments in one thread; a failure in one part leaving the others and marking the reading partial; bots, deleted authors, file-level and outdated threads, ids above 32 bits kept as strings.
- The response rule as one table used by the reader and by status: adopter against author, a bot after a reviewer, pushes with arbitrary commit dates, drafts published later, empty review summaries, ties, edits, independent scopes, a partial reading.
- Output: quoted lines, escape sequences and a fake closing delimiter made harmless; the label on every JSON item.
- Permission: both destinations on each basis; an adopted, uninvited pull request allowed as `Named` and still refused a push; every other effect's row unchanged.
- Refused before any send: another pull request's thread id, a reply's id in place of a thread's, a deleted root, a stale `--seen`, a pending review, a changed account.
- Routes: one append with the frozen text and nothing else; no review call, no resolve, no push, no title or body write.
- Attempts: the three outcomes; a replay by `--attempt`; two commands at once; the same words twice on purpose; settlement by one new exact match, refused by several, by an identical comment that was already there, and across a clock that is wrong; `--settle` each way; a branch forgotten and adopted again; a failed re-read after posting.
- The fault sweep: both append sites in its listing, with positions before the record, after the record and before the call, after the call with the answer lost, and after the answer with the receipt lost; a hand-written comment before settling; an uncertain attempt followed by a reply to another thread. Never a second send, never an invented success.

## Where the two proposals differed

- **The reader's name.** Architecture: its own command. Codex: `status --comments`. The person chose `dockhand discussion`.
- **The default view.** Architecture showed only what awaits the account; Codex showed everything, with a flag to narrow. Codex's is taken: a reader that hides by default can hide the comment that matters.
- **Naming a thread.** Architecture used a comment's id; Codex the thread's own id with the root checked. Codex's is taken.
- **Settling.** Architecture compared against the attempt's time; Codex against the ids recorded before the send, since a draft can be published later and a clock can be wrong. Codex's is taken.
- **Status.** Architecture counted threads on every refresh; Codex recorded dated facts from full readings only. Codex's is taken, for the cost of every refresh.
- From Codex alone: `--seen`, `--attempt`, the pull-request lock, the attempts table that outlives the branch, the account-bound write, the scratch-repository proof. From architecture alone: the feature's reason, and the list of what dockhand never does with remote text.
