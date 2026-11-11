# The preparation result and the change request

Two core items for v0.9.0, [#194](https://github.com/herbygillot/dockhand/issues/194) and [#195](https://github.com/herbygillot/dockhand/issues/195). The issues say where they stand; this file is the design of both. Architecture merged it on 2026-11-11 from two proposals written apart from one problem statement, architecture's and Codex's (the person's "I'd like Codex to co-design and help review as well", 2026-11-11 05:48Z, in the architecture thread). Codex's was written at main 2bdf27a06, and the merge was read against main ab0e07431. "Where the two proposals differed" lists each difference and what decided it. Codex then reviewed the merge against main 3e091339b and said to revise it before adoption. It accepts the records as they are here, with no tables of their own, no Git refs and no generation on every edit, and asks nine changes. Each was read against the code, each holds, and each is taken; "What the review changed" lists them. Codex then read the revision at b4f9e1e3a in a second pass against main fce0515a0. It found seven of the points answered and asked four more changes, in the patch list, the request's ID, a pending tested statement and the rename, and two rules for the saved outcomes. Each was read against the code and is taken; "The second pass" lists them. Its last look, at d1e792162 against main 23b41575a, found the storage and most of that answered and five boundaries still open: what a patch's result on one system shows of another, which request a publication takes, a tested statement across other edits, what an outcome was judged by, and the ways to a branch that pass `Resolve` by. Each was read against the code and is taken; "The last look" lists them. Asked to confirm those five, it confirmed three and the hand-over, and corrected two things within them: how a request ends where its work is found already delivered, and what a flag changed after its edit may be called. Both are taken, under "The confirmation". This is the page revised for all of it. The person adopted it ("Adopt", 2026-11-11 15:51Z, on the architecture thread's card). Codex had not then read back its last two corrections as they are folded in here: anything it breaks in them comes back to the person as an amendment to this page.

## In short

- Today an update writes a port's files and then records the edit. Where the record fails, the next run finds the files at the new version, says "nothing to change", and tidy calls them a hand edit. Most of what an update found is printed once and gone. What a ship was asked is held by its process alone, so a ship started again looks for a release again and forgets how far it was to go.
- **The edit is recorded before its files are written**, with what preparing it found. A second, small record says the files were written. A run that finds the first without the second reads the files: all written, it completes the record; not written, it writes them from what was kept; changed by someone, it stops and overwrites nothing. dockhand's own edit is never called a hand edit again.
- **What an update found is kept with its edit**: each finding with what was done about it. ship lists the notices its updates raised from these records, after a restart too (the first part of [#180](https://github.com/herbygillot/dockhand/issues/180)).
- **A hold before the check is read, not stored.** One verdict, `branchstate.Preparation`, answers for ship, bump, serve and status from the same records. It gains one answer, unfinished. A patch that no longer applies holds the port until an update of that port, on the same system, has checked that patch again and found it applies, or no longer declares it. A later revision bump no longer lifts it, and where no such update can be had the person ends it.
- **Recovery expects exactly what dockhand committed**: the branch's head is the commit its own tidy made, every changed file follows from its edits by the test tidy itself uses, and nothing is uncommitted. Anything else is named and left for the person.
- **One request** holds what was asked of a branch's work: the ports and the release selected for each, the source and side actions, how far to go, and what was asked of the check and the submission. One resolver builds it for update, bump, ship and serve. It is kept on the branch from the first edit until the submission that took it has finished.
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
- Three writers besides the update record an edit, each after its files are written: `create`, the move of a created port, and a port's rename or move (internal/engine/create.go:257, :387; internal/engine/portrename.go:252).
- A rename for a new version moves the Git branch and then saves the name, and its witness shows only that the branch's row exists (`renameForVersion`, internal/engine/branches.go:1373-1393; `branchRecorded`, :237).
- ship keeps a title, a note and the tested statements only after its commit, and only where it stops before submit (internal/command/ship/ship.go:758-768). A ship that goes to submit holds them in memory.

## The records

```go
// On model.Edit, stored on the edits row.
Written time.Time   // when its files were seen written; zero until then
Result  *EditResult // what preparing it found and did; nil on an edit from before

// On model.EditedFile.
BeforeMode, AfterMode string // Git's mode for the file on each side; empty where it is absent

// EditResult is what preparing one edit found and did.
type EditResult struct {
	Before, After string      // the port's version and revision on each side
	Patches       *PatchCheck // nil where the final declaration wasn't read
	Findings      []Finding
}

// PatchCheck is the port's patches as the prepared Portfile declares them,
// after anything this edit dropped or refreshed, with what the check found.
type PatchCheck struct {
	Scope    string        // the evaluation it was read in; lists compare only where equal
	Declared []PatchResult // every patch declared there; empty where none is
}

type PatchResult struct {
	Name  string
	State string // applied, rejected, unchecked
}

type Finding struct {
	Kind    string   // a stable name: patch-rejected, plain-http, ...
	Subject string   // the patch, the URL, the repository or the pull request it is about
	Fixed   bool     // this edit dealt with it; otherwise it is left
	Detail  []string // the tool's own lines: the hunks that failed, a comment quoted
}

// On model.Branch.
Request  *ChangeRequest // what was asked of this branch's work
RenameTo string         // the name a rename is moving the branch to; empty but while one is under way

type ChangeRequest struct {
	ID       string       // new whenever what was asked changes; a publication keeps the one it took
	Origin   string       // update, bump, ship or serve
	Targets  []Target     // #194 saves these; the rest is #195's
	Shared   Asked[bool]  // --shared-release
	Source   SourceAsk    // --source, --keep-http, --keep-old-checksums, --keep-modeline, --follow-moves
	Actions  ActionsAsk   // --with-obsolete, --revbump-dependents with --except, --closes
	Outcomes []Outcome    // the dependents and the ticket, each once it is concluded
	To       Asked[Stage] // --to: commit, check or submit
	Check    CheckAsk     // the place, --on or --build, as one choice; and --tests
	Publish  PublishAsk   // --draft, --type, --skip-notification; the tested statements as asked, until they are bound
}

// Target is one port of the branch's work.
type Target struct {
	Port     string
	Action   string         // update, or stub for an obsolete stub that follows
	Named    string         // the version the person named; empty for the newest
	Release  *model.Release // the release selected for it, saved when it is selected
	Current  string         // the version it was found at, where nothing newer was selected
	Outcomes []Outcome      // each action asked of this port, once it is concluded
}

// Asked is a flag with whether it was given.
type Asked[T any] struct {
	Given bool
	Value T
}

// Outcome is what became of one action of a request.
type Outcome struct {
	Action string // version, source, moves, obsolete, dependents, closes
	Result string // done, satisfied, refused (which blocks); kept is said of a flag and not stored
	Why    string
}
```

- **The preparation result is two fields on the edit**, one JSON column and one time, and two modes on each of its files. There is no second table. [#194](https://github.com/herbygillot/dockhand/issues/194) says the result is recorded "with its edit", and the statement's limit is that the edit and `Preparation` are extended.
- **An edit with `Written` zero is not yet an edit to anyone but the settler.** `Reader.Edits` (internal/store/store.go:198) returns written edits only, so its nine call sites in the engine, tidy's among them, see what they see today. Two new readers see the rest: one returns a branch's unwritten edits, and one returns a single edit by its ID, written or not, which is what the two witnesses read. One new write removes an unwritten edit; no written edit is ever removed by it.
- **Two ways to add an edit, and no third.** `AddUnwritten` is step 3's. `AddEdit` takes an edit whose files are already written and refuses one with `Written` zero, so a writer nobody changed fails in its own test and can't leave an applied edit hidden from every reader. The three other writers record their edit after their files and keep that order: `create`, the move of a created port, and a port's rename or move. Each sets `Written` to the edit's own time, which is true of it. They gain nothing from this design and lose nothing: a record lost after their files leaves what it leaves today ("What this does not do").
- **A finding is data, worded when it is shown.** A kind a reader doesn't know is shown by its name and detail. Quoted text (a maintainer's comment, a tool's refusal) stays as it was captured.
- **What is stored of `engine.Update`** (update.go:166-284), field by field:
  - Findings left: `PatchProblems` (patch-rejected, with the hunks), `PatchesUnchecked`, `MissingWorksrcdir`, `Instructions` (the comment quoted, with its line), `PlainHTTP`, `Others` and `ClosedBefore` (as seen then), `CrossesMajor`, `LeavesStable`, `Renamed`, `Stealth`, and `ChecksumsLeft` with `Differing`, the archives a checksum refresh found other than the Portfile says.
  - Findings fixed: `PatchesDropped`, `PatchesRefreshed`, `Followed`, `Regenerated`, `Fixed`, `DistSubdirRemoved`.
  - `Patches` is the port's declaration as the edit leaves it, and not the preparer's working list. That list (`editprep.Result.Patches`) is filled before a merged patch is dropped and keeps the dropped patch as one that doesn't apply (`checkPatches`, then `dropMergedPatches`, internal/macports/portedit/patches.go:64, :88-135; Codex's run of the drop fixture returned `fix.diff` so, after it was dropped). The saved list is made from the prepared Portfile's final evaluation: a dropped patch is absent, a refreshed one is applied (:162-180), and each of the rest is applied, rejected or unchecked.
    - It is nil wherever that final declaration wasn't read: the list was malformed, no port was evaluated, or the edit checked nothing, as an update that only follows a moved repository does (`followMoves`, update.go:754; Codex's probe). A declaration read and found empty is a list with nothing in it, and says the port declares no patch. Today all of these return alike (:28-47). Where the declaration was read and no archive could be checked, every entry is unchecked.
    - `Scope` names the evaluation the list was read in: the provenance a record already keeps of one (`model.EvaluatedUnder`, internal/macports/runtime.go:20: MacPorts' and Tcl's versions, the platform and the host), with the Darwin version in full, the developer tools the evaluator saw, the subport and the variants. A Portfile's conditions read these. A patch declared for one release of macOS is not in a list read on another, and a condition can turn on the minor version (`TestAModelledContextHasItsReleasesMinorVersion`, internal/macports/eval/installation_test.go:289). Two lists are compared only where their scopes are equal. Where they aren't, or one has none, nothing is concluded from one about the other.
    - **Equal scopes are the standard for comparing, and no proof.** A Portfile can test what no record names, a file or the environment, and two evaluations with equal scopes can then declare different patches. dockhand keeps no record of what a Portfile's Tcl read, and this design adds none. `EvaluatedUnder` keeps the two versions, the platform and the host today (internal/model/change.go:12); the Darwin version in full and the tools are added to it. A scope with a part the evaluator couldn't name is comparable with none, itself included: two unknowns are never taken as equal.
    - `PatchesApplied` is read from it. A rejected or an unchecked patch is in both places: the finding is what the person is told, with the hunks, and the list is what a later edit is compared with. The hold reads the list.
  - As an outcome, on the stub's target: `Obsolete`.
  - Already on the edit or the branch: `Release`, `Files`, `Subject`, `Upstream`, `HTTPS`, `Source`, `RenamedFrom`, `Closes`.
  - Not stored: `Diff`, `Started`, `Applied`, `Current` and `Setup`, each worked out from the records; `Distfiles` and `Compared`, two counts the command prints once and nothing reads again; `HandEdit` and `Left`, which the hand edit's own record keeps (`recordHandEdit`, update.go:1331).
  - The notice for a new `-sys` crate is no finding of its own. The source comparison raises it (`nativeLinks`, internal/sourcecompare/compare.go:1098), and it is in the upstream comparison the edit already keeps (`Upstream`). The notices list reads it there.
- **A port found current has no edit and no result.** Its target says so (`Current`), saved when it is found where the branch exists, so a continuation doesn't look again. That concludes the version alone. What else was asked of the port, its source, its stub, its dependents, is concluded each by its own outcome. **A port held before any edit** (a major version, another open pull request) has neither a branch nor a record: it is a fact about now, judged at each look and said by serve's pass, as today.
- **The request is one value for one branch's work**, in `model`, kept in one JSON column on the branch. A Portfile's ports are its targets. A batch is one request for each branch, made from one set of flags, and each stands or is refused apart.

## An edit is recorded before its files are written

The order of an update changes:

1. Select the release, then prepare the edit into its tree. A plan returns here, as today. On a branch that exists, the selection is saved on its target as soon as it is made, before anything is downloaded.
2. Start the branch where the request says to, as today. The request is saved with it, or before the first edit where the branch exists. A new branch's request carries the release step 1 selected and prepared; no second look is made for it.
3. **Record the edit, unwritten**, in one transaction through `store.Recorded`: the edit with both blobs and both modes for each file, its result, the target's selected release, the tickets it closes, and the assessment and change record of the prepared tree where the update can make them.
4. Write the files (`ApplyToWorkingFiles`).
5. **Record that they are written**: `Written`, the event `branch.edit`, and the outcome of each action this edit concluded.
6. Rename the branch for its version where that applies ("The lock, and a rename").

- Everything in step 3 is made from the prepared tree, not from the working files. Today `editFindings` reads the working tree after the write (update.go:833) and `instructionLines` reads the Portfile from disk (:1371); both read the prepared tree instead. `editRecord` already builds its blobs from the prepared files (:936). So a later run can finish step 5 with no preparer and no download.
- **Each record has a witness that reads what it wrote.** Step 3's reads the edit by its ID, written or not. Today's (`editRecorded`, update.go:1066) asks `Reader.Edits`, which now hides the very row it looks for, so a record that landed with its answer lost would read as absent. Step 5's reads the same edit and passes only where `Written` is set. Both are plain store reads: a witness never settles files and never opens another operation.
- **The assessment is the prepared tree's, and stays that.** `ApplyToWorkingFiles` checks the edit's own files and no others (internal/git/working.go:119-131). A tracked file outside the edit can differ, and the tree after the write is then not the prepared one; Codex's probe shows it. Step 5 compares the two trees. Where they are equal, the assessment recorded in step 3 is the working tree's. Where they differ it is never relabelled: it stays an assessment of the prepared tree, and the working tree's is made by the reader that makes it today (update.go:833), when it is asked for.
- **Step 3 reads the request again under the lock.** Step 1 runs outside it and can take minutes, and notes the request's ID when it starts. Another command can meanwhile change what was asked without touching a file, and step 4's check of the files wouldn't notice. Where the ID is still that one, the edit is recorded. Where it isn't, everything this edit was prepared with is compared with the request as it now stands: that the port is still a target, its release, the source asks, and each side action this edit carries out. All equal, as where only the stage or a later stage's flag changed, the edit is recorded under the request as it stands. Otherwise nothing is recorded and the port is prepared again. Either way step 5 writes its outcomes and its tickets from the request it reads then, never from the copy step 1 read.
- Where step 3's outcome is unknown and the read back finds no edit, nothing was written and the command fails clean. Where step 4 refuses because a file changed while the edit was prepared (`ErrWorkingFile`), nothing was written: the run removes the edit it just recorded and says to run it again, as today. Where step 5 fails, the command says the files are written and the record isn't complete, and the next command completes it.
- A hand edit's attempt (`update --hand-edit`) records no edit of dockhand's, as today, and skips step 3. Its files are the person's, and so is what a failed record of it leaves.

**Settling.** Every command that reads a branch's edits first asks for its unwritten ones. There are normally none. For each, it reads the edit's files in the worktree, their presence, blob and mode, against the two sides recorded. Whether a file is as recorded after is told by hashing the file as it is, which needs none of the kept objects:

| The edit's files are | It means | update of that port, bump, ship; serve for its own branches | every other command |
| --- | --- | --- | --- |
| all as recorded after | they were written and step 5 was lost | settle: step 5 | the same; it is a store write only |
| all as before | never written, or put back | write them from the kept blobs, then settle | say it in a line and go on with the files as they are, under the rule below |
| some after, the rest before | the write was cut short | write the rest, then settle | say it and stop, naming `dockhand update <port>`; status says it and shows the rest |
| any one neither | someone changed it since | stop. Nothing is written. The message names each such file | the same. `tidy`, with the person looking, commits the files as theirs and removes the unwritten edit |

- **A plan settles nothing.** Under `--plan`, of update, bump, ship, check or submit, an unwritten edit is read and shown as it would be settled. No file is written, `Written` is not set and nothing is recorded, whichever row applies.
- **An unfinished update is carried into every gate, not hidden.** Its edit is absent from `Reader.Edits`, and nothing may take that absence for "no such update". `Preparation` answers unfinished for it ("Readiness"), and that answer is asked where a check is proposed and where a submission is planned, as it is before a commit. With nobody there, each stops and names the command that finishes the update: the files as they are must not be checked and submitted as the update that was asked. A person's own `check` or `submit` is told what isn't in the files and goes on, as with a hold. Looking at the tree as it stands, or checking it, is theirs to ask.
- An edit is settled whole or not at all. A file outside the edit's own list is not read here: a person's change to another file of the port leaves the edit to settle, and stays an uncommitted change that isn't dockhand's, which holds an unattended commit as today.
- A version named for its port replaces an unwritten edit whose files are all as before, and says so. Where some of them are written, the edit is finished first.
- "Nothing to change" can't be said over an unwritten edit: it is found before any release is looked for.
- **Where the kept blobs are gone.** An edit's After blobs are loose objects no ref keeps, as they are today. An unwritten edit left longer than Git keeps unreferenced objects can find them collected. A missing object is its own answer, and not a file that differs. With every file as before, the unwritten edit is removed and the port prepared again at its kept release and the request as it stands, with its findings made afresh, and that is said. With every file as after, nothing was needed from the objects and the edit settles. With some files written, nothing is written and the run names the files and stops.
- **The lock.** From the moment the branch exists, steps 2 to 6 run under the lock history's transitions take, and so does settling. Each reads the branch and its edits after it has the lock. Step 1 runs outside it, since it downloads; step 4 checks again that each file is as step 1 captured it, and step 3 that the request is.

**The lock, and a rename.** The lock is by the branch's name (internal/git/branch.go:32), and step 6 changes the name. Today's rename moves the Git branch first and saves the name after, and its witness shows only that the branch's row exists (internal/engine/branches.go:237, :1393). Codex's probe lost that save unseen: the rename returned success, Git had the new name and the store the old one, and a process following the old name could never learn the new. So:

- **The intent is recorded first.** Under the old name's lock, the rename takes the new name's lock too, without waiting; where it can't have it, the branch keeps its name this time. Then `RenameTo` is saved, with a witness that reads it back; the Git branch is renamed; and the name is saved and `RenameTo` emptied, with a witness that reads the new name. The old name's lock is let go, and the command goes on under the new one.
- **A name being moved to is taken.** A rename is refused a name that another branch has, as its name or as its `RenameTo`, and so is a new branch. So a `RenameTo` names one branch.
- **Either name finds the branch.** `Resolve` looks a typed name up among the branches' names, then among their `RenameTo`, short or full, and only then follows a rename made by hand with Git (internal/engine/branches.go:615-634). Without that, a rename that died before Git moved anything can't be reached by its new name: no record has that name, and `followRenames` needs a worktree already on it (internal/engine/renames.go:42-61). `followRenames` and `carryRename` pass over a branch whose `RenameTo` is set. They follow what a person did with Git, and an intent is dockhand's own to finish.
- **A rename cut short is finished by the next command that takes the branch's lock to change it**, before anything else. It takes both names' locks, the old name's and then the new one's without waiting, and reads the intent and Git's two names again under them. Where it can't have the second lock it leaves the intent as it is, lets go of the first, and says a rename of the branch is under way; it never waits for one lock while it holds the other. Where Git has the new name and not the old, the name is saved. Where Git has the old and not the new, the rename is made and then saved. Where it has both or neither, nothing is guessed: the command stops and says so.
- **The repair is at the lock, not at the lookup.** Not every command comes by a branch through `Resolve`. The engine reads one by its ID (`Engine.Branch`, internal/engine/branches.go:895), serve's recovery lists rows and reads each one's name in Git (`ResumePrepared`, internal/engine/recovery.go:45-64), and a pull request's number finds a tracked row (`AdoptPullRequest`, branches.go:945-960). However it came by the branch, a command that changes it takes its lock, reads it again by ID and finishes a rename cut short before its first Git operation that uses the branch's name. A row with `RenameTo` set is never used for its name as it was read: recovery would otherwise pass over a branch whose rename Git had finished, as one with no branch. A pull request's own head is another name, and the repair doesn't touch it.
- **A plan and `status` finish no rename.** They find the branch by either name and say the rename as it would be finished, as a plan does for an unwritten edit. Nor does a process that holds a head's lock (#191): under it no branch's lock is taken ("Where the request meets its neighbours"). Reads from the store stay reads.
- **A process is on a branch by its ID, never by a name it read earlier.** After it gets a name's lock it reads the branch again by ID. Where the name is now another, it lets go, takes that name's lock and reads again, until the name it holds the lock for is the branch's. A new branch that has since taken the old name has another ID, so it is never mistaken for this one.

## Readiness

`Preparation` takes more than it does today: the branch's written edits; its unwritten edits with what settling found; its uncommitted paths in two lists, those its edits explain (as tidy tells them) and the others; the request's targets; whether the caller is attended; and the allowances as they are now. It answers the first of these that applies, and returns every reason with it, and whether the edits are committed:

| Answer | When | ship, bump and serve | status |
| --- | --- | --- | --- |
| hand-edits | an uncommitted change that isn't dockhand's, or a file an unwritten edit can't be settled over | stop | as today |
| unfinished | an edit unwritten or partly written; a target with no edit that wasn't found current; an action asked that has no outcome yet, or was refused; dockhand's own edits written and not committed | finish it and ask again; serve only for its own branches. A refusal is not tried again: it is said, and stops | a line for the first three: "an update of jq wasn't finished; `dockhand ship jq` finishes it" |
| held | below | stop before the check, and say whether the edits are committed | held before its check, with what holds it |
| ready | | queue the check | as today |

- **A port is held** where a rejection of one of its patches stands, the caller is unattended, and the port's `holds.allow` doesn't name the kind now. Who prepared the update doesn't enter into it. A person who saw the rejection at their own `update` has not thereby approved a check nobody attends, and no such approval is on record. The policy applied is the caller's, at the moment it asks.
- **A patch's rejection stands** from the update edit that recorded it until a later update edit of the port, with a list read in the same scope, records that patch as applied or doesn't hold it. Nothing else ends it. An edit whose `Patches` is nil changes nothing: a moved repository followed alone, a revision bump, a checksum refresh, a declaration that couldn't be read. Nor does an edit that left that patch unchecked. Nor does a list read in another scope, whatever it says of the patch: that it applies on another system, or isn't declared there, shows nothing of the system that rejected it. So a rejection is never cleared by the absence of a result, and one patch left unchecked never hides another's rejection in the same edit.
- **dockhand's own drop of a merged patch is no exception.** `DropPatch` takes out a top-level word and leaves a conditional declaration of the same file standing (internal/macports/portfile/patches.go:17; Codex's probe), and the check that the port evaluates as before is made in one context (`correct`, internal/macports/portedit/correction.go:36). A drop ends a rejection as any absence does, in its own scope.
- **Where no comparable update can be had, the person ends it.** The Mac that rejected the patch may since have a newer system, or the next update may run on another Mac. The hold then stands and says why: the patch, where it was rejected, and that a later update on another system found it otherwise. `holds.allow` or the person's own ship ends it, as either ends any hold. The hold keeps a build nobody will look at from being spent. The check is where a patch is applied on each system asked, so a hold kept too long costs a person one command.
- **A rejection from before has no patch's name and no scope**, so nothing recorded later is comparable with it. It stands until the person ends it, or the branch ends.
- **A refusal blocks until the ask changes.** A refused action is concluded and not carried out, and is never read as finished. The verdict says unfinished and names it; nothing commits, checks or submits past it, attended or not; and a continuation doesn't try it again, since nothing it was judged by has changed. It ends where the ask it answers changes, which removes the outcome ("Each action has its own outcome"): `--source` given another value, `--with-obsolete=false`. The message names the flag and what changes it. A port whose update failed with an error is different: it has no outcome, and the next continuation tries it.
- **Ports aggregate by their own edits**, so the order of updates doesn't matter. A clean sibling and a port found current change nothing of another port's hold or of this port's.
- **Nothing clears a hold, because none is stored.** What ends one: `holds.allow` naming it; a person's own ship, check or submit, which is told and goes on; an update of the port in the same scope that checks the patch again and finds it applies, or no longer declares it. A newer release appearing does nothing. `Edit.Held`, `SetHeld` and `holdBeforeCheck` go.
- **Unfinished comes before held**, because serve commits held work and holds it at its check (ship.go:142-159). A verdict that answered held first would leave it uncommitted.
- status keeps its two sources, one for each kind: preparation's findings before a check, recorded assessments after (`assessedHolds`). One list is shown, and a branch held before its check says whether its edits are committed.

## The commit, and what recovery expects

- **Before an unattended commit**: nothing is unwritten, every target has a written edit or was found current, every action asked is done or already so, and no uncommitted change is anyone's but dockhand's. Then tidy plans, the commit gate judges and tidy applies, as today, leaving its checkpoint. No hold is written before it: the step #116 added goes with the stored hold.
- **Recovery** considers serve's open branches with no pull request and no run, as today, and at each of serve's daily looks as well as at its start. It continues a preparation where `serve.ship_to` is commit or further, and queues a check where it is check or further.
  - Unfinished: it settles, prepares the targets still without an edit at their kept releases, and commits.
  - Committed: it expects two things, and nothing uncommitted.
    - **The history is dockhand's.** Of the branch's checkpoints that were applied, the newest is a tidy, not restored, whose `After` is the branch's head. One only proposed, or abandoned, after it is not counted either way (`CheckpointPrepared`, `CheckpointAbandoned`, internal/model/history.go:296). This catches a commit made with Git, a rebase and an undo.
    - **The content is dockhand's, by the test tidy itself uses.** For each port's directory the branch changes, `fromEdits` (internal/engine/tidy.go:1069) is asked of the base's tree and the head's. It starts from the base's files, requires each written edit's Before to be what the edit before it left, steps through every edit, and compares the end with the head. It is the test the unattended commit passed for the same files (`Plan.Unambiguous`, internal/branchstate/commit.go:36), and it takes any tree, so it is asked of a commit as it is of working files. The merge first compared only each path's newest After, and Codex's probe passed a person's work through that: dockhand updates a port, a person adds a comment, dockhand updates again over the comment, and the person's own reviewed tidy commits it. The newest After is the head's file. The chain breaks at the second edit's Before.
    - **Every changed path is covered.** `fromEdits` answers for one directory's paths, and says no for a directory with no edit (tidy.go:1069-1078). So the caller lists every path that differs between the base's tree and the head's, and each must lie in a port's directory that passes. A changed path outside any port's directory, under `_resources` say, or in a directory with no edit, fails the test.
    - **Modes are part of the chain.** `fromEdits` compares blobs today. It gains the two modes each edit now records, so a person's change of a file's mode breaks it as a change of its content does. An edit from before has no modes: for its files the head must have the mode the base has, or a plain file's where the base has none. Anything else can't be shown to be dockhand's, and is left.
    - **What tidy adds itself is never there.** A lint fix, an untracked patch file it took, a declaration and a restyle (tidy.go:753, :800, :1506-1522) each make the head something the chain doesn't end at. None is made on this path: a lint fix or a taken file already fails the unattended gate, a declaration needs a check that passed, and a restyle is asked for. A branch that has one was committed with a person looking, and is left for them. This holds because recovery considers only a branch with no check: once a check has passed, a declaration is tidy's own to make, and nothing here is said of a tidy after one.

    Then it asks `Preparation`, and where the answer is ready it proposes and queues the check under the settings of that moment.
  - Anything else is named and left: a commit dockhand didn't make, a commit of its own undone, a rebase, uncommitted changes that aren't its own. The line names `dockhand ship -b <branch>`.
- "Prepared, committed, check not yet queued" is therefore read from the checkpoint and the edits, and no longer inferred from four facts. No field is added to the checkpoint.
- A person's ship is not recovery. It takes a committed branch as it stands, under its own gates, as today.

## A preparation that stopped part-way

- The branch holds the request's targets and the edits made so far, written or not, and the outcome of each action concluded. Nothing is committed unattended until every target has a written edit or was found current, and every action asked is done or already so.
- **A release is kept from the moment it is selected**, not from the edit. Selecting comes before the download (`ResolveRelease`, update.go:497; `Prepare`, :532), and a second port's download can fail after its release was chosen. On a branch that exists the selection is saved on the target at once, so the same command typed again prepares that release, and not a newer one that has appeared. Where no branch exists yet nothing was accepted, and a failure there leaves nothing: the next run looks afresh.
- **A written edit for the port doesn't finish what was asked beside it.** A stub that follows, each dependent's revision bump and a ticket are each owed until their outcome is saved: in the transaction that records the action's last edit as written, or by itself where the action was already so. A continuation reads the outcomes, not the port's own edit. For dependents it looks them up again and leaves out those the branch already changes (`LinkedPorts`, internal/engine/diff.go:505), and saves the outcome when the last is written.
- Whoever continues it settles first, then prepares each target that has no edit, with the release kept for it or a fresh look where none was selected yet, then carries out each action that has no outcome, then goes on. The same command typed again continues it, and so does `ship` of the branch. serve continues its own branches at each daily look, before the outdated pass, which would otherwise skip the port as already in a branch. serve never continues a person's.
- A port whose update failed stays failed and is said. The others' edits stand.
- **A port's own refusal comes at its turn.** The resolver refuses what it can know before any edit (see "The flag check"). What only a port's preparation can know, such as a source move that port can't make, is refused when that port is prepared. For a single port that is before any file is written. For several ports of one Portfile the earlier ports' edits stand, nothing is committed, and the command says which port refused and why.

## The request

- **One resolver in the engine** builds it. It takes the flags with whether each was given, the settings, serve's configuration, the targets discovery found, and the request kept on the branch where there is one. It returns the request, the outcome of each action it can already judge, and each fact not resolved yet with its reason. Commands parse flags and ask questions; they build no request.
- **Resolved when it is built**: the targets, the branch, the stage, and the source policy from settings where no flag was given. **Carried as asked and resolved at its stage**: a target's release (when it is selected, then kept), the check's place and tests (when the check is proposed), the destination (at the submission's plan).
- A release that can't be selected stops an execution before any file. It is never kept as "the newest", to mean something else later.
- **What it replaces.** `VersionUpdate` stays the commands' flag set and feeds the resolver; its `Request` goes. `LinkedOptions`, `shipOptions` and `shipTarget` go into the request. `PrepareOptions` builds a request from settings. `UpdateRequest`, `WhereRequest`, `CheckRequest` and `SubmitRequest` stay the stages' own requests, built from the request by the engine and no longer by a command.
- **Primitives keep their literals**: a checksum refresh, `revbump`, the checksum update `create` runs once it has written its Portfile, the obsolete stub's update and the dependents' revision bump. Each goes through `Engine.update`, so its files are recorded before they are written too. `create`'s own Portfile, the move of a created port and a port's rename do not go through it: they record their edit themselves, written ("The records"). None reads a request or changes one.
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

- **Four outcomes**: done, satisfied, refused, kept. Kept is said of a flag given to a command that stops before the stage that reads it: the flag is on the branch's request, and no outcome is stored for it. A question nobody answered is none of the four. It is asked again where a person is there, and is a refusal where nobody is.
- A lookup that failed is an error. It is never "already so".
- A plain update of a current port still says "already at 1.8.1; nothing to change" and starts no branch. The `update.source` setting alone asks nothing of a current port; `--source` given does (#121's own words: "a plain update of a current port stays the no-op it is").
- Where a side action has something to change and the version is current, the branch is started for it.
- `--closes` with nothing else to change is refused by name: a ticket alone starts no branch. Today it is dropped with no word (update.go:633 returns before the tickets are joined).
- **An outcome is saved when its action is concluded**: on the target for what was asked of a port, and on the request for the dependents and a ticket. That is in the transaction that records the action's last edit as written, or by itself where the action was already so or was refused. An action with no outcome is still owed, and the verdict says unfinished for it.
- **An outcome is removed when anything it was judged by changes**, in the transaction that changes it. The flag that asks for an action is not the whole of that:

  | Outcome | Judged by |
  | --- | --- |
  | version, on a target | the version named and the release selected; `--shared-release`, which decides whose release it is before the source is chosen (internal/macports/portedit/version.go:83); `--keep-http`, `--keep-old-checksums`, `--keep-modeline` |
  | source, on a target | `--source`; the release selected; `--shared-release` |
  | moved repository, on a target | `--follow-moves`; the release selected |
  | obsolete stub, on a target | `--with-obsolete`; the release selected |
  | dependents, on the request | `--revbump-dependents`, `--except`; the set of targets; the branch's base, which `LinkedPorts` reads (internal/engine/diff.go:505) |
  | ticket, on the request | `--closes` |

  The action is then owed again, and is judged from the Portfile and the branch as they are. So a refusal for a sibling that would have moved is judged again once `--shared-release` allows it, and a target added to a branch whose dependents were done has its own looked up. Without this a port found already fetching by archive would keep "satisfied" after `--source releases` was given, and a continuation that carries out only what has no outcome would pass the new ask by. The rule is the first sentence; the table is its reading of today's flags, and a flag added later is placed in it by the flag check's test.
- **Judging again never rewrites a written edit, and never calls an unapplied flag done.** An action carried out is found already so, and costs no second edit. A shaping flag changed after its edit is judged by what it asks. Where the Portfile is already as it asks, satisfied. Where an edit is prepared that makes it so, that edit is added beside the old one, and done. Where neither, the flag is refused by name, and the refusal blocks until the ask changes ("Readiness"). `--keep-http` withdrawn after a version's edit that kept a plain-HTTP link is the case: the version being current doesn't answer it. Today's update gives the preparer its HTTPS answers by that flag before it prepares (update.go:474), so an edit prepared the other way shows nothing of this one. Nothing in this design prepares an edit for a version already written, so there the flag is refused unless the Portfile is already so. The same holds for `--keep-old-checksums`, `--keep-modeline` and `--shared-release`.
- **A refusal is kept, and lets nothing go on.** It is an outcome, so it is not judged again at every look; and it is not "done", so every gate that asks whether the actions are concluded asks for done or already so ("Readiness").

## The flag check

- **The table is Go data in the engine**, one row a flag: the request's field, the commands that take it, the stage that reads it, what leaving it out means, what false or empty means, how long it lasts, and its outcomes. `docs/usage/flags.md`, a new page, shows the same table, and a test fails where the page and the data differ. The table as designed is under "The flag check's table".
- **The test that finds a flag with no row** extends the walk of the command tree that exists (`commands`, internal/command/contract_test.go:73). For update, bump, ship, submit, check, edit and revbump it visits every local and inherited flag. Each has a row, or a row that names the contract that owns it. A row for a flag that no longer exists fails too. No flag is excused by a prefix or by being unfamiliar. The request's fields are reflected the same way, so a field added without a row fails.
- **A row's truth is a behaviour test** for each of its outcomes, which looks at files, the record, a queued plan or the stand-in forge, never at two requests compared. Each row is tested left out against given false or empty, and across a restart where it is kept. Most rows run through the engine with the test preparer. The rows only the real preparer decides run with MacPorts' Tcl: the source moves and their refusals, the obsolete stub, a version equal to the current one or older, `--shared-release` and `--keep-old-checksums`. About six fixtures.
- **Refused before any edit**, by the resolver: `--except` alone; a version named for several unrelated ports; a ticket that isn't a number; a `--tests` value that isn't one of the three; and each pair of flags the commands refuse today. What a port's preparation alone can know is refused at that port's turn, as said above.
- **A stage that stops short** keeps every flag a later stage reads, and says each: `--title`, `--note`, `--tested-binaries`, `--tested-variants`, `--type`, `--draft`, `--skip-notification`, and `--on`, `--build` and `--tests` too. "Kept for the check: --on sequoia, --tests required. Kept for the submit: --draft, --type bugfix."
- **`--on` and `--build` are one choice, the place.** The commands refuse the two together, and that refusal sees only what was typed. So the request keeps one place: an `--on` given replaces a kept `--build`, and the other way round, and the place that results is checked again after the kept one is read. `--tests` is kept apart.
- **A baseline reads nothing kept.** `check --baseline` builds as the check it compares with was built, and takes none of these flags (`baselineFlags`, internal/command/check/check.go:346). A kept place or test policy is not handed to it either.

## What is kept, and what a later command reads

- **Saved** in the transaction that starts the branch or, on a branch that exists, before the command's first edit. update's is saved too, so that its stub and its dependents are finished after a stop. The title and the note are saved in the same transaction, and the tested statements go into the request as asked (below).
- **Ended** by the publication that took it, when that one finishes ([#191](https://github.com/herbygillot/dockhand/issues/191)); by a ship that reaches its submit and finds its work already delivered by a person's own `submit`; or with the branch. The rule and its table are under "Where the request meets its neighbours". Until then a submission that failed part-way is still this request's work, and the next ship goes on with the same release. A branch with no request gets one from the next update, bump or ship, as a new command does today, with a fresh look for its release.
- **A flag given replaces its kept field and is said. A flag left out leaves it.** Nothing is refused for differing.
- **The release.** A ship or bump of a branch that has a request goes on with the release kept for each target. It says which and when it was selected, and it does not look for a newer one. A version named replaces the selection and is said. `update` with no version finishes what is unfinished at the kept release, and where nothing is unfinished it looks for the newest as it does today, and says what it replaces.
- **The stage** is ship's. A `--to` given is kept; a later ship without `--to` stays at it, and says so with the command that goes further. `bump` is `--to submit`, given. Where no `--to` was ever given nothing is kept and ship's default applies. `update` stops after its edits and neither reads nor changes a kept stage. `ship --passing` leaves a branch kept below submit where it is, and names it.
- **The check's flags and the submission's** are read by whoever next runs that stage for the branch: ship going on, or `check` or `submit` on its own. `check` reads the place and `--tests`, each where it wasn't given, from the request, says which it took from there, and never changes the request. `submit` reads each of `--draft`, `--type` and `--skip-notification` it wasn't given the same way.
- **A weaker check doesn't stand for the one asked.** `check --on local`, typed against a kept `--build minmax`, runs where it was told and changes nothing kept. When ship, bump or serve then goes on to submit, the submit's request carries the kept place and tests as asked, so the evidence that stands is judged against them (`coversAsked`, internal/engine/submit.go:731, which is reached only with `PendingCheck` set, so it is set here), and where it falls short the check asked is run. Today that judgment is made only for a place or tests typed on the same command. A person's own `submit` judges evidence as it does today, and says where the check kept on the branch is not the one that stands.
- **The title, the note and the tested statements are kept from the start.** Today ship keeps them only after its commit, and only where it stops before submit; a ship that goes to submit holds them in memory, and one killed before its commit or during its check loses them. The title and the note are the branch's own fields, and are saved with the request's first save. A tested statement can't be saved that early as a statement: it is about the files at the branch's head (#153; `KeepForSubmit`, internal/engine/submit.go:1888), and those aren't there yet. So it is kept in the request as asked, given and true or false. It is bound to its files where it is bound today, after the commit or at the submission, and taken out of the request then. A later `--tested-binaries=false` replaces a pending yes before it is ever bound, and withdraws a bound one as landed (#198). Nothing bound is said again from the request.
- **A pending statement is for the work it was given for, and is bound to nothing else.** It is taken out of the request, and the command says so, in the transaction that changes what the files will be (a target added or removed, a selection replaced by another release, a source ask or a side action changed), and in the transaction that records any edit the request's own preparation didn't make: `edit`, a checksum refresh, `revbump`, a hand edit's record, a rename. Those change nothing else of a request. "The tested statement given for 1.9 was dropped; give it again for 2.0."
- **What no transaction sees is caught where the statement is bound.** A statement carried from an earlier command is bound only where the branch, from its base to the commit, is dockhand's own by the chain recovery uses, over every path the branch changes and with modes ("The commit, and what recovery expects"). The commit is the one the statement's files are read from. A file changed by hand breaks the chain, however it was committed, and the statement is then dropped and said. A commit made with Git that changes no file, or commits exactly what dockhand's edits wrote, passes, and rightly: the statement is of the files, and they are the ones it was given for. Otherwise an earlier yes would be bound to files it was never said of, and #153 can't withdraw a statement that was never bound. A statement given on the command that binds it is bound as today, whatever the files: the person said it of them. The same preparation finished later, by the same command typed again or by a continuation, binds the statement it was given, once.
- `--yes`, the branch selectors and the selections that make targets (`--mine`, `--maintainer`, `--category`, `--not-maintainer`, `--passing`) belong to the command that was typed. None is kept, and a continuation never runs a selection again to add ports.

## serve

- "Each new action" is each stage of each branch: its preparation, the queueing of its check, its submission. Each reads the process's configuration then. Nothing of the configuration is kept on the branch, so serve's request holds its origin and its targets and nothing else.
- Read at preparation: the source, HTTP and modeline settings, the port's `moves`, and the holds judged before an edit. At the check: `serve.ship_to`, `holds.allow` through the verdict, `build.unattended`, and `check.tests` through `ProposeCheck`. At submission: `serve.ship_to`, the holds and the limit.
- A branch prepared under one configuration gets the configuration of the process that queues its check, at recovery too. A check it queued keeps its plan, and a publication it recorded keeps its intent; each is read from its own record, with no copy in the request.
- Configuration is read when the process starts, so a changed setting counts from serve's next start. This design adds no reading of it mid-run.
- What serve can't be asked for stays that way: an obsolete stub, dependents, a ticket.

## Where the request meets its neighbours

- **The publication record (#191).** The request's `Publish` moves into `Publication.Asked` in the publication's first transaction: each field unless the submit's own flags say otherwise, and it is emptied in the request in that transaction. From there #191's rules decide alone: for the same commit and destination a later submit keeps each choice unless that run says otherwise, and `--rewrite-description` is never carried. No copy stays in the request to disagree with it, and none is copied back.
  - **A publication ends only the request it took.** The request has an ID, and every write that changes what was asked gives it a new one: a target added or removed, a version named, a selection replaced, a source ask, a side action, the stage, the check's place or its tests, a flag of the submission. A write of progress keeps it: a release saved when it is selected, a port found current, an outcome, a tested statement bound. The publication's first transaction keeps the ID of the request it took. When that publication finishes, the request is ended only where its ID is still that one. Without this the following is lost: 1.9 is submitted as publication A; the person runs `update foo 2.0`, which replaces the selection and writes new work; A then finishes, and ending "the request" deletes what 2.0's work needs. The branch's head can still be A's commit then, with the new edit uncommitted, so the commit can't tell the two apart. The same holds where no version changed: with A unfinished, `update foo --source archive`, or a ship that asks another check or another stage, asks something A never took, and A's finishing must not end it. This page first kept the ID where only a flag changed, and Codex's second pass lost exactly that.

    | The publication | The request |
    | --- | --- |
    | finishes with every step done | ended, where its ID is the one taken |
    | its pull request is found merged or closed | ended the same way; nothing reopens it |
    | is replaced by another, for another commit, destination or account | the old one is folded by #191's rules; a request with another ID stays, and nothing is copied back into it |
    | ends as refused, or waits for a person's answer | the request stays, since nothing was delivered; the next submit is a new attempt under it |
    | its branch is archived or forgotten | ended with the branch, after #191's folding |
  - **A publication takes a request only from a submission that is a stage of it**: ship, bump or serve going on to submit, planned and judged under that request. The submission carries the request's ID from its plan into the publication's first transaction, which reads the request and compares. Equal, `Publish` moves over and the publication keeps the ID. Not equal, another command changed what was asked while this one was being judged: nothing is recorded, the head's lock is let go, and the submission is planned and judged again under the request as it stands. A submit that goes on with a publication already recorded, for the same commit and destination, compares the same way, and where the IDs are equal the publication's kept ID becomes that one. The same commit and destination are never enough: a stronger check asked in between changes neither.
  - **A person's own `submit` takes none.** It judges evidence as today and goes on past an unfinished preparation it was told of, so it can't be said to have carried out what the request asks. It reads the submission's kept flags it wasn't given, and those move to its publication, which delivers them. It keeps no ID, and its finishing ends no request: the targets, the actions still owed, the stage and the check asked stay. The next ship of the branch goes on under them and says what is still owed; a stage kept at commit or check still stops it there. Where a person's own `submit` goes on with a publication a stage's submission recorded, the publication's kept ID is emptied in that transaction, so its finishing ends nothing either.
  - **A ship that finds its work already delivered ends its request under the same guard.** Where that ship reaches its submit, its plan is made as any submission's is, under the request it was judged by. Where the plan finds something still to do, a title or a type newly asked, it is a submission, and takes the request by the hand-over above. Where it finds nothing to do, the pull request being there for this commit and destination as now asked, the request is ended in a transaction that compares the ID the ship was judged under with the request's, as the hand-over does. Unequal, the request stays and the ship is judged again. "Nothing to do" is that plan's own finding, made now. It is never read from there being no publication on the branch, which is also what a refused or a replaced one leaves (#191, "Ending a publication"), and never from the commit and destination alone.
  - **The request is changed only inside a transaction that reads it first.** Authoring runs under the branch's lock and a submission under the head's (#191). Those are two locks, and both write this column. Nothing writes back a request it read earlier. A process that needs both locks takes the branch's first and the head's second. Under the head's lock no branch's lock is taken and no rename is finished: a submission resolves its branch, and finishes a rename cut short, before it takes the head's lock, and where it then finds `RenameTo` set it lets go of the head's lock and starts over. So nothing that holds a head's lock waits for a branch's.
- **The check's lifecycle ([#193](https://github.com/herbygillot/dockhand/issues/193)).** The place and the tests are resolved by `ProposeCheck`. Whether a check that exists already covers the one asked is admission's answer, as today. ship's stay with its check is #193's `Stay`, through `See`. A cancelled check never counts as the check the request asked for.
- **The source chooser ([#16](https://github.com/herbygillot/dockhand/issues/16)).** Its typed refusal is the source action's `refused`, with its reason. Its ambiguity is a question where a person is there, and a refusal that names the candidates where nobody is.
- **The review question ([#162](https://github.com/herbygillot/dockhand/issues/162)).** Its answer is a decision's record. No request holds it, and a later process asks again under #162.
- **The two drivers ([#43](https://github.com/herbygillot/dockhand/issues/43)).** Both read the same value and call the same engine functions with requests built from it. They keep their scheduling, their questions and their output.
- **The boundary test** reads the syntax tree. It forbids a composite literal of `ChangeRequest` outside the resolver, the store's decoding and tests, and one of `UpdateRequest`, `WhereRequest`, `CheckRequest` or `SubmitRequest` in a command or a driver, except a named list: the checksum refresh (author.go:301), `revbump` (internal/command/author/edit.go:136), the stub (update.go:346), `create` (internal/engine/create.go:269), the dependents' bump (diff.go:607), and `check` and `submit` on their own. Beside it, one test for each of update, bump, ship and serve shows the command reaches the resolver.

## `ship --plan`

It shows the value: each target with its release or "not resolved now" and why; the side actions with what is already so; the stage, kept or asked; the place as asked or "decided when the check is queued"; the destination "decided at submit"; the holds known now; and what is kept from an earlier command. An unwritten edit is shown as it would be settled, and is not settled. That holds for every command's `--plan`, update's and bump's included ("Settling"). A plan saves no request, starts no branch, records no edit, sets no `Written` and writes no file. JSON carries the same fields.

## Existing databases and branches

- **One schema step for each issue.** #194's adds `written` and `result` to edits, the two modes to an edit's files, and `request` and `rename_to` to branches. #195's changes no table, since the request's other fields are in the same column. It still raises the schema's number: a dockhand built before it would drop those fields when it saved a branch, and must not open the database.
- **Both wait for quiet the way #191's does**, by its amended page: the upgrade is refused while another process named dockhand is running. An earlier update paused between its files and its record would otherwise write files no record explains, once, at the upgrade. This design adds no test of its own for it.
- **An edit from before** gets `Written` set to its own time, which is true: its files were written. Its `Held` becomes a `patch-rejected` finding, left, with the held lines as its detail, and the column goes. The old record doesn't name the patch and has no list of patches, so the hold reads that finding. Nothing recorded later is comparable with it, and it stands until the person ends it or the branch ends ("Readiness"). Nothing else is worked out: it has no other result, its files have no modes, and the notices list says nothing for it. Recovery reads such a branch's modes against the base ("The commit, and what recovery expects").
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
| 12. What ends a hold | a new edit of the port | new evidence that covers it | Codex's, by each patch: a later update, in the same scope, that records the patch applied or gone (the review, F3, and its last look). Today a revision bump lifts it (preparation.go:45-52) |
| 13. What recovery expects | every changed path explained by the edits | the exact checkpoint, with new fields on it | Both, and no new field: the checkpoint as it is (history.go:306) for the history, tidy's own chain for the content (the review, F2) |
| 14. Committed branches from before | today's four facts | never queued unattended | Neither. The same test as any branch |
| 15. When serve continues its own | at its start | at each pass | Codex's: each daily look, before the outdated pass |
| 16. Several ports, one refusing | at its turn | all prepared in scratch first | At its turn, and said so. The loop stays as it is |
| 17. The hand edit's attempt | as today | recorded before its files too | As today. A lost record leaves files called the person's, which they are |
| 18. update's request | not kept | kept for its side edits | Kept, with its targets |
| 19. The request's identity | a value on the branch | a row with a generation, and references to the check and the publication | The value, with an ID that changes with what was asked and that a publication keeps (the review, F5, and its second pass). No generation, and no reference to the check |
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
| 30. A tested statement and changed files | not raised | must lapse | Already so for a statement that is saved (#153; submit.go:1889). Saving it early is the review's F6 |

## What the review changed

Codex reviewed the merge at main 3e091339b and said to revise it before adoption. It accepts the records as merged: fields on the edit, targets on the branch, no table of their own, no ref for an unwritten edit's files, no generation on every edit. It ran four probes of today's code in a copy of that commit. Each point below was read against the code again, and each holds.

| Codex's point | What was wrong in the merge | Taken |
| --- | --- | --- |
| F1. The witnesses | Step 3 kept today's witness, which asks the reader this design teaches to hide unwritten edits, so a record that landed read as absent. Step 5 had none of its own | A reader by ID for both: step 3's finds the row, step 5's finds `Written` set. Neither settles anything |
| F1. Every writer of an edit | "The primitives go through update." Three writers record their own edit after their files (create.go:257, :387; portrename.go:252), and one left unchanged would have hidden an applied edit | `AddEdit` refuses an unwritten edit, and `AddUnwritten` is the update's. The three keep their order and record their edit as written, and are said not to be improved |
| F2. Recovery's test of the content | "Each changed path ends at its newest After" passed a person's edit between two of dockhand's (probe). It was not the chain tidy follows, and it compared no modes | `fromEdits` itself, over the base's tree and the head's, with the two modes added. What tidy adds itself is never on this path, and a branch that has it is left. A checkpoint only proposed is not counted |
| F3. What ends a hold | `PatchesTried` was "nothing left unchecked", which is also true of an edit that checked no patches at all (probe: a move followed alone). It lifted a rejection with no new evidence, and one unchecked patch hid another's rejection | Each patch's state is kept. A rejection stands until that patch is recorded as applied, or as no longer declared |
| F3. Who prepared it | The hold asked that the update was prepared unattended, so a person's `update` followed by `bump` went on to its check | The caller's policy alone. The field is gone |
| F4. When a release is kept | In step 3, after the download. A second port could have its release chosen, fail to download, and be given a newer one on the next run | Saved when it is selected, on a branch that exists |
| F4. The actions beside the version | "Every target has an edit or is current" finished a request whose stub or second dependent was still owed | An outcome for each action, saved when it is concluded. Without one the verdict says unfinished |
| F5. A publication ending a later request | "The request ends when a publication finishes" deleted what a later `update` had put in its place | The request has an ID that changes with the work, and a publication ends only the one it took. The table of endings is Codex's |
| F5. A request changed during the download | The files' check doesn't see a version or a source ask that another command changed | Step 3 reads the request again under the lock, and prepares again where it changed |
| F6. Title, note and tested statements | "Kept as today", and today ship keeps them only after its commit and only below submit (ship.go:758-768) | The title and note with the request's first save. The tested statements in the request as asked, bound to their files where they are bound today |
| F7. `--on` and `--build` | Read apart from the request, a kept `--build` and a typed `--on` met after the commands' own refusal had run | One place: either replaces the other, checked after the kept one is read |
| F7. A baseline, and a weaker check | A kept place would have been handed to `check --baseline`, which takes none. A weaker check typed alone could have stood for the one asked | A baseline reads nothing kept. The submit that follows judges the evidence against what is kept (`coversAsked`) |
| F8. The rename | Putting today's rename under the lock left its witness, which shows the branch exists and not its name (probe), and left a waiter no way to the new name | The intent recorded first, both witnesses reading what was written, a rename cut short finished by the next command, and every process on the branch by its ID |
| F9. A plan, and files as before | "Every reader settles" and "a plan writes nothing" disagreed. "Go on" let an unattended run deliver the old files as the update | A plan settles nothing. Unfinished is asked where a check is proposed and where a submission is planned, and stops one nobody attends |
| T1. The assessment | "The tree after the write is the prepared tree" (probe: a file outside the edit differs) | The assessment stays the prepared tree's. Where the trees differ, the working tree's is made as today |
| R2. Collected objects | A missing object read as a file that differs | Its own answer. Files all as after need no object |
| Two facts | `Distfiles`, `Compared` and `Differing` given to #192; the `-sys` notice called a regenerated block's finding | The two counts are printed and not kept, and `Differing` is a finding. The notice is in the comparison the edit keeps (compare.go:1098) |

Accepted as merged, with no change: no ref for the kept blobs (R2); no record for a port held before an edit (R3); the ports of a Portfile prepared in turn (R5); a `--to` given kept, and update leaving it (R6); four outcomes (R7); `--source` on a current port by the flag alone (R8); configuration read at the process's start (R9); what a plan may touch besides (R10); unfinished before held (T4); the later-stage flags kept and said (T5); the request outliving the publication's first transaction (M1); the hold for `ship --yes` and `bump` (M2); and #195's raise of the schema's number (M3).

### The second pass

Codex read the revision at b4f9e1e3a against main fce0515a0 and ran four tests of today's code, which passed. It found F1, F2 (for serve's path before its first check), F4, F7, F9, T1 and R2 answered, and the storage as it should be. What it still broke is below. Each was read against the code and holds. Its traces of the new records are inferred, since nothing of this design is built.

| Codex's point | What was wrong in the revision | Taken |
| --- | --- | --- |
| F3. The list that is saved | The preparer's list is filled before a merged patch is dropped, and keeps it as not applying (its run of the drop fixture). A malformed list, no archive and no patch declared all returned nothing | The final declaration, read after drops and refreshes. Nil is "not read"; an empty list is "none declared" |
| F3. A patch absent from a later list | A patch declared for one platform is absent from a list read on another, and that is not the patch gone | Absence ends a rejection only in a list read in the same scope, which is kept. A drop was first excepted; the last look took that back |
| F3. A rejection from before | "Records none rejected" ended it beside a patch left unchecked, which may be the one that was rejected | None rejected and none unchecked; then, at the last look, nothing later at all |
| F4. A new branch's release | Not said which release the new branch's request carries | The one step 1 selected and prepared, with no second look |
| F5. Which changes give a new ID | "A flag replaced does not": a source ask, another check or another stage given while a publication was unfinished kept the ID, and that publication's finishing ended them | Every change of what was asked gives a new ID; progress keeps it. The hand-over to a recorded publication was first unconditional; the last look tied it to the request the run was judged under |
| F5. Step 3's second reading | It compared the release and the source asks only | The ID first, then everything the edit was prepared with. Outcomes and tickets are written from the request read then |
| F5. The two locks | Loading a branch under the head's lock could reach the rename's repair, which takes a branch's lock | Under the head's lock no branch's lock is taken and no rename is finished |
| F6. A pending tested statement | Given for 1.9, it outlived `update foo 2.0` and would have been bound to 2.0's files | Taken out, and said, in the transaction that changes what the files will be |
| F8. Finding the branch | With the intent saved and Git not yet moved, the new name reached no branch (branches.go:615-634, renames.go:42-61) | `Resolve` reads `RenameTo`; a name being moved to is taken; `followRenames` passes over an intent |
| F8. The repair | It took one lock, and a plan would have repaired | Both names' locks, the second without waiting. A plan and `status` finish nothing |
| Outcomes that are stale | "Satisfied" for a source stayed after `--source` was given another value, and the continuation passed the new ask by | An outcome is removed with the ask it was judged by |
| A refusal saved | It counted as an outcome, so "every action has its outcome" let a refused branch be committed and checked after a restart | A refusal blocks, attended or not, until the ask changes |

### The last look

Codex read d1e792162 against main 23b41575a and ran one probe of today's code, of `portfile.DropPatch`. It found the storage, F4, the ID's changes, the refusal's gates and the lookup by either name answered. What it still broke is below; each was read against the code and holds. Its traces of the new records are inferred.

| Codex's point | What was wrong | Taken |
| --- | --- | --- |
| F3. A drop, in any scope | `DropPatch` takes out a top-level word and leaves a conditional declaration of the same file (probe; portfile/patches.go:17). The file removed proves nothing of another system | A drop ends a rejection in its own scope only |
| F3. Applied, on another system | A patch applied on another system ended a rejection, though that system can fetch another archive | The same scope, for applied as for absent |
| F3. What a scope is | OS, major version, architecture, subport and variants don't decide a condition on the minor version or the tools (eval/installation_test.go:289) | The evaluation's kept provenance, the Darwin version in full and the developer tools. Unequal or unknown: nothing is concluded, and the person ends the hold |
| F3. A rejection from before | Any later list, on any system, ended it | Nothing later is comparable. The person ends it |
| F5. The hand-over | "The request as it stands then" took a request the run's gates never judged: a stronger check saved between the plan and the first transaction | The ID the run was judged under is carried in and compared. Unequal: let go and judge again |
| F5. A person's own `submit` | It goes on past what is unfinished, and its finishing ended the request | It takes no request. A later ship ends it, having passed its own gates |
| F6. Other edits | `edit`, a checksum refresh, `revbump` and a hand edit change the files and no ask, so an earlier yes was bound to them | Taken out by any edit the request's own preparation didn't make, and bound only where the branch is dockhand's by the chain |
| F8. Ways to a branch | `Engine.Branch`, `ResumePrepared` and `AdoptPullRequest` pass `Resolve` by (branches.go:895, :945-960; recovery.go:45-64) | The repair is at the lock: before a command's first use of the name in Git, however it came by the branch |
| Outcomes | The list of five flags left out `--shared-release`, the checksum, HTTP and modeline asks, the set of targets and the base | "Anything it was judged by", with a table. Judging again rewrites no written edit |

### The confirmation

Codex read d5f197a93 against main fb3e42036 and ran nothing. It confirmed the patch comparison within the bound the scope states, the pending statement where the content test covers every changed path, the repair at the lock, and the hand-over by the judged ID. It corrected two things, and called both bounded, with no change to what is stored.

| Codex's point | What was wrong | Taken |
| --- | --- | --- |
| F3. What equal scopes mean | The bound was only under "Not checked", and two scopes with a part unknown could compare equal | The bound is in the comparison's own rule. A scope with a part unknown is comparable with none |
| F5. The ending at "already delivered" | It ended "the request" with no comparison, so a request saved meanwhile was lost; and "finished" could be read from an absent publication, which a refusal leaves too | The same comparison of the judged ID. "Nothing to do" is the plan's finding now |
| F5. A person's `submit` going on with a stage's publication | The publication kept its ID, so its finishing ended the request | The kept ID is emptied in that transaction |
| F6. The content test's reach | `fromEdits` is asked for one directory and refuses one with no edit (tidy.go:1069), so a path outside a port's directory was passed by; and a commit made with Git need not break the chain | Every changed path, in recovery too. A commit that changes no file passes, and rightly |
| Outcomes. A shaping flag changed after its edit | "The edit stands, and the command says so" let the version read satisfied with the flag unapplied (update.go:474) | Satisfied where already so; done by a new edit; otherwise refused by name, which blocks |

## The cases

"Settle" is the table under "Settling". The letters are the statement's; Y to AD are the cases Codex's proposal added, AE to AO the ones its review asked for, and AP to AX its second pass's.

| | The store then | The next command, and what it does | The person is told |
| --- | --- | --- | --- |
| A. `update jq` writes the Portfile and step 5 fails | the edit, unwritten, with its result | update, ship, tidy, status, serve for its own: settle | "finished the update to 1.8.1 an earlier run began" |
| B. As A, then (i) the person edits that Portfile, (ii) another file of the port, (iii) a newer release is out | as A | (i) neither: stop. (ii) settle; the other file holds an unattended commit. (iii) `update jq` settles 1.8.1, then looks and goes on to the newer one | which file, and the two ways on. For (iii), both steps |
| C. Killed around the write | no edit; the edit and no file; some files; all files; the edit written | nothing to finish; write and settle; write the rest; settle; stands | |
| D. Three ports under serve, one with a patch rejected, in every order | three targets; two edits, one with the finding; one target current | held in every order: the finding is on its own port's edit | the port, the patch, `holds.allow`, and that the edits are committed |
| E. Two ports, the second's update fails | two targets, one edit | the same command or serve's next look prepares the second again; nothing is committed meanwhile | the second's failure |
| F. Killed after the last edit; after the commit | edits written; then the checkpoint | recovery commits and queues; queues. No hold is written between | |
| G. Recovery finds serve's branch with commits and no check | | (i) its own commit: queue. (ii) a commit on top, (iii) other uncommitted changes, (v) a rebase: named and left. (iv) held: stays held. (vi) from before: as (i) to (v), its modes read against the base. (vii) a person's edit between two of dockhand's, committed by their own tidy: left, by the chain | what it found, and `ship -b` |
| H. A patch is rejected, then | the finding, left | (i) `holds.allow` gains it: ready. (ii) the person ships: told, goes on. (iii) update again, on the same system: the patch applied or no longer declared ends it; a move followed alone, the patch left unchecked, or an update on another system, doesn't. (iv) a newer release: nothing | |
| I. A notice and failed hunks under `ship`, one port and three | findings on each edit | ship lists them from the records as each stage passes, and after a restart | a line each, by port; what holds apart from what informs |
| J. A port held before any edit | nothing | the pass says it again | as today |
| K. `undo`; `clean` | | the edits stay, uncommitted, with their results; both go with the branch | |
| L. Edits from before | `Written` set, no result; `Held` as a finding | status and ship list nothing for them but the hold | |
| M. A current port with `--source` | | done: a source-only edit. Already so. Refused with the chooser's reason | each by name; the version said to be current |
| N. A current port with `--with-obsolete` | | the stub moves; already at the version; no stub: already so, said | which |
| O. Two or three of M to O at once | | each action its own outcome, on its target | a line each |
| P. A version named, equal or older | | equal: satisfied, and the other actions judged. Older: as today | |
| Q. `ship --to commit` or `--to check` with every later flag | the request, with each | kept and said. The later `ship`, `check` or `submit` reads them | "kept for the check: … kept for the submit: …" |
| R. `bump foo --tested-binaries=false` | the branch's statement | withdrawn, as landed (#198); its row stays | |
| S. `ship foo --draft --type bugfix` killed at four points; a newer release is out | the request: 1.9, draft, bugfix | every later ship, check and submit goes on with 1.9 and the kept flags. serve never takes it | which release it goes on with |
| T. A new command against it | | `--type enhancement` replaces bugfix. `update foo 2.0` replaces 1.9 and stops after its edit. `--to commit` is kept from then | each replacement |
| U. serve's settings change between stages | | the next stage serve starts after its restart reads them; a queued check and a recorded publication keep theirs | |
| V. `ship --plan` | nothing saved | "not resolved now", a row for each port, the kept flags shown | |
| W. Combinations | | `--except` alone, a version for several ports: refused, as today. No stub, no sibling, no dependent: already so, said. `--to commit --tests required`: kept | |
| X. `edit`, a checksum refresh, `revbump` on a branch with a request | | each settles first, acts, and leaves the request alone, but for a pending tested statement, which it takes out and says | |
| Y. Two commands on one branch; a rename cut short | `RenameTo`, where one was under way | the second waits for the lock, reads the branch again by its ID, and follows a new name. A rename cut short is finished first, or named where Git has both names or neither | |
| Z. A mode change, a deletion, an untracked file in the way | modes on each file | presence, blob and mode are compared; the untracked file is "neither" | which file |
| AA. Killed between two dependents' bumps, or before the stub | the first's edit, and no outcome for the action | unfinished, though the port's own edit is written. The dependents are looked up again, the one the branch already changes is left out, the rest are bumped, and the outcome is saved | |
| AB. A port found current, a revision bump, or a move followed alone, after a held update | the finding, on the update's edit | still held | |
| AC. Git collected an unwritten edit's blobs | the edit, unwritten | all as before: removed and prepared again. Partly written: stop | which |
| AD. A kept `--type` and a publication's for the same commit | the request's `Publish` moved into the publication when it was recorded | the publication's stands, by #191's rule; only a flag given now changes it | |
| AE. A record lands and its answer is lost | the edit, unwritten; or written | step 3's witness reads it by ID and finds it, and the write goes on. Step 5's finds `Written` unset and doesn't take it as done | |
| AF. `create`, the move of a created port, a port's rename | their edit, written | each records as today with `Written` set; none is hidden | |
| AG. A person's `update` shows a rejected patch; later `bump` | the finding | held before its check: the caller is unattended, whoever prepared it | the patch, `holds.allow`, and that `ship` goes on |
| AH. Two patches, one rejected and one unchecked | both states | held for the rejected one | |
| AI. The second port's release is selected and its download fails | the target with its release | the same command prepares that release again, though a newer one is out | which release |
| AJ. 1.9 is being submitted; `update foo 2.0`; the submission finishes | a request with a new ID; the publication with the old | the publication ends, and the request stays with 2.0's work | |
| AK. Another command names a version while this one downloads | the request, changed | step 3 reads it, records nothing and prepares again | which version it now prepares |
| AL. `ship foo --title T --note N --tested-binaries` killed before its commit, and during its check | the title and note on the branch; the statement in the request | the next ship has all three, and the statement is bound after the commit. `--tested-binaries=false` given meanwhile replaces it | |
| AM. A kept `--build minmax`; then `check --on local`; `check --baseline`; then ship goes on | the request, unchanged | the check runs locally; the baseline takes nothing kept; the submit finds the local check doesn't cover what was asked, and that check is run | |
| AN. Any `--plan` over an unwritten edit, its files before, partly or after | unchanged | shown as it would settle; nothing written or recorded | |
| AO. Files as before, and serve or `ship --yes` comes to its check or its submit | the edit, unwritten | unfinished: finished first, or stopped. Never checked and submitted as they are | the command that finishes it |
| AP. An update drops a merged patch that an earlier update recorded as rejected: on the same system; on another; with a conditional declaration of the same file left in the Portfile | the list without it, with its scope; the drop as a finding, fixed | same scope: the rejection ends. Another: still held, and the person ends it. The conditional declaration is in no list read here, and nothing is concluded of it | where it stands: the patch, where it was rejected, and `holds.allow` or `ship` |
| AQ. The port's last patch is removed; or its list can't be read | an empty list, with its scope; or none | the first ends the rejection where the scope is the same. The second changes nothing | |
| AR. A later update on another system: the rejected patch isn't declared there; or it applies there; or only the minor version or the tools differ | a list in another scope | still held in each, and the person ends it | |
| AS. A hold from before; then an update of the port with every patch applied | the finding with no name and no scope | still held: nothing later is comparable. The person ends it | |
| AT. A publication is unfinished; `update foo --source archive`, or `ship foo --to check --on sequoia`; the publication finishes. A submit is planned under one request and a stronger check is asked before its first transaction. A person's own `submit` over a kept `--to check --on sequoia` | the request with a new ID; the publication with the ID it was judged under, or none | the publication ends and the request stays. The submit finds the IDs unequal, records nothing and is judged again. The person's submit takes no request, and the next ship says what is owed. Where that ship finds nothing left to deliver and another request was saved meanwhile, the IDs are unequal: the newer one stays and the ship is judged again | |
| AU. `ship foo --tested-binaries` at 1.9 is killed before its commit; then `update foo 2.0`; or `revbump foo`, `edit` or a checksum refresh; or a file changed by hand, committed or not, in a port's directory or under `_resources`; or a commit made with Git that changes no file | the statement taken out of the request; for the last two it is still there | the next ship binds nothing: the command that changed the files said so, or the chain breaks where it would be bound. After the commit that changes no file the statement is bound, since the files are the ones it was given for | "the tested statement given for 1.9 was dropped; give it again for 2.0" |
| AV. A rename dies before Git moves the branch, or after; the branch is reached by the new name, the old name, a `--plan`, `status`, serve's recovery from its list, or a pull request's number | `RenameTo`, the old name | either name finds it. A command that changes the branch finishes the rename before its first use of the name in Git, however it came by the branch; recovery doesn't pass it over. A plan and `status` show it and finish nothing | |
| AW. A port already fetching by archive, then `--source releases`; dependents bumped, then another `--except`, or a target added, or a rebase; a sibling's refusal, then `--shared-release`; `--keep-http` withdrawn after the version's edit | the outcome removed, then the one judged again | the source is carried out or refused; the dependents already changed are found so and the new target's are bumped; the sibling's update is prepared. For `--keep-http`: satisfied where no plain-HTTP link is left; otherwise refused, stored as that, and nothing is committed or delivered until the flag is given as it was | the flag, the edit it can't be applied to, and what changes it |
| AX. A source refused, the version's edit written, then a restart | the refusal | unfinished: said, and nothing commits or checks past it. `--source` given another value removes it | the flag, why it was refused, and what changes it |

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
| `--on`, `--build` | `Check`, one place | B, S, C | the check | the place kept, else the settings of that moment | | the request; either replaces the other | done; kept* below the check; not read by `--baseline` |
| `--tests` | `Check` | B, S, C | the check | what is kept, else the settings of that moment | | the request | done; kept* below the check; not read by `--baseline` |
| `--draft`, `--type`, `--skip-notification` | `Publish` | B, S, Sub | submit | what is kept, else not a draft, the type worked out, notified | false, over a kept true | the request, then the publication | done, kept |
| `--title`, `--note` | the branch's own | B, S, Sub | submit | what the branch holds, else worked out | empty clears it | the branch, saved with the request | done, kept |
| `--tested-binaries`, `--tested-variants` | `Publish` as asked, then the branch's own | B, S, Sub | submit | what the branch holds for these files | false withdraws it, pending or bound | the request until bound to its files, then the branch | done, kept |
| `--ready`, `--accept`, `--standing`, `--no-check`, `--head`, `--remote`, `--rewrite-description` | submit's own | Sub | submit | | | #191's | #191's |
| `--fresh`, `--only`, `--also`, `--variants`, `--keep`, `--head`, `--staged`, `--working-tree`, `--include`, `--replace`, `--baseline`, `--of`, `--enqueue`, `--for` | check's own | C | the check | | | #193's | #193's |
| `--plan` | none | U, B, S, C, Sub | | | | | writes nothing |
| `--subject`, `--no-revbump`, `--no-open` | a primitive's own | revbump, the checksum refresh, edit | | | | | as today |
| `--tree`, `--db`, `--tool`, the output flags | settings' | all | | | | | settings' |

Where "as today" stands in a cell, what the code does there was not read for this file. The test, not this table, is what finds a flag that is missing.

## Choices the person should see

Each is something a person will meet. The person took all twelve as written, with the page ("Adopt", 2026-11-11 15:51Z).

1. **A flag for a later stage is kept, and said, where the command stops before that stage.** `ship --to commit --on sequoia --tests required --draft` keeps all three; the `ship`, `check` or `submit` that comes later reads what it isn't given. Today the check's and three of the submission's are dropped with no word. The other way refuses the check's flags with `--to commit`. `--on` and `--build` are one choice: a later one given replaces whichever is kept.
2. **A flag with nothing to do is said, and the command goes on**: `--with-obsolete` on a port with no stub, `--shared-release` on a port with no sibling, `--revbump-dependents` where nothing depends on it. `--except` alone, and a version named for several ports, stay refused. The other way refuses the first three for a named port, which stops commands that work today.
3. **`--closes` with nothing else to change is refused.** A ticket alone starts no branch. Today it is dropped unsaid.
4. **A ship or bump goes on with the release it selected**, says which, and doesn't look for a newer one. A `--to` given is kept until another is given. `update` is how a newer release is taken, and it says what it replaces. The cost: `bump foo` on a branch kept at 1.9 submits 1.9 though 2.0 is out, and says so. Your own `submit` delivers the branch and ends nothing else that was asked of it: the next ship says what is still owed.
5. **`--source releases` and `--source archive` are carried out on a port already at its newest version.** The `update.source` setting alone is not: a plain update of a current port stays "nothing to change".
6. **`ship --yes` and `bump` are held before their check for a patch that no longer applies, as serve is**, and as `--yes`'s help says. `holds.allow` lets it build, and a person's plain `ship` is told and goes on. It holds whoever ran the update: seeing the rejection at your own `update` doesn't let a later `bump` through. Today only serve is held.
7. **A revision bump or a checksum refresh no longer lifts that hold.** Only an update of the port that checks that patch again and finds it applies, or no longer declared, on the same system as the update that rejected it. After a system update on that Mac, or where the next update runs on another Mac, the hold stays until you end it, with `holds.allow` or your own `ship`. A hold from before the upgrade is ended the same way. The other way lets any later update that applies the patch lift the hold, on whatever system; what that risks is a build that fails at its patch on the system that rejected it.
8. **serve leaves a branch of its own that a person has changed**: a commit on top, a rebase, an undone commit. It names it and `dockhand ship -b`. Today it queues the check if the branch has commits and no check.
9. **serve takes up its own unfinished preparations at each daily look**, where today a branch written and never committed is never taken up again.
10. **Nothing unattended checks or submits past an update that wasn't finished.** serve, `bump` and `ship --yes` finish it first, or stop and name the command that does. Your own `check` or `submit` is told what isn't in the files and goes on.
11. **A tested statement given earlier is bound only to the work it was given for.** `ship foo --tested-binaries` that stops before its commit keeps the statement for the next ship of the same work. `update foo 2.0` in between drops it and says so, and so does `edit`, `revbump`, a checksum refresh, or a file changed by hand. The other way binds an earlier statement to files it was never said of.
12. **A flag that was refused stops the branch until the flag is changed.** `ship foo --source releases` refused for want of a release asset is kept as asked, so a later plain `ship foo` stops with the same refusal and names `--source`. The other way forgets a refused flag, and the next plain command delivers the update without what was asked. A flag changed after the edit it would have shaped is refused the same way where the Portfile isn't already as it asks: `--keep-http=false` after 1.9's edit kept its links stops the branch until `--keep-http` is given again.

## What this does not do

- It records nothing for a port held before any edit, and adds no general undo of an edit.
- It keeps no Git ref for an unwritten edit's files. Where they were collected the rule under "Settling" applies.
- It prepares the ports of a Portfile in turn, as today, and does not prepare all of them apart first. A port's own refusal can therefore follow an earlier port's edit.
- It doesn't make serve read its configuration again while it runs.
- It leaves what a plan does today besides: fetching master, and what opening the engine writes. A plan that touches neither the store nor a ref would be a change of its own.
- It leaves the hand edit's attempt as it is, and the two ship drivers two (#43).
- It leaves `create`, the move of a created port and a port's rename writing their files before their record, as today. A record lost there leaves files tidy calls the person's.
- It concludes nothing about a patch from a list read on another system, and nothing from a hold recorded before the upgrade. Where that leaves a hold standing, a person ends it.
- It doesn't rewrite a written edit for a flag changed afterwards, and prepares no second edit for a version already written. Where the Portfile isn't already as the flag asks, the flag is refused.
- The second part of #180, the table of code-generating crates, is not here.

## Tests

- **Settling**, one table: an edit and each state of its files, with modes, a deletion and an untracked file in the way, against what is written, what is recorded and what is said. With #194's failing case written first: the files written, the record lost, and a second update that says nothing to change.
- **`Preparation`**, one table: the three ports of D in each order; a revision bump after a held update; a port found current after one; the allowance given and taken away; attended and not; committed and not.
- **Recovery**: G's seven, and an unfinished branch of serve's at a daily look, each asserting what is queued and that nothing is overwritten.
- **The flag check**: the walk; a row for a flag that is gone; each row's outcomes; the Tcl fixtures for #121's three, the rows #156 and #169 left, and P.
- **The request kept**: S at each of its four points and each next command; T; Q through the later `ship`, `check` and `submit`; the handoff to a publication, with a submit that gives a flag and one that gives none; AD.
- **The review's cases**, each written to fail on the merge as it was: a record that landed with its answer lost, at step 3 and at step 5 (AE); every writer of an edit, after the schema step (AF); a person's edit between two of dockhand's, a person's change of a mode, a lint fix and a restyle by tidy, and a later checkpoint only proposed (G); a move followed alone after a rejected patch, a rejected and an unchecked patch together, and a person's update followed by `bump` (AB, AH, AG); a release selected and its download failed, and a stop between the port's edit and its stub or its second dependent (AI, AA); an old publication finishing over a newer request with uncommitted work, and a request changed while the files stayed the same (AJ, AK); a title, a note and a tested statement across a stop before the commit, and a later withdrawal (AL); a typed `--on` over a kept `--build` and the reverse, a baseline, and a weak check before a submit that asks more (AM); a rename whose save is unseen, a waiter on the old name, and the old name taken by a new branch (Y); each command's `--plan` over an unwritten edit in each state of its files (AN); and a check and a submit nobody attends over files as before (AO).
- **The second pass's cases**: a merged patch dropped after its rejection, the last patch removed, a list that can't be read, and a list from another scope (AP, AQ, AR); a hold from before against an unchecked patch (AS); a source ask, a check and a stage each given while a publication is unfinished, and a submit that goes on with a recorded publication (AT); a pending tested statement across a version named, beside the withdrawal (AU); a rename dead on each side of Git's move, reached by the old name, the new name, the worktree's branch and with the worktree gone, with two renames taking the locks in opposite orders and a waiter on the old name (AV); an outcome after its ask changed, and a refusal across a restart (AW, AX).
- **The last look's cases**: a drop on another system, and one that leaves a conditional declaration; a patch applied on another system; scopes that differ only in the minor version or the tools; a hold from before against an update with every patch applied (AP, AR, AS); a stronger check saved between a submit's plan and its first transaction, and a person's own `submit` over a kept check, with the ship that follows (AT); a pending statement across `revbump`, `edit`, a checksum refresh, a file changed by hand and a commit made with Git (AU); a rename cut short reached through `Engine.Branch`, serve's recovery and a pull request's number (AV); each row of the outcomes' table, with `--shared-release` after a refusal, a target added after the dependents, and a flag withdrawn after its edit (AW).
- **The confirmation's cases**: two scopes each with a part unknown, which never compare equal; a changed path under `_resources` and one in a directory with no edit, at recovery and at a statement's binding, and a commit that changes no file; a request saved while a ship finds its work delivered, and a branch with no publication after a refusal; a person's `submit` going on with a stage's publication; each shaping flag changed after its edit, asserting the outcome stored and whether anything is delivered.
- **After a restart the effect is asserted**: the files, the record, the queued plan, what the stand-in forge received. Two requests that compare equal show nothing.
- **The boundary test** and the four tests that each command reaches the resolver.
- **The upgrade**, against a database made by the code before it: edits with and without `Held`, a committed branch of serve's, and one written and never committed.
- **The fault sweep** ([#129](https://github.com/herbygillot/dockhand/issues/129)): a failpoint after a release is saved, after step 3, after each file written, after step 5, after the targets are saved, after `RenameTo` is saved, between the Git rename and the name's save, at `ship.committed`, after the check is queued, after `Publish` moves at the handoff, and after the request ends; with the store's two unknown outcomes at steps 3 and 5. A second run after any of them ends as the clean run does, with one edit for each port and no hand edit said. Where a continuation rightly stops for a person, the scenario declares the stop and its sentence, and asserts nothing was overwritten.
- The regressions of #116, #156, #169 and #198 stay.

## How it is built

Each step is green on its own. A schema step lands with the readers and writers that need it, never ahead of them. [#125](https://github.com/herbygillot/dockhand/issues/125) lands before step 1: "kept this context's checksums" is one of the findings the result records.

1. **#194, the pieces**, with no caller: the fields and their store operations, the two readers and the two ways to add an edit; the function from an edit and its files' state to what settling does; the findings mapped from `Update`, the final declaration list with its scope from the preparer, and which rejection stands; `fromEdits` with modes; `Preparation`'s new inputs and its fourth answer. Unit and store tests.
2. **#194, switched on, in one commit**: the schema step, behind #191's wait for quiet, with every writer of an edit, every reader, both witnesses and the migration together, since an old writer left live for one commit would make its edits vanish; the update's new order; the rename's, with `Resolve` reading the intent; settling in every reader, and the unfinished verdict asked at the check's proposal and the submission's plan; the targets saved by the three callers that have them today, with a release saved when it is selected; `Held`, `SetHeld` and `holdBeforeCheck` gone; recovery by the checkpoint and tidy's chain, at each look; the notices list (#180's first part); its failpoints; the usage pages. **#194 closes here**, with the review's cases for F1 to F3 passing.
3. **#195, the pieces**, with no caller: the request's other fields, the resolver, the stages' requests built from it, the table, and the walk.
4. **#195, the real preparer** plans the actions after a current version, with #16's selector. The Tcl fixtures. No command reaches it yet: update's callers keep today's behaviour until step 5, and a test shows they do.
5. **#195, switched on**: update, bump, ship and serve through the resolver; the request kept and read, with its ID and the publication's keeping of it; the outcomes saved, removed with their asks, and a refusal blocking; the title, the note and the tested statements saved from the start; `check` and `submit` reading it; the handoff to the publication, compared by the ID the submission was judged under; `ship --plan` from the value; the old builders and the four exceptions gone; the boundary test; the usage pages. **#121 and #195 close here.**

## Not checked

- Read, not run: everything in "Today". No test was run for this file. Codex ran four existing tests and one probe of its own for its proposal, at 2bdf27a06: the probe showed an update whose record fails leaves files that the second update calls current and tidy calls a hand edit.
- Codex ran four probes of today's code for its review, in a copy of main 3e091339b: recovery's first content test passing a person's edit; a file outside an edit differing after the write; a move followed alone checking no patches; and a rename whose save was lost returning success. Nothing of this design is built, so none of its own rules was run.
- That `ApplyToWorkingFiles` can be given a subset of an edit's files, and that a blob reader exists for tidy's use that settling can share.
- That every unattended commit leaves a tidy checkpoint, a first commit on a new branch included. That a lint fix or a taken file always fails `fromEdits`: read from the tree it is handed (tidy.go:285-290, :431), not run.
- #191's page for each way a publication ends, against the five rows here: read, not run.
- What `--shared-release` does on a port with no sibling today, and what `--keep-old-checksums` does where no checksum changes.
- How the edits' files are stored, and so whether the two modes are a column or a field of what is there.
- #16 and #162 are not built. This design leans on what their issues say they return.
- That every command reaches a branch by its ID before it takes the name's lock, which the rename's rule needs. Whether anything takes the head's lock and then the branch's, against the order this file sets.
- `coversAsked` beyond its call (submit.go:731), and what a person's own `submit` does today where the evidence falls short of a check that was asked.
- That the preparer can read the port's final declaration after a drop and a refresh on every path that makes an update edit, and that the evaluator can name the Darwin version in full and the developer tools it saw. A Portfile can still test what no record names, a file or the environment; equal scopes are taken as comparable all the same.
- Codex's last look ran one probe, of `portfile.DropPatch`, in an archive of main 23b41575a. The whole preparer and a second platform were not run, by it or for this page.
- Codex's confirmation ran nothing, and read main fb3e42036. How a submission's plan tells that nothing is left to do for the delivery now asked was not read in #191's page beyond "Ending a publication".
- That every command that changes a branch can be brought to its lock before its first use of the name in Git: the three ways to a branch named under "The lock, and a rename" were read, and no search for a fourth was run.
- Codex's second pass ran four of today's tests in an archive of main fce0515a0, the drop fixture among them with one log line added. Its traces of the request's ID, the pending statement, the rename and the outcomes are inferred, as this page's answers are.
- What #191's page says of a submit that goes on with a recorded publication, against the handing over of the ID here.
- Which pairs of flags the commands refuse today, beyond the three read for this file.

## Usage docs

- `docs/usage/flags.md`, new: the flag check's table, kept equal to the code by a test.
- `docs/usage/branches.md`: an update that was cut short and how the next command finishes it; `--source` and the side actions on a port already current; a flag with nothing to do; `--closes` alone.
- `docs/usage/batches.md`: what ship keeps from one run to the next; the release it goes on with; `--to` kept; the flags kept for a later stage; the notices listed as stages pass; `ship --plan`'s unresolved lines; `--passing` and a branch kept below submit.
- `docs/usage/checks.md`: `check` reads the place and tests kept on the branch where it is given none; `--on` and `--build` replace each other; a baseline takes nothing kept; the hold before a check for `ship --yes` and `bump`, whoever ran the update.
- `docs/usage/publishing.md`: `submit` reads `--draft`, `--type` and `--skip-notification` kept on the branch; where they pass to the submission; a tested statement given for one version and dropped at the next.
- `docs/usage/recovery.md`: files written with their record lost, partly written, or changed in between, and the message for each; an update that wasn't finished and what stops for it; a flag that was refused and what changes it; a rename cut short, found by either name; what serve takes up again and what it leaves.
- `docs/usage/unattended.md`: serve continues its own unfinished preparations at each look; a branch a person changed is left and named; a patch's rejection holds until an update on the same system checks that patch again and finds it applies or gone, and otherwise until the person ends it; settings count from serve's next start.
- `docs/usage/hand-edits.md`: the `--source` paragraph, as #121 asks.
- `docs/architecture.md`: the edit's two records, settling, the verdict's inputs, the request and its resolver.
- Messages name the port, the release, the file or the flag, and the command to run next. None names a state of these records.
