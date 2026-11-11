# The preparation result and the change request

Two core items for v0.9.0, [#194](https://github.com/herbygillot/dockhand/issues/194) and [#195](https://github.com/herbygillot/dockhand/issues/195). The issues say where they stand; this file is the design of both. Architecture merged it on 2026-11-11 from two proposals written apart from one problem statement, architecture's and Codex's (the person's "I'd like Codex to co-design and help review as well", 2026-11-11 05:48Z, in the architecture thread). Codex's was written at main 2bdf27a06, and the merge was read against main ab0e07431. "Where the two proposals differed" lists each difference and what decided it. Codex has not reviewed the merge yet and the person has not adopted it, so nothing is built from it.

## In short

- Today an update writes a port's files and then records the edit. Where the record fails, the next run finds the files at the new version, says "nothing to change", and tidy calls them a hand edit. Most of what an update found is printed once and gone. What a ship was asked is held by its process alone, so a ship started again looks for a release again and forgets how far it was to go.
- **The edit is recorded before its files are written**, with what preparing it found. A second, small record says the files were written. A run that finds the first without the second reads the files: all written, it completes the record; not written, it writes them from what was kept; changed by someone, it stops and overwrites nothing. dockhand's own edit is never called a hand edit again.
- **What an update found is kept with its edit**: each finding with what was done about it. ship lists the notices its updates raised from these records, after a restart too (the first part of [#180](https://github.com/herbygillot/dockhand/issues/180)).
- **A hold before the check is read, not stored.** One verdict, `branchstate.Preparation`, answers for ship, bump, serve and status from the same records. It gains one answer, unfinished. A patch that no longer applies holds the port until an update of that port has tried its patches again; a later revision bump no longer lifts it.
- **Recovery expects exactly what dockhand committed**: the branch's head is the commit its own tidy made, every changed file is as its edits left it, and nothing is uncommitted. Anything else is named and left for the person.
- **One request** holds what was asked of a branch's work: the ports and the release selected for each, the source and side actions, how far to go, and what was asked of the check and the submission. One resolver builds it for update, bump, ship and serve. It is kept on the branch from the first edit until its submission has finished.
- **Every flag is acted on, already so, refused by name, or kept for the stage that reads it and said.** "Already at the newest version" stops being the whole answer: the version is one action among those asked, and each has its own outcome. That is [#121](https://github.com/herbygillot/dockhand/issues/121).
- **The flag check** is a table in the code with a row for every flag of the commands in scope, a test that fails on a flag with no row, and a behaviour test for each row.
- What a person sees change is under "Choices the person should see". No command or flag is added.

## Today

The problem statement's reading of the code (parts 2 and 3 of the packet) stands, with the corrections under "Corrections to the statement". The anchors this design leans on, at main ab0e07431:

- `Engine.update` (internal/engine/update.go:384) prepares an edit into a tree of its own, returns there for a plan (:645), starts the branch (:652), writes the files (`ApplyToWorkingFiles`, :661), builds the edit (`editRecord`, :683), works out its assessment from the working files as written (`editFindings`, :694 and :833), reads the Portfile's instruction comments from disk (:698), and records all of it in one transaction (:702). Between :661 and :702 the files are in the worktree and the store knows nothing.
- On the second run the release resolves as current, no files come back, and `Current` is set (:633). tidy then finds files no recorded edit explains: "has changes dockhand's commands did not make; review them" (internal/engine/tidy.go:469).
- `ApplyToWorkingFiles` (internal/git/working.go:110) checks every file's presence, blob and mode against what was captured before it writes any (:119-131). `model.EditedFile` keeps the two blobs and no mode.
- `branchstate.Preparation` (internal/branchstate/preparation.go:37) answers hand-edits, held or ready. It calls any uncommitted edit the person's, and reads a hold from the newest edit of each port whatever its kind (:45-52), so a revision bump made after a held update lifts the hold.
- The one stored hold is written by serve's driver before the commit (`holdBeforeCheck`, internal/engine/ship.go:145, :230). The command's `shipOne` writes none and reads none, though `ship --yes` says it holds where serve would (internal/command/ship/ship.go:149).
- `ResumePrepared` (internal/engine/recovery.go:42) queues a check for a branch of serve's with no pull request, no run and a head that isn't its base. A branch whose edits were written and never committed is passed over (:65), and the outdated pass skips its port as "already in" that branch (internal/engine/outdated.go:442). Nothing takes it up again.
- tidy and rebase leave a checkpoint with the head before and after (`model.Checkpoint`, internal/model/history.go:306). History's transitions run under a lock for the branch (`WithBranchLock`, internal/git/branch.go:32, taken at internal/history/history.go:49). `Engine.update` takes no lock.
- The request is built four times: by `VersionUpdate.Request` (internal/command/author/author.go:398), by `shipOne` (ship.go:673), by bump through ship, and by serve's `Engine.ship` (internal/engine/ship.go:80).
- A check asked with no test policy takes the configured one (`cmp.Or(request.Tests, e.options.Tests)`, internal/engine/plan.go:67), serve's included.
- Configuration is read when a process starts (docs/usage/configuration.md:258).

## The records

```go
// On model.Edit, stored on the edits row.
Written time.Time   // when its files were seen written; zero until then
Result  *EditResult // what preparing it found and did; nil on an edit from before

// On model.EditedFile.
BeforeMode, AfterMode string // Git's mode for the file on each side; empty where it is absent

// EditResult is what preparing one edit found and did.
type EditResult struct {
	Unattended    bool      // nobody was there when it was prepared
	Before, After string    // the port's version and revision on each side
	PatchesTried  bool      // the update tried the port's patches against the new source
	Findings      []Finding
	Actions       []Outcome // #195: each action asked beside the version step
}

type Finding struct {
	Kind    string   // a stable name: patch-rejected, plain-http, ...
	Subject string   // the patch, the URL, the repository or the pull request it is about
	Fixed   bool     // this edit dealt with it; otherwise it is left
	Detail  []string // the tool's own lines: the hunks that failed, a comment quoted
}

// On model.Branch: what was asked of this branch's work.
Request *ChangeRequest

type ChangeRequest struct {
	Origin  string       // update, bump, ship or serve
	Targets []Target     // #194 saves these; the rest is #195's
	Shared  Asked[bool]  // --shared-release
	Source  SourceAsk    // --source, --keep-http, --keep-old-checksums, --keep-modeline, --follow-moves
	Actions ActionsAsk   // --with-obsolete, --revbump-dependents with --except, --closes
	To      Asked[Stage] // --to: commit, check or submit
	Check   CheckAsk     // --on, --build, --tests, as asked
	Publish PublishAsk   // --draft, --type, --skip-notification
}

// Target is one port of the branch's work.
type Target struct {
	Port    string
	Action  string         // update, or stub for an obsolete stub that follows
	Named   string         // the version the person named; empty for the newest
	Release *model.Release // the release selected for it, once it is; kept from then on
	Current string         // the version it was found at, where nothing newer was selected
}

// Asked is a flag with whether it was given.
type Asked[T any] struct {
	Given bool
	Value T
}

// Outcome is what became of one action of a request.
type Outcome struct {
	Action string // version, source, obsolete, moves, dependents, closes
	Result string // done, satisfied, refused, kept
	Why    string
}
```

- **The preparation result is two fields on the edit**, one JSON column and one time, and two modes on each of its files. There is no second table. [#194](https://github.com/herbygillot/dockhand/issues/194) says the result is recorded "with its edit", and the statement's limit is that the edit and `Preparation` are extended.
- **An edit with `Written` zero is not yet an edit to anyone but the settler.** `Reader.Edits` (internal/store/store.go:198) returns written edits only, so its nine call sites in the engine, tidy's among them, see what they see today. One new reader returns a branch's unwritten edits. One new write removes an unwritten edit; no written edit is ever removed by it.
- **A finding is data, worded when it is shown.** A kind a reader doesn't know is shown by its name and detail. Quoted text (a maintainer's comment, a tool's refusal) stays as it was captured.
- **What is stored of `engine.Update`** (update.go:166-284), field by field:
  - Findings left: `PatchProblems` (patch-rejected, with the hunks), `PatchesUnchecked`, `MissingWorksrcdir`, `Instructions` (the comment quoted, with its line), `PlainHTTP`, `Others` and `ClosedBefore` (as seen then), `CrossesMajor`, `LeavesStable`, `Renamed`, `ChecksumsLeft`, `Stealth`, and each notice a regenerated block raises.
  - Findings fixed: `PatchesDropped`, `PatchesRefreshed`, `Followed`, `Regenerated`, `Fixed`, `DistSubdirRemoved`.
  - `PatchesTried` is true where `PatchesUnchecked` is empty. `PatchesApplied` is its count and is not kept apart.
  - As an outcome: `Obsolete`.
  - Already on the edit or the branch: `Release`, `Files`, `Subject`, `Upstream`, `HTTPS`, `Source`, `RenamedFrom`, `Closes`.
  - Not stored, because each is worked out from the records or belongs to another: `Diff`, `Started`, `Applied`, `Current`, `Setup`; `Distfiles`, `Compared` and `Differing`, which are the source comparison's ([#192](https://github.com/herbygillot/dockhand/issues/192)); `HandEdit` and `Left`, which the hand edit's own record keeps (`recordHandEdit`, update.go:1331).
- **A port found current has no edit and no result.** Its target says so (`Current`), so a continuation doesn't look again. **A port held before any edit** (a major version, another open pull request) has neither a branch nor a record: it is a fact about now, judged at each look and said by serve's pass, as today.
- **The request is one value for one branch's work**, in `model`, kept in one JSON column on the branch. A Portfile's ports are its targets. A batch is one request for each branch, made from one set of flags, and each stands or is refused apart.

## An edit is recorded before its files are written

The order of an update changes:

1. Prepare the edit into its tree. A plan returns here, as today.
2. Start the branch where the request says to, as today. The request's targets are saved with it, or before the first edit where the branch exists.
3. **Record the edit, unwritten**, in one transaction through `store.Recorded` with today's witness (`editRecorded`, update.go:1066): the edit with both blobs and both modes for each file, its result, the target's selected release, the tickets it closes, and the assessment and change record where the update can make them.
4. Write the files (`ApplyToWorkingFiles`).
5. **Record that they are written**: `Written`, and the event `branch.edit`.
6. Rename the branch for its version where that applies, as today.

- Everything in step 3 is made from the prepared tree, not from the working files. Today `editFindings` reads the working tree after the write (update.go:833) and `instructionLines` reads the Portfile from disk (:1371); both read the prepared tree instead. `editRecord` already builds its blobs from the prepared files (:936). So a later run can finish step 5 with no preparer and no download.
- Where step 3's outcome is unknown and the read back finds no edit, nothing was written and the command fails clean. Where step 4 refuses because a file changed while the edit was prepared (`ErrWorkingFile`), nothing was written: the run removes the edit it just recorded and says to run it again, as today. Where step 5 fails, the command says the files are written and the record isn't complete, and the next command completes it.
- A hand edit's attempt (`update --hand-edit`) records no edit of dockhand's, as today, and skips step 3. Its files are the person's, and so is what a failed record of it leaves.

**Settling.** Every command that reads a branch's edits first asks for its unwritten ones. There are normally none. For each, it reads the edit's files in the worktree, their presence, blob and mode, against the two sides recorded:

| The edit's files are | It means | update of that port, bump, ship; serve for its own branches | every other command |
| --- | --- | --- | --- |
| all as recorded after | they were written and step 5 was lost | settle: step 5 | the same; it is a store write only |
| all as before | never written, or put back | write them from the kept blobs, then settle | say it in a line and go on: the files hold nothing of it |
| some after, the rest before | the write was cut short | write the rest, then settle | say it and stop, naming `dockhand update <port>`; status says it and shows the rest |
| any one neither | someone changed it since | stop. Nothing is written. The message names each such file | the same. `tidy`, with the person looking, commits the files as theirs and removes the unwritten edit |

- An edit is settled whole or not at all. A file outside the edit's own list is not read here: a person's change to another file of the port leaves the edit to settle, and stays an uncommitted change that isn't dockhand's, which holds an unattended commit as today.
- A version named for its port replaces an unwritten edit whose files are all as before, and says so. Where some of them are written, the edit is finished first.
- "Nothing to change" can't be said over an unwritten edit: it is found before any release is looked for.
- **Where the kept blobs are gone.** An edit's After blobs are loose objects no ref keeps, as they are today. An unwritten edit left longer than Git keeps unreferenced objects can find them collected. With every file as before, the unwritten edit is removed and the port prepared again at its kept release, and that is said. With some files written, nothing is written and the run names the files and stops.
- **The lock.** From the moment the branch exists, steps 2 to 6 run under the lock history's transitions take, and so does settling. Each reads the branch and its edits after it has the lock. Step 1 runs outside it, since it downloads; step 4 checks again that each file is as step 1 captured it. The lock is by the branch's name and step 6 can rename the branch: the rename is made under the lock, and a process that then gets the old name's lock reads the branch again, finds another name, and takes that one.

## Readiness

`Preparation` takes more than it does today: the branch's written edits; its unwritten edits with what settling found; its uncommitted paths in two lists, those its edits explain (as tidy tells them) and the others; the request's targets; whether the caller is attended; and the allowances as they are now. It answers the first of these that applies, and returns every reason with it, and whether the edits are committed:

| Answer | When | ship, bump and serve | status |
| --- | --- | --- | --- |
| hand-edits | an uncommitted change that isn't dockhand's, or a file an unwritten edit can't be settled over | stop | as today |
| unfinished | an edit unwritten or partly written; a target with no edit that wasn't found current; dockhand's own edits written and not committed | finish it and ask again; serve only for its own branches | a line for the first two: "an update of jq wasn't finished; `dockhand ship jq` finishes it" |
| held | below | stop before the check, and say whether the edits are committed | held before its check, with what holds it |
| ready | | queue the check | as today |

- **A port is held** where the newest of its update edits that tried its patches (`PatchesTried`) left a finding of a kind that holds, today `patch-rejected`; that edit was prepared unattended; the caller is unattended; and the port's `holds.allow` doesn't name the kind now.
- **Ports aggregate by that edit**, so the order of updates doesn't matter. A clean sibling, a port found current, a revision bump and a checksum refresh change nothing of another port's hold or of this port's.
- **Nothing clears a hold, because none is stored.** What ends one: `holds.allow` naming it; a person's own ship, check or submit, which is told and goes on; an update of the port that tried its patches again. A newer release appearing does nothing. `Edit.Held`, `SetHeld` and `holdBeforeCheck` go.
- **Unfinished comes before held**, because serve commits held work and holds it at its check (ship.go:142-159). A verdict that answered held first would leave it uncommitted.
- status keeps its two sources, one for each kind: preparation's findings before a check, recorded assessments after (`assessedHolds`). One list is shown, and a branch held before its check says whether its edits are committed.

## The commit, and what recovery expects

- **Before an unattended commit**: nothing is unwritten, every target has a written edit or was found current, and no uncommitted change is anyone's but dockhand's. Then tidy plans, the commit gate judges and tidy applies, as today, leaving its checkpoint. No hold is written before it: the step #116 added goes with the stored hold.
- **Recovery** considers serve's open branches with no pull request and no run, as today, and at each of serve's daily looks as well as at its start. It continues a preparation where `serve.ship_to` is commit or further, and queues a check where it is check or further.
  - Unfinished: it settles, prepares the targets still without an edit at their kept releases, and commits.
  - Committed: it expects two things, and nothing uncommitted. The history is dockhand's: the branch's newest checkpoint is a tidy, applied and not restored, whose `After` is the branch's head. The content is dockhand's: every path that differs between the base and the head ends at the After blob of the newest written edit of that path, the chain tidy already follows to tell its own edits. The first catches a commit made with Git, a rebase and an undo. The second catches a person's change that a tidy of theirs committed. Then it asks `Preparation`, and where the answer is ready it proposes and queues the check under the settings of that moment.
  - Anything else is named and left: a commit dockhand didn't make, a commit of its own undone, a rebase, uncommitted changes that aren't its own. The line names `dockhand ship -b <branch>`.
- "Prepared, committed, check not yet queued" is therefore read from the checkpoint and the edits, and no longer inferred from four facts. No field is added to the checkpoint.
- A person's ship is not recovery. It takes a committed branch as it stands, under its own gates, as today.

## A preparation that stopped part-way

- The branch holds the request's targets and the edits made so far, written or not. Nothing is committed unattended until every target has a written edit or was found current.
- Whoever continues it settles first, then prepares each target that has no edit, with the release kept for it or a fresh look where none was selected yet, then goes on. The same command typed again continues it, and so does `ship` of the branch. serve continues its own branches at each daily look, before the outdated pass, which would otherwise skip the port as already in a branch. serve never continues a person's.
- A port whose update failed stays failed and is said. The others' edits stand.
- **A port's own refusal comes at its turn.** The resolver refuses what it can know before any edit (see "The flag check"). What only a port's preparation can know, such as a source move that port can't make, is refused when that port is prepared. For a single port that is before any file is written. For several ports of one Portfile the earlier ports' edits stand, nothing is committed, and the command says which port refused and why.

## The request

- **One resolver in the engine** builds it. It takes the flags with whether each was given, the settings, serve's configuration, the targets discovery found, and the request kept on the branch where there is one. It returns the request, the outcome of each action it can already judge, and each fact not resolved yet with its reason. Commands parse flags and ask questions; they build no request.
- **Resolved when it is built**: the targets, the branch, the stage, and the source policy from settings where no flag was given. **Carried as asked and resolved at its stage**: a target's release (at its preparation, then kept), the check's place and tests (when the check is proposed), the destination (at the submission's plan).
- A release that can't be selected stops an execution before any file. It is never kept as "the newest", to mean something else later.
- **What it replaces.** `VersionUpdate` stays the commands' flag set and feeds the resolver; its `Request` goes. `LinkedOptions`, `shipOptions` and `shipTarget` go into the request. `PrepareOptions` builds a request from settings. `UpdateRequest`, `WhereRequest`, `CheckRequest` and `SubmitRequest` stay the stages' own requests, built from the request by the engine and no longer by a command.
- **Primitives keep their literals**: `edit`, a checksum refresh, `revbump`, `create`, the obsolete stub's update and the dependents' revision bump. They go through the same `Engine.update`, so their files are recorded before they are written too. They read no request and change none.
- The existing refusals move into the resolver unchanged: `--except` with no `--revbump-dependents` (author.go:399), a ticket that isn't a number (:402), and a version named for several unrelated ports (`updateSeveral`, author.go:121).

## Each action has its own outcome

`ReleaseSelection.NoUpdate` stays the selector's fact. It stops deciding the whole request: the preparer no longer returns on it before the source request is read (`planArchiveVersion`, internal/macports/portedit/version.go:47), and the four exceptions written beside it go. The version is one action among those asked.

| Action | Asked by | Already so when | Shown by |
| --- | --- | --- | --- |
| version | every update | no newer release, or the named one is the current one | the Portfile's version |
| source | `--source` given | the Portfile already fetches that way | its fetch declaration |
| obsolete stub | `--with-obsolete` | the stub is at the port's version, moved with it, or there is none | the stub's version |
| moved repository | `--follow-moves`, or the port's `moves` | the Portfile names the repository GitHub answers with | `github.setup` |
| dependents | `--revbump-dependents` | none depend on it, all are excepted, or the branch already changes each (`LinkedPorts`, internal/engine/diff.go:505) | the edits in the branch |
| ticket | `--closes` | the branch already closes it | `Branch.Closes` |

- **Four outcomes**: done, satisfied, refused, kept. Kept is its own outcome: the flag was given to a command that stops before the stage that reads it, and it is on the branch's request and said.
- A lookup that failed is an error. It is never "already so".
- A plain update of a current port still says "already at 1.8.1; nothing to change" and starts no branch. The `update.source` setting alone asks nothing of a current port; `--source` given does (#121's own words: "a plain update of a current port stays the no-op it is").
- Where a side action has something to change and the version is current, the branch is started for it.
- `--closes` with nothing else to change is refused by name: a ticket alone starts no branch. Today it is dropped with no word (update.go:633 returns before the tickets are joined).
- An outcome is written in the result of the edit that carried it out. An action that made no edit leaves no record: it is said by the command that judged it, and judged again by a continuation from what it would have changed.

## The flag check

- **The table is Go data in the engine**, one row a flag: the request's field, the commands that take it, the stage that reads it, what leaving it out means, what false or empty means, how long it lasts, and its outcomes. `docs/usage/flags.md`, a new page, shows the same table, and a test fails where the page and the data differ. The table as designed is under "The flag check's table".
- **The test that finds a flag with no row** extends the walk of the command tree that exists (`commands`, internal/command/contract_test.go:73). For update, bump, ship, submit, check, edit and revbump it visits every local and inherited flag. Each has a row, or a row that names the contract that owns it. A row for a flag that no longer exists fails too. No flag is excused by a prefix or by being unfamiliar. The request's fields are reflected the same way, so a field added without a row fails.
- **A row's truth is a behaviour test** for each of its outcomes, which looks at files, the record, a queued plan or the stand-in forge, never at two requests compared. Each row is tested left out against given false or empty, and across a restart where it is kept. Most rows run through the engine with the test preparer. The rows only the real preparer decides run with MacPorts' Tcl: the source moves and their refusals, the obsolete stub, a version equal to the current one or older, `--shared-release` and `--keep-old-checksums`. About six fixtures.
- **Refused before any edit**, by the resolver: `--except` alone; a version named for several unrelated ports; a ticket that isn't a number; a `--tests` value that isn't one of the three; and each pair of flags the commands refuse today. What a port's preparation alone can know is refused at that port's turn, as said above.
- **A stage that stops short** keeps every flag a later stage reads, and says each: `--title`, `--note`, `--tested-binaries`, `--tested-variants`, `--type`, `--draft`, `--skip-notification`, and `--on`, `--build` and `--tests` too. "Kept for the check: --on sequoia, --tests required. Kept for the submit: --draft, --type bugfix."

## What is kept, and what a later command reads

- **Saved** in the transaction that starts the branch or, on a branch that exists, before the command's first edit. update's is saved too, so that its stub and its dependents are finished after a stop.
- **Ended** when a publication of the branch finishes ([#191](https://github.com/herbygillot/dockhand/issues/191)), in that transaction, or with the branch. Until then a submission that failed part-way is still this request's work, and the next ship goes on with the same release. A branch with no request gets one from the next update, bump or ship, as a new command does today, with a fresh look for its release.
- **A flag given replaces its kept field and is said. A flag left out leaves it.** Nothing is refused for differing.
- **The release.** A ship or bump of a branch that has a request goes on with the release kept for each target. It says which and when it was selected, and it does not look for a newer one. A version named replaces the selection and is said. `update` with no version finishes what is unfinished at the kept release, and where nothing is unfinished it looks for the newest as it does today, and says what it replaces.
- **The stage** is ship's. A `--to` given is kept; a later ship without `--to` stays at it, and says so with the command that goes further. `bump` is `--to submit`, given. Where no `--to` was ever given nothing is kept and ship's default applies. `update` stops after its edits and neither reads nor changes a kept stage. `ship --passing` leaves a branch kept below submit where it is, and names it.
- **The check's flags and the submission's** are read by whoever next runs that stage for the branch: ship going on, or `check` or `submit` on its own. `check` reads each of `--on`, `--build` and `--tests` it wasn't given from the request, says which it took from there, and never changes the request. `submit` reads each of `--draft`, `--type` and `--skip-notification` it wasn't given the same way.
- The title, the note and the tested statements stay the branch's own fields, kept by `KeepForSubmit` as today (internal/engine/submit.go:1888). A tested statement already stands only for the files it was made about (#153).
- `--yes`, the branch selectors and the selections that make targets (`--mine`, `--maintainer`, `--category`, `--not-maintainer`, `--passing`) belong to the command that was typed. None is kept, and a continuation never runs a selection again to add ports.

## serve

- "Each new action" is each stage of each branch: its preparation, the queueing of its check, its submission. Each reads the process's configuration then. Nothing of the configuration is kept on the branch, so serve's request holds its origin and its targets and nothing else.
- Read at preparation: the source, HTTP and modeline settings, the port's `moves`, and the holds judged before an edit. At the check: `serve.ship_to`, `holds.allow` through the verdict, `build.unattended`, and `check.tests` through `ProposeCheck`. At submission: `serve.ship_to`, the holds and the limit.
- A branch prepared under one configuration gets the configuration of the process that queues its check, at recovery too. A check it queued keeps its plan, and a publication it recorded keeps its intent; each is read from its own record, with no copy in the request.
- Configuration is read when the process starts, so a changed setting counts from serve's next start. This design adds no reading of it mid-run.
- What serve can't be asked for stays that way: an obsolete stub, dependents, a ticket.

## Where the request meets its neighbours

- **The publication record (#191).** The request's `Publish` moves into `Publication.Asked` in the publication's first transaction: each field unless the submit's own flags say otherwise, and it is emptied in the request in that transaction. From there #191's rules decide alone: for the same commit and destination a later submit keeps each choice unless that run says otherwise, and `--rewrite-description` is never carried. No copy stays in the request to disagree with it. The rest of the request ends when that publication finishes.
- **The check's lifecycle ([#193](https://github.com/herbygillot/dockhand/issues/193)).** The place and the tests are resolved by `ProposeCheck`. Whether a check that exists already covers the one asked is admission's answer, as today. ship's stay with its check is #193's `Stay`, through `See`. A cancelled check never counts as the check the request asked for.
- **The source chooser ([#16](https://github.com/herbygillot/dockhand/issues/16)).** Its typed refusal is the source action's `refused`, with its reason. Its ambiguity is a question where a person is there, and a refusal that names the candidates where nobody is.
- **The review question ([#162](https://github.com/herbygillot/dockhand/issues/162)).** Its answer is a decision's record. No request holds it, and a later process asks again under #162.
- **The two drivers ([#43](https://github.com/herbygillot/dockhand/issues/43)).** Both read the same value and call the same engine functions with requests built from it. They keep their scheduling, their questions and their output.
- **The boundary test** reads the syntax tree. It forbids a composite literal of `ChangeRequest` outside the resolver, the store's decoding and tests, and one of `UpdateRequest`, `WhereRequest`, `CheckRequest` or `SubmitRequest` in a command or a driver, except a named list: the checksum refresh (author.go:301), `revbump` (internal/command/author/edit.go:136), the stub (update.go:346), `create` (internal/engine/create.go:269), the dependents' bump (diff.go:607), and `check` and `submit` on their own. Beside it, one test for each of update, bump, ship and serve shows the command reaches the resolver.

## `ship --plan`

It shows the value: each target with its release or "not resolved now" and why; the side actions with what is already so; the stage, kept or asked; the place as asked or "decided when the check is queued"; the destination "decided at submit"; the holds known now; and what is kept from an earlier command. An unwritten edit is shown as it would be settled. A plan saves no request, starts no branch, records no edit and writes no file. JSON carries the same fields.

## Existing databases and branches

- **One schema step for each issue.** #194's adds `written` and `result` to edits, the two modes to an edit's files, and `request` to branches. #195's changes no table, since the request's other fields are in the same column. It still raises the schema's number: a dockhand built before it would drop those fields when it saved a branch, and must not open the database.
- **Both wait for quiet the way #191's does**, by its amended page: the upgrade is refused while another process named dockhand is running. An earlier update paused between its files and its record would otherwise write files no record explains, once, at the upgrade. This design adds no test of its own for it.
- **An edit from before** gets `Written` set to its own time, which is true: its files were written. Its `Held` becomes a `patch-rejected` finding, left, tried, prepared unattended, with the held lines as its detail, and the column goes. Nothing else is worked out: it has no other result, its files have no modes, and the notices list says nothing for it.
- **A branch from before** has no request. A committed one is judged by recovery as any other, since its tidy left a checkpoint. One whose edits are written and not committed has no targets on record: serve says so and leaves it, and `dockhand ship -b` finishes it.
- `undo` restores a tidy or a rebase and deletes no edit: the edits are uncommitted again and keep their results. `clean` removes a branch's edits with the branch, unwritten ones included, under its own gates. Nothing of these records must outlive the branch.

## Corrections to the statement

| | The statement said | The code says | Found by |
| --- | --- | --- | --- |
| 1 | serve's check has no test policy | an empty policy takes the configured one (plan.go:67) | Codex |
| 2 | #198 was being fixed | it has landed (internal/command/bump/bump.go:90) | Codex |
| 3 | `--except` alone and a version named for several ports are unsettled | both are refused today (author.go:399, :121) | Codex |
| 4 | a table of flags | a starting list: update also takes `--yes`; ship `--port`, `--pr` and `--over-allowance`; check eleven more; the checksum refresh `--no-revbump`; edit `--no-open` | Codex |
| 5 | moving the transaction is enough | the assessment and the instruction lines are read from the working files after the write (update.go:833, :1371) | Codex |
| 6 | the blobs are enough to write from | the write also checks modes (working.go:129), and `EditedFile` has none | Codex |
| 7 | `undo` of a recorded edit | there is none: undo restores a tidy or a rebase | Codex |
| 8 | nothing on the upgrade | it needs every earlier dockhand stopped, and a session scan can't show that | Codex |
| 9 | status doesn't ask `Preparation` | it does, through `Attend` (internal/branchstate/attention.go:204) | architecture |
| 10 | the record could be named `Prepared` | `branchstate.Prepared` is the verdict's type | both |
| 11 | no case for a branch of serve's written and never committed | nothing takes it up again (recovery.go:65, outdated.go:442) | architecture |

## Where the two proposals differed

| | Architecture | Codex | Taken, and what decided it |
| --- | --- | --- | --- |
| 1. Where the result lives | two fields on the edit | tables of groups and results | The edit. #194's words, and the limit on new records |
| 2. An edit not yet written | in `edits`, `Written` zero | a draft, promoted to an edit | One row with `Written` zero, hidden from `Edits`: Codex's rule that it authorizes nothing, in architecture's place |
| 3. Modes and deletions | blobs only | the two trees kept | The two modes on each file. Codex's catch (working.go:129); a tree would need a ref to keep it |
| 4. What step 3 is made from | the assessment after the write | everything from the prepared tree | Codex's (update.go:833, :1371). A later run then needs no preparer |
| 5. Keeping the objects | not raised | private refs, removed when proved orphan | None. An edit's blobs are unkept today (update.go:936); the rule for collected blobs is in "Settling" |
| 6. A port current, or held before an edit | no record | a result row for each, shown by status | `Target.Current` for the first; nothing for the second. #194 records a result with its edit |
| 7. The members of a preparation | the request's targets, from #195 | a group, from #194 | The targets, on the branch, saved from #194. Codex's timing in architecture's place |
| 8. Who completes a record | the authoring commands | any command, where the files are all written | Codex's. It is a store write, as #191's settling is |
| 9. Files all as before, plain update | remove the edit, prepare again | write what was kept | Codex's: one rule for every command |
| 10. A person's change to another file | stop | settle the edit, keep the change apart | Codex's. The edit's own files decide |
| 11. The new answer | unfinished | incomplete, after held | `unfinished`, before held (ship.go:142-159). Every reason returned, and whether committed, are Codex's |
| 12. What ends a hold | a new edit of the port | new evidence that covers it | Codex's: the last update that tried the patches. Today a revision bump lifts it (preparation.go:45-52) |
| 13. What recovery expects | every changed path explained by the edits | the exact checkpoint, with new fields on it | Both, and no new field: the checkpoint as it is (history.go:306) for the history, the edits for the content |
| 14. Committed branches from before | today's four facts | never queued unattended | Neither. The same test as any branch |
| 15. When serve continues its own | at its start | at each pass | Codex's: each daily look, before the outdated pass |
| 16. Several ports, one refusing | at its turn | all prepared in scratch first | At its turn, and said so. The loop stays as it is |
| 17. The hand edit's attempt | as today | recorded before its files too | As today. A lost record leaves files called the person's, which they are |
| 18. update's request | not kept | kept for its side edits | Kept, with its targets |
| 19. The request's identity | a value on the branch | a row with a generation, and references to the check and the publication | The value, under the branch's lock. Each neighbour is read from its own record |
| 20. Check flags with `--to commit` | refused | kept and said | Kept. One rule for every later-stage flag; choice 1 |
| 21. A flag with nothing to do | refused for a named port | already so, and said | Codex's. Today it is silent (update.go:329), and a refusal would break a command that works; choice 2 |
| 22. `check` alone, against kept flags | reads them | its own, reads none | It reads each it wasn't given and changes nothing |
| 23. The stage | any flag left out keeps | kept while unfinished | A `--to` given is kept. update has none to keep |
| 24. Outcomes | four | five, with held | Four. An ambiguity is a question or a refusal |
| 25. Source on a current port | flag or setting | flag, and the setting where it names a mode | The flag only (#121's words) |
| 26. serve's settings | the process's | a snapshot read again at each action | The process's (configuration.md:258) |
| 27. What a plan may touch | no request, branch, edit or file | not the store or a ref either | Architecture's. The rest is under "What this does not do" |
| 28. Where a person reads the table | a usage page | this file, checked against the code | The usage page. A design is not kept equal to the code |
| 29. serve's test policy | a choice for the person | already read (plan.go:67) | Dropped |
| 30. A tested statement and changed files | not raised | must lapse | Already so (#153; submit.go:1889) |

## The cases

"Settle" is the table under "Settling". The letters are the statement's; Y to AD are the cases Codex added.

| | The store then | The next command, and what it does | The person is told |
| --- | --- | --- | --- |
| A. `update jq` writes the Portfile and step 5 fails | the edit, unwritten, with its result | update, ship, tidy, status, serve for its own: settle | "finished the update to 1.8.1 an earlier run began" |
| B. As A, then (i) the person edits that Portfile, (ii) another file of the port, (iii) a newer release is out | as A | (i) neither: stop. (ii) settle; the other file holds an unattended commit. (iii) `update jq` settles 1.8.1, then looks and goes on to the newer one | which file, and the two ways on. For (iii), both steps |
| C. Killed around the write | no edit; the edit and no file; some files; all files; the edit written | nothing to finish; write and settle; write the rest; settle; stands | |
| D. Three ports under serve, one with a patch rejected, in every order | three targets; two edits, one with the finding; one target current | held in every order: the finding is on its own port's edit | the port, the patch, `holds.allow`, and that the edits are committed |
| E. Two ports, the second's update fails | two targets, one edit | the same command or serve's next look prepares the second again; nothing is committed meanwhile | the second's failure |
| F. Killed after the last edit; after the commit | edits written; then the checkpoint | recovery commits and queues; queues. No hold is written between | |
| G. Recovery finds serve's branch with commits and no check | | (i) its own commit: queue. (ii) a commit on top, (iii) other uncommitted changes, (v) a rebase: named and left. (iv) held: stays held. (vi) from before: as (i) to (v) | what it found, and `ship -b` |
| H. A patch is rejected, then | the finding, left | (i) `holds.allow` gains it: ready. (ii) the person ships: told, goes on. (iii) update again: the new edit decides. (iv) a newer release: nothing | |
| I. A notice and failed hunks under `ship`, one port and three | findings on each edit | ship lists them from the records as each stage passes, and after a restart | a line each, by port; what holds apart from what informs |
| J. A port held before any edit | nothing | the pass says it again | as today |
| K. `undo`; `clean` | | the edits stay, uncommitted, with their results; both go with the branch | |
| L. Edits from before | `Written` set, no result; `Held` as a finding | status and ship list nothing for them but the hold | |
| M. A current port with `--source` | | done: a source-only edit. Already so. Refused with the chooser's reason | each by name; the version said to be current |
| N. A current port with `--with-obsolete` | | the stub moves; already at the version; no stub: already so, said | which |
| O. Two or three of M to O at once | | each action its own outcome, in the edit's result | a line each |
| P. A version named, equal or older | | equal: satisfied, and the other actions judged. Older: as today | |
| Q. `ship --to commit` or `--to check` with every later flag | the request, with each | kept and said. The later `ship`, `check` or `submit` reads them | "kept for the check: … kept for the submit: …" |
| R. `bump foo --tested-binaries=false` | the branch's statement | withdrawn, as landed (#198); its row stays | |
| S. `ship foo --draft --type bugfix` killed at four points; a newer release is out | the request: 1.9, draft, bugfix | every later ship, check and submit goes on with 1.9 and the kept flags. serve never takes it | which release it goes on with |
| T. A new command against it | | `--type enhancement` replaces bugfix. `update foo 2.0` replaces 1.9 and stops after its edit. `--to commit` is kept from then | each replacement |
| U. serve's settings change between stages | | the next stage serve starts after its restart reads them; a queued check and a recorded publication keep theirs | |
| V. `ship --plan` | nothing saved | "not resolved now", a row for each port, the kept flags shown | |
| W. Combinations | | `--except` alone, a version for several ports: refused, as today. No stub, no sibling, no dependent: already so, said. `--to commit --tests required`: kept | |
| X. `edit`, a checksum refresh, `revbump` on a branch with a request | | each settles first, acts, and leaves the request alone | |
| Y. Two commands on one branch | | the second waits for the lock and reads again | |
| Z. A mode change, a deletion, an untracked file in the way | modes on each file | presence, blob and mode are compared; the untracked file is "neither" | which file |
| AA. Killed between two dependents' bumps | the first's edit | the dependents are looked up again; one the branch already changes is left out | |
| AB. A port found current, or a revision bump, after a held update | the finding, on the update's edit | still held | |
| AC. Git collected an unwritten edit's blobs | the edit, unwritten | all as before: removed and prepared again. Partly written: stop | which |
| AD. A kept `--type` and a publication's for the same commit | the request's `Publish` moved into the publication when it was recorded | the publication's stands, by #191's rule; only a flag given now changes it | |

## The flag check's table

U is update, B bump, S ship, Sub submit, C check, Sv serve. A star marks a row whose outcome is one of the person's choices.

| Flag | Field | Taken by | Read at | Left out | False or empty | Lasts | Outcomes |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `[version]` | `Targets[].Named` | U, B; S through a pinned target | edit | the newest release, or the one kept | | with the target's release, on the request | done, satisfied, refused |
| `--shared-release` | `Shared` | U, B, S | edit | each port its own release | as left out | the request | done; satisfied* with no sibling |
| `--keep-old-checksums` | `Source` | U, B, S | edit | false | as left out | the request | done, satisfied |
| `--keep-modeline` | `Source` | U, B, S; Sv by setting | edit | the setting | false, over a setting or a kept true | the request | done, satisfied |
| `--keep-http` | `Source` | U, B, S; Sv by setting | edit | `update.keep_http` | false, over the setting | the request; the edit's `HTTPS` | done, satisfied |
| `--source` | `Source` | U, B, S; Sv by setting | edit | `update.source`, applied when a version edit is made | | the request; the edit's `Source` | done, satisfied, refused (#121, #16) |
| `--follow-moves` | `Source` | U, B, S; Sv by the port's `moves` | edit | the port's `moves` | don't follow | the request | done, satisfied |
| `--with-obsolete` | `Actions` | U, B, S | edit | the stub is left, and said to lag | as left out | the request | done; satisfied* with no stub |
| `--revbump-dependents` | `Actions` | U, B, S | edit | none bumped | as left out | the request | done; satisfied* with none |
| `--except` | `Actions` | U, B, S | edit | none left out | as left out | the request; the edit's `Excepted` | done; refused alone, as today |
| `--closes` | `Actions` | U, B, S | edit | none | | the branch's `Closes` | done, satisfied; refused* with nothing else to change |
| `--hand-edit` | none: the attempt's own | U | edit | | | the hand edit's record | as today |
| `--branch`, `--new`, `--port`, `--pr` | none: they choose the branch | U, S, edit, revbump | before the request | the port's branch, started where none is | | the command | done, refused |
| `--mine`, `--maintainer`, `--category`, `--not-maintainer`, `--passing` | none: they make targets | U, S | before the request | | | the command | targets, or none |
| `--over-allowance` | none: the lookup's | U, S | before the request | false | | the command | as today |
| `--to` | `To` | S; B as `--to submit` | every stage | what is kept, else submit | | the request, where given | done; kept; replaced by a later one given |
| `--yes` | none: the caller's | U, S; B fixed | every stage | a person answers | | the command | as today |
| `--on`, `--build`, `--tests` | `Check` | B, S, C | the check | what is kept, else the settings of that moment | | the request | done; kept* below the check |
| `--draft`, `--type`, `--skip-notification` | `Publish` | B, S, Sub | submit | what is kept, else not a draft, the type worked out, notified | false, over a kept true | the request, then the publication | done, kept |
| `--title`, `--note` | the branch's own | B, S, Sub | submit | what the branch holds, else worked out | empty clears it | the branch | done, kept |
| `--tested-binaries`, `--tested-variants` | the branch's own | B, S, Sub | submit | what the branch holds for these files | false withdraws it | the branch | done, kept |
| `--ready`, `--accept`, `--standing`, `--no-check`, `--head`, `--remote`, `--rewrite-description` | submit's own | Sub | submit | | | #191's | #191's |
| `--fresh`, `--only`, `--also`, `--variants`, `--keep`, `--head`, `--staged`, `--working-tree`, `--include`, `--replace`, `--baseline`, `--of`, `--enqueue`, `--for` | check's own | C | the check | | | #193's | #193's |
| `--plan` | none | U, B, S, C, Sub | | | | | writes nothing |
| `--subject`, `--no-revbump`, `--no-open` | a primitive's own | revbump, the checksum refresh, edit | | | | | as today |
| `--tree`, `--db`, `--tool`, the output flags | settings' | all | | | | | settings' |

Where "as today" stands in a cell, what the code does there was not read for this file. The test, not this table, is what finds a flag that is missing.

## Choices the person should see

Each is something a person will meet. The recommendation is the design as written.

1. **A flag for a later stage is kept, and said, where the command stops before that stage.** `ship --to commit --on sequoia --tests required --draft` keeps all three; the `ship`, `check` or `submit` that comes later reads what it isn't given. Today the check's and three of the submission's are dropped with no word. The other way refuses the check's flags with `--to commit`.
2. **A flag with nothing to do is said, and the command goes on**: `--with-obsolete` on a port with no stub, `--shared-release` on a port with no sibling, `--revbump-dependents` where nothing depends on it. `--except` alone, and a version named for several ports, stay refused. The other way refuses the first three for a named port, which stops commands that work today.
3. **`--closes` with nothing else to change is refused.** A ticket alone starts no branch. Today it is dropped unsaid.
4. **A ship or bump goes on with the release it selected**, says which, and doesn't look for a newer one. A `--to` given is kept until another is given. `update` is how a newer release is taken, and it says what it replaces. The cost: `bump foo` on a branch kept at 1.9 submits 1.9 though 2.0 is out, and says so.
5. **`--source releases` and `--source archive` are carried out on a port already at its newest version.** The `update.source` setting alone is not: a plain update of a current port stays "nothing to change".
6. **`ship --yes` and `bump` are held before their check for a patch that no longer applies, as serve is**, and as `--yes`'s help says. `holds.allow` lets it build, and a person's plain `ship` is told and goes on. Today only serve is held.
7. **A revision bump or a checksum refresh no longer lifts that hold.** Only an update of the port that tried its patches again does.
8. **serve leaves a branch of its own that a person has changed**: a commit on top, a rebase, an undone commit. It names it and `dockhand ship -b`. Today it queues the check if the branch has commits and no check.
9. **serve takes up its own unfinished preparations at each daily look**, where today a branch written and never committed is never taken up again.

## What this does not do

- It records nothing for a port held before any edit, and adds no general undo of an edit.
- It keeps no Git ref for an unwritten edit's files. Where they were collected the rule under "Settling" applies.
- It prepares the ports of a Portfile in turn, as today, and does not prepare all of them apart first. A port's own refusal can therefore follow an earlier port's edit.
- It doesn't make serve read its configuration again while it runs.
- It leaves what a plan does today besides: fetching master, and what opening the engine writes. A plan that touches neither the store nor a ref would be a change of its own.
- It leaves the hand edit's attempt as it is, and the two ship drivers two (#43).
- The second part of #180, the table of code-generating crates, is not here.

## Tests

- **Settling**, one table: an edit and each state of its files, with modes, a deletion and an untracked file in the way, against what is written, what is recorded and what is said. With #194's failing case written first: the files written, the record lost, and a second update that says nothing to change.
- **`Preparation`**, one table: the three ports of D in each order; a revision bump after a held update; a port found current after one; the allowance given and taken away; attended and not; committed and not.
- **Recovery**: G's six, and an unfinished branch of serve's at a daily look, each asserting what is queued and that nothing is overwritten.
- **The flag check**: the walk; a row for a flag that is gone; each row's outcomes; the Tcl fixtures for #121's three, the rows #156 and #169 left, and P.
- **The request kept**: S at each of its four points and each next command; T; Q through the later `ship`, `check` and `submit`; the handoff to a publication, with a submit that gives a flag and one that gives none; AD.
- **The boundary test** and the four tests that each command reaches the resolver.
- **The upgrade**, against a database made by the code before it: edits with and without `Held`, a committed branch of serve's, and one written and never committed.
- **The fault sweep** ([#129](https://github.com/herbygillot/dockhand/issues/129)): a failpoint after step 3, after each file written, after step 5, after the targets are saved, at `ship.committed`, after the check is queued, after `Publish` moves at the handoff, and after the request ends; with the store's two unknown outcomes at steps 3 and 5. A second run after any of them ends as the clean run does, with one edit for each port and no hand edit said. Where a continuation rightly stops for a person, the scenario declares the stop and its sentence, and asserts nothing was overwritten.
- The regressions of #116, #156, #169 and #198 stay.

## How it is built

Each step is green on its own. A schema step lands with the readers and writers that need it, never ahead of them. [#125](https://github.com/herbygillot/dockhand/issues/125) lands before step 1: "kept this context's checksums" is one of the findings the result records.

1. **#194, the pieces**, with no caller: the fields and their store operations; the function from an edit and its files' state to what settling does; the findings mapped from `Update`; `Preparation`'s new inputs and its fourth answer. Unit and store tests.
2. **#194, switched on**: the schema step, behind #191's wait for quiet; the update's new order; settling in every reader; the targets saved by the three callers that have them today; `Held`, `SetHeld` and `holdBeforeCheck` gone; recovery by the checkpoint, at each look; the notices list (#180's first part); its failpoints; the usage pages. **#194 closes here.**
3. **#195, the pieces**, with no caller: the request's other fields, the resolver, the stages' requests built from it, the table, and the walk.
4. **#195, the real preparer** plans the actions after a current version, with #16's selector. The Tcl fixtures. No command reaches it yet.
5. **#195, switched on**: update, bump, ship and serve through the resolver; the request kept and read; `check` and `submit` reading it; the handoff to the publication; `ship --plan` from the value; the old builders and the four exceptions gone; the boundary test; the usage pages. **#121 and #195 close here.**

## Not checked

- Read, not run: everything in "Today". No test was run for this file. Codex ran four existing tests and one probe of its own for its proposal, at 2bdf27a06: the probe showed an update whose record fails leaves files that the second update calls current and tidy calls a hand edit.
- That the working tree after the write always equals the prepared tree, so that an assessment made from the prepared tree is the one `editFindings` makes today.
- That `ApplyToWorkingFiles` can be given a subset of an edit's files, and that a blob reader exists for tidy's use that settling can share.
- That every unattended commit leaves a tidy checkpoint, a first commit on a new branch included, and that tidy's chain from the base through an edit's blobs (internal/engine/tidy.go) can be asked of a commit as it is of working files.
- Which of #191's ways of ending a publication count as finished for the request. This file takes it to be the one where every step was done; a pull request found merged or closed ends the request too.
- Where the notice for a new `-sys` crate is raised, and so which field carries it.
- What `--shared-release` does on a port with no sibling today, and what `--keep-old-checksums` does where no checksum changes.
- How the edits' files are stored, and so whether the two modes are a column or a field of what is there.
- #16 and #162 are not built. This design leans on what their issues say they return.
- Whether a process that waited on a renamed branch's old lock can always tell the branch was renamed.
- How admission decides that a check which exists covers the one asked, which the kept `--on`, `--build` and `--tests` lean on.
- Which pairs of flags the commands refuse today, beyond the three read for this file.

## Usage docs

- `docs/usage/flags.md`, new: the flag check's table, kept equal to the code by a test.
- `docs/usage/branches.md`: an update that was cut short and how the next command finishes it; `--source` and the side actions on a port already current; a flag with nothing to do; `--closes` alone.
- `docs/usage/batches.md`: what ship keeps from one run to the next; the release it goes on with; `--to` kept; the flags kept for a later stage; the notices listed as stages pass; `ship --plan`'s unresolved lines; `--passing` and a branch kept below submit.
- `docs/usage/checks.md`: `check` reads the place and tests kept on the branch where it is given none; the hold before a check for `ship --yes` and `bump`.
- `docs/usage/publishing.md`: `submit` reads `--draft`, `--type` and `--skip-notification` kept on the branch; where they pass to the submission.
- `docs/usage/recovery.md`: files written with their record lost, partly written, or changed in between, and the message for each; what serve takes up again and what it leaves.
- `docs/usage/unattended.md`: serve continues its own unfinished preparations at each look; a branch a person changed is left and named; a hold follows the last update that tried the patches; settings count from serve's next start.
- `docs/usage/hand-edits.md`: the `--source` paragraph, as #121 asks.
- `docs/architecture.md`: the edit's two records, settling, the verdict's inputs, the request and its resolver.
- Messages name the port, the release, the file or the flag, and the command to run next. None names a state of these records.
