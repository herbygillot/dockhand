# Architecture and code-organization review

Reviewed 2026-09-27 at `a71fc67f133553fd247a000adf9e759c7f915aff`.

This follows the [architecture and data-flow review](2026-09-27-architecture-and-data-flow.md) of the same day, after roadmap items 1 to 5 landed and item 6 began. It answers the six questions the person asked. Every finding was checked by three independent readers (code accuracy, design intent, materiality and remedy); where a reader narrowed a claim, the finding says so. Refuted findings are under Validation and limits.

## Assessment

The code is in good shape where the previous review asked for it: the plan is per environment, history changes are complete transitions, the provider contract is its own package, and v2 is gone. The engine's and the command's boundary tests hold their lines mechanically. `model`, `store`, `coord`, `history`, `git` and the forge adapters are coherent packages whose remaining problems are small.

Three moves matter most. First, the engine assembles its collaborators lazily without the lock its own comment promises, while serve drives runs concurrently; taking the lock is fifteen lines (finding 1). Second, the only channel progress reports have is a context reporter that nothing in production installs, so multi-minute waits and several by-hand warnings from the editor are silent; a stderr reporter in command is a documented obligation and a precondition for the roadmap's outdated-progress item (finding 31). Third, two rules the design promises live only in the command layer: one check per branch, which `submit --check` and `retry` bypass, and the capture stability check, which `--include` disables (findings 2 and 43).

On the six questions. Logic concentrates in `command` composing engine workflows, and in three long functions (`PlanCheck`, `server.run`, `PlanTidy`) whose phases have no names. Repetition is mostly small helpers and adapters copied across the command/engine seam; two copies have drifted with effect (upstream's overridden livecheck path, Tart's SSH readiness loop). The package map needs one deletion (five hundred dead lines in `git`) and no split by size; two proposed splits are declined on evidence. The missing concepts are a per-cell evidence verdict and one read-back helper for uncertain commits, not new packages. Unread data is mostly deferred for item 6; three losses are real (Go and Cargo updates lose the upstream comparison, the journal grows unbounded, a cancelled `create` reports nothing). Most findings are smaller items for when their area is next touched; the order at the end puts the few that are not first.

### Size and dependency map

Non-test lines under `internal/` and `cmd/` total 52,672. Packages over 1,000 lines, with fan-in for the most-imported:

| Package | Lines | Fan-in | Note |
| --- | ---: | ---: | --- |
| `internal/engine` | 9,763 | | +434 since the previous review; 31 internal imports |
| `internal/command` | 6,922 | | the CLI and the composition root |
| `internal/macports/portedit` | 3,025 | | delegates to seven subpackages |
| `internal/git` | 2,764 | 15 | about 503 lines have no production caller |
| `internal/macports/portindex` | 2,013 | 6 | construction and reading in one package |
| `internal/tart/provision` | 1,525 | | |
| `internal/model` | 1,513 | 31 | imports nothing |
| `internal/tcl/syntax` | 1,503 | 11 | |
| `internal/store/sqlite` | 1,361 | | |
| `internal/macports` | 1,354 | 21 | about 570 lines are the shared contract |
| `internal/macports/dependency` | 1,188 | | |
| `internal/upstream` | 1,172 | | |
| `internal/buildenv/tart` | 1,086 | | |
| `internal/macports/portedit/observe` | 1,049 | | |
| `internal/macos` | 1,003 | 9 | about 700 lines serve one importer |

The engine grew by 434 lines since `e8e62eb4` although item 4 removed 410: items 1 to 3 and 6 added more than the seams took out. The most-imported packages are where a cut has consequences; two of them carry code most importers never use, and the roadmap's rule against splitting by size is right for both.

## Findings

P1 is a plausible correctness or data risk, or a structural cost that grows with every feature; P2 is a defect or cost worth its own commit; P3 is taken when the area is next touched. Citations are `path:start-end` at `a71fc67f`.

## 1. Logic concentration

### 1. [P1] First-use assembly is unlocked, and composition is split between engine and command

`Engine` has seven "X when nil" collaborators and a `lazy` mutex whose comment says it guards "what the engine assembles on first use and serve's concurrent runs share" (internal/engine/engine.go:57-87). Only `forge()` takes it (internal/engine/forge.go:38-40). `preparer()`, `selectionReader()`, `portReader()`, `outdatedReader()` and `projectReader()` check-then-set engine fields with no lock (internal/engine/preparer.go:34-44, 60-72; internal/engine/ports.go:22-32; internal/engine/outdated.go:68-78; internal/engine/create.go:244-250). The race is reachable: serve runs `e.Resume` per admitted run in a goroutine (internal/engine/serve.go:204-210) while its loop runs the outdated scanner (240-247); a run reaches `selectionReader` through the Tart provider's `Index`, bound to `e.PortIndex` (internal/command/settings.go:100; internal/buildenv/tart/provider.go:488-491; preparer.go:76-82), and the scanner reaches it through `outdatedReader()` and `Update`'s `preparer()`. Not observed racing; `make test-race` would flag it.

Composition is split around them: `engine.Open` builds only `Repo`, `Store` and `Repository` (engine.go:92-107) while `settings.open` builds the providers from engine internals (settings.go:86-128), and the system GitHub client literal appears at four sites (forge.go:42, preparer.go:48, create.go:246, internal/command/auth.go:29-31). Narrowed: the `workspace.Registry` duplication showed no cost; the structural move waits for item 6.

Impact: a data race under serve; four sites for one credential path.

Remedy. Now: `e.lazy` (or a `sync.Once` per field) in the five assemblers, and one constructor for the system GitHub client. With item 6: `settings.open` passes a `Dependencies` value built once (client, selection reader, one registry) to `engine.Open`, and the lazy getters go; the interface fields suffice.

Roadmap: new for the lock; the constructor is what the previous review's finding 4 recommended and item 4 dropped without recording why.

### 2. [P2] The check request is composed in command, and decision 29's guardrail lives only in `check`

Environments, Capture, PlanCheck, Runnable, Enqueue is written in `check` (internal/command/check.go:73-111), `submit --check` (internal/command/submit.go:197-215) and `PrepareOutdated` (internal/engine/outdated.go:230-247). Only `check` calls `replaceActive`, the one-check-per-branch rule (check.go:107, 682-700); `Enqueue` records without checking (internal/engine/runner.go:32-51), so `submit --check`, `retry` (internal/engine/verbs.go:88-107) and serve's updates queue a second check of a branch that `check` refuses. `--tests` is cast unvalidated (check.go:84) and caught late by `Plan.Validate` (internal/model/plan.go:284-288). `submitChecked`'s commit-binding guard (submit.go:226-231, no engine test) and `editSubjects`' edits to `TidyGroup` from outside the plan (internal/command/review.go:255-280) are two more rules in command, which `command/doc.go:4-10` places in the engine. Narrowed: the three "can't run" wordings fit their outputs; `coord` and `store` imports are allowed with reasons (internal/command/boundary_test.go:21-23); a single `RequestCheck` verb would not serve `check`'s plan printing and confirm.

Impact: the documented rule is bypassed by two verbs; a misspelled `--tests` runs advisory.

Remedy: `engine.Enqueue` refuses an active check with a typed `ActiveRunError{Run, SameRevision}`, the command keeping the prompt; `PlanCheck` validates `PlanRequest.Tests`; commit binding becomes a `SubmitRequest.BoundTo` field enforced in `PlanSubmit`/`ApplySubmit` with an engine test; `TidyPlan.SetSubject(i, s)` beside `Regroup` (internal/engine/tidyplan.go:22). When `check.go` is next touched, `report` returns its baseline candidates so RunE (check.go:118-130) stops recomputing them.

Roadmap: new; item 4 declined splitting command by size, not moving a guardrail. Merges three confirmed findings.

### 3. [P3] `server.run` is a scheduler and a dispatcher, and its workers copy "say once"

`run` (internal/engine/serve.go:151-314) builds capacity, defines `launch`/`settle` closures, fences the lease and dispatches four workers inline (240-247); `settle` resets the submitter's timer (238). Each worker hand-rolls "say a problem once" differently, two never clearing on success (321-349, 358-382, 417-440, 511-531), and the daily scan runs on the loop goroutine, so a finished run parks on the unbuffered `done` until `PrepareOutdated` finishes (192, 209). Narrowed: admission is tested end to end; per-worker goroutines would race on `submitter.last`.

Remedy: one keyed once-reporter for the four workers; the scanner's body on a goroutine behind a busy flag if the stall matters; `status.Held` consulted before `PlanSubmit` in `ServeCandidates`. No scheduler type.

Roadmap: new.

### 4. [P3] `PlanCheck` is 230 lines of positional per-environment state

`PlanCheck` (internal/engine/plan.go:58-287) is the longest function in the tree: its evaluation closure mutates `plan`, `candidates` and `evaluations` (98-135); reasons, built, needs and union are index-parallel maps (169-212); the receiver is shadowed at 99, 170, 196, 225; and the port name travels as `PlanTarget.ID`, `Target.Name`, `Unmet.Target` and `Exclusion.Target.Name`, bridged by casts (plan.go:109, 456, 463; internal/model/plan.go:380) that `Validate` never checks. Narrowed: narrowing, ordering, unmet and eligibility are already pure functions (440-523, 295-341); `PlanTarget.ID` is persisted JSON, so no re-keying.

Remedy: name the per-environment evaluation type and extract reasons, needs/union and merge as pure functions, doing the evaluate phase as item 6's first commit since its input becomes the evaluation report; a `Validate` clause asserting `target.ID == TargetID(target.Target.Name)`.

Roadmap: bound to item 6; the 2026-09-27 double-counting regression (fixed at plan.go:143-161) shows the shape's cost.

### 5. [P3] `PlanSubmit` threads one 23-field struct through four phases

`evidence()`, `destination()`, `title()` and `searchOthers()` (internal/engine/submit.go:189-196, 218-277, 279-357, 395-433) set fields on a shared `SubmitPlan` (51-90); the last two read `Existing` that `destination` set, so the order is unstated. The publication rule is already pure (`publicationProblems`, internal/engine/evidence.go:424-445); `evidence()` adds flag handling and two store reads (231, 261-269) that `PlanSubmit` repeats (200-208). Narrowed from a "next seam".

Remedy: hoist the two reads into `PlanSubmit` and fold the `--accept` validation (246-258) into `publicationProblems`; about forty moved lines.

Roadmap: new; the previous review's finding 4 named "publication coverage" and item 4 closed without it.

## 2. Logic repetition

### 6. [P2] upstream's two livecheck paths are one copy, and the copy has drifted with effect

`discoverListing` (internal/upstream/http.go:30-52) and `discoverOverridden` (internal/upstream/livecheck.go:33-55) are the same 23 lines (two differ), reached only from `DiscoverPort` (internal/upstream/latest.go:42, 52). The block after them differs: http.go:55-68 keeps the candidate's version when already current; livecheck.go:86-95 always records `port.Version`, so its `Release` carries `Version: port.Version` beside an older candidate's `Tag`/`Commit` (livecheck.go:56-57, 97) whenever `comparison < 0`, the backport case the design note names. `Evidence` is written at three sites (latest.go:180, http.go:75, livecheck.go:99) and read only by tests. Narrowed: the two `automatic()` guards test different strings (latest.go:48; livecheck.go:34) and both stay; the tag lookups and `Release` literals have their own semantics.

Remedy: one `livecheckNewest()` covering the pipeline and the discovery pair's evaluate-and-refuse block, so the overridden path gets http.go's rule; pass `DiscoverPort`'s `Result` down and drop the inner defers; a `comparison < 0` test. One commit before item 7 edits discovery.

Roadmap: new.

### 7. [P2] Tart's two `native` machines copy the VM plumbing, and one copy lacks the SSH refusal check

Both copy the overwrite refusal (internal/tart/host/machine.go:138-147; internal/tart/provision/vm.go:54-58; adopt.go:62-66; connect.go:200-204), the tolerant Stop (internal/buildenv/tart/machine.go:110-116; vm.go:165-174) and the SSH readiness loop (machine.go:128-147 versus connect.go:60-87). The loops differ where it matters: `channel` maps every ssh exit 255 to `ErrTransport` (internal/tart/channel/channel.go:83-87); provision's loop returns at once on "Host key verification failed" or "Permission denied" (connect.go:68-70); the provider's retries for its full wait (provider.go:398) and drops the output. Narrowed: the roadmap's Tart workarounds already live once each; provision bypasses `host.Machine.Clone` for reasons; reshaping `Provisioner.Run` is disproportionate.

Remedy: one `AwaitSSH(ctx, guest, run, wait)` carrying provision's output checks, its own commit; `host.Machine.Clone` taking a caller-supplied guard and progress writer, with `absent` exported; one tolerant `host.Machine.Stop`; a `conforms(checked, config, release)` for provision's three comparison blocks (provision.go:330-343, 442-448, 499-504).

Roadmap: new.

### 8. [P3] Branch selection: one prologue copied, four identical lines, `create` without the adopt hint

`chooseBranch` (internal/command/author.go:432-448) and `revbumpBranch` (internal/command/verbs.go:134-150) share ten lines including the `e.Repo.CurrentBranch` master/main guard (author.go:446; verbs.go:143); "Started %s from master %s" is printed at author.go:215, create.go:74, verbs.go:41, 98; `create`'s default arm (internal/command/create.go:76-79) refuses on `ErrNoBranch` generically while `Current` names the adopt hint only in text (internal/engine/branches.go:319-330), so on an untracked branch `update` prompts and `create` refuses. Narrowed: `workingBranch`, `Path`, `status` and `watch` are deliberate policies.

Remedy, about thirty lines: one command helper wrapping `Current` plus the guard for the three callers; one `startedLine`; optionally a second sentinel under `ErrNoBranch`.

Roadmap: new; take with item 7's `create` work.

### 9. [P3] Two commit-message composers, one dead

`commitmsg.Compose`/`Rewrite` (internal/macports/commitmsg/message.go:57-133) have no production caller: `Compose`'s only caller (internal/macports/portedit/message.go:9) is itself uncalled since `df281067`. The live composer is tidy's (internal/engine/tidy.go:428-487), although architecture.md:64 says tidy's messages come from `commitmsg`; `create` records "<port>: new port, version X" (internal/engine/create.go:189) where `derivedSubject` proposes "<port>: new port" (tidy.go:319-323). Narrowed: `RevbumpLinked` already reaches `commitmsg.Subject`; the PR body keeping trailers is a product choice.

Remedy: move tidy's composer into `commitmsg`; delete `Compose`, `Rewrite`, `lastParagraph`, `citesTickets`, `portedit/message.go`; align `create`'s subject with design v3 §8.

Roadmap: a leftover from item 5's sweep.

### 10. [P3] `--json`: a branch in three shapes, no baseline marker, eighteen map results

A branch is `branchJSON` in status (internal/command/json.go:308-325), `branchRefJSON` in start/adopt/edit/create/update (internal/command/json_results.go:12-21), and a bare string in eight other commands (json.go:278, 398, 427; json_results.go:153, 184; internal/command/verbs.go:214, 264; internal/command/review.go:339). A baseline's JSON is a `checkJSON` with no marker (internal/command/check.go:555-573), and eighteen results are `map[string]any`. Narrowed: the 27-name list fails safe (root.go:100-107); `omitempty` on `branchJSON` would change status's shape within version 1.

Remedy: `baseline_of` (omitempty) on `runJSON`; the pairing on `checkJSON`'s targets; `branchRefJSON` embedded untagged in `branchJSON`; typed structs for the eighteen maps. The string-versus-object split is the person's decision.

Roadmap: new; item 1 treated the same class as a defect for `update --json`.

### 11. [P3] Run recipes and the finished-checks rule are copied

`enqueue` and `Retry` share thirteen lines (internal/engine/runner.go:32-51; internal/engine/verbs.go:88-107); `Capture` and `baseRevision` scan `tx.Revisions` with different predicates (internal/engine/capture.go:122-141; internal/engine/baseline.go:247-264); "finished and not a baseline" is spelled three ways (internal/engine/evidence.go:218-237; baseline.go:178-188; internal/engine/status.go:153-165); the run-name grammar twice (internal/command/queue.go:253; status.go:250-262). Narrowed: no duplicate row follows.

Remedy: a `newRun` helper; `baseRevision` uses `Capture`'s predicate; `RunState.Finished()`; the command calls the engine's parser.

Roadmap: new.

### 12. [P3] `tidy.go` hosts the commit-rules adapter, and tidy applies two port rules

`ruleCommits`/`portfileFindings` (internal/engine/tidy.go:534-561) serve tidy, review and submit (internal/engine/review.go:100-101; internal/engine/submit.go:177-178). In `PlanTidy`, `ruleCommits` classifies ports with `ScopeOf` (Portfile or files/ only, internal/engine/scope.go:13-43) while grouping uses `portPath` (any file under the port, tidy.go:328-337); for a group with no CI-scope change, tidy accepts a combined subject only with the "port:" prefix (tidy.go:235) while `commitrules` skips its rule when `Ports` is empty (internal/macports/commitrules/rules.go:110). Narrowed: the block appears only past the Keep gate (tidy.go:160); unifying the rules would split a README from its Portfile commit.

Remedy: at tidy.go:235 accept any combined subject when `ScopeOf(group.Paths).Ports` is empty, with a test; move the adapter beside `ScopeOf` and export one `Plural` when touched.

Roadmap: new.

### 13. [P3] GitHub remotes: two parsers, dead constants

`remoteFor`, `theirRemote` and `fork` (internal/engine/clean.go:277-288; internal/engine/submit.go:358-370, 617-648) use `NameFromRemote`; `UpstreamRemote` uses its own `namesRepository` (internal/engine/engine.go:234-257), which accepts `http://` and `git://` where `NameFromRemote` accepts https and ssh only (internal/forge/github/publication.go:17-36); `macports.PortsRepository`, `PortsRepositoryURL`, `PortsBranch` (internal/macports/source.go:5-8) duplicate engine's constants and have no callers. Narrowed: a `git://` upstream remote changes only init's sentence.

Remedy: lift `NameFromRemote`'s body to a package function used by `UpstreamRemote`; delete the dead constants.

Roadmap: new.

### 14. [P3] Environment wording and provider names spelled ad hoc

Five describers plus two aliases live in the run driver file (internal/engine/runner.go:662-748; internal/command/check.go:360-362); `describePlace` calls `macos.Describe` then rewrites "darwin " (733-736); `releaseWords` and `tart.ReleaseForPlatform` share one core (runner.go:711-721; internal/tart/release.go:12-25). Provider names are literals at eight sites (internal/engine/provider.go:28-62, 73-79; body.go:266-276; clean.go:179; config.go:198-216; settings.go:86-104; providers.go:133-144); v2 had typed constants. Narrowed: every wording caller goes through `DescribeEnvironment`; `model` cannot hold macOS words.

Remedy: delete the aliases; split `describePlace` into `platformWords` plus prefix; one macOS release-to-words helper; one name constant per provider.

Roadmap: distinct from the "one image descriptor" item.

### 15. [P3] Exec admission is two parsers over one table

Go parses and judges exec lines (internal/macports/programs.go:167-288); Tcl re-implements parse/pipeline/program/admitted (internal/macports/eval/dispatcher.tcl:154-260); only `HostPrograms` crosses (internal/macports/eval/evaluator.go:98-99), and the tests are separately maintained literals (programs_test.go:66-76; refusal_test.go:47-55). Tcl admits a program the fresh installation lacks (dispatcher.tcl:211-216, deliberate); Go refuses computed words (programs.go:233-234, 253-254, 280-281). Narrowed: two judges over one table is the documented design (docs/oracle.md:47-52); every known difference makes Go stricter.

Remedy: in `eval/refusal_test.go` one table run through both `CommandLineRefusal` and `evaluateLines`, computed-word rows Go-only; two sentences in `programs.go`'s note.

Roadmap: fits the oracle's remaining phases.

### 16. [P3] `forge/github` repeats its guards and preamble

The ref guard opens five methods (internal/forge/github/pullrequests.go:74-76, 180-182, 241-243, 267-269; inspect.go:18-20); the `API`/`RateLimitError`/`Cut` preamble opens eighteen; File and CommitTime guards are byte-identical across github and gitlab (files.go:18, gitlab/files.go:17; commits.go:19, gitlab/commits.go:18). Narrowed: Observe's unmapped 404 matches the contract; a forge-level `Validate` would cycle.

Remedy, package-local when touched: a `validRef` one-liner and a ghactions-style `api(ctx, name, authenticated)` preamble for methods without a git fallback or `publicationError`.

Roadmap: new.

### 17. [P3] Small helpers copied across packages

`plural` (command/work.go:272-282 = engine/tidy.go:800-810), `short` (engine/branches.go:386-391 = history/history.go:313-318), `firstOf` (command/settings.go:37-44 = engine/body.go:257-264; `cmp.Or` on Go 1.27), `sameText`/`normalize` (command/submit.go:437-440; body.go:342-344), the editor launch (command/verbs.go:55-59; submit.go:512-516), the run and checkpoint name parsers apart from their `Name` methods (engine/status.go:200-262; tidy.go:688-692), and `ExitCode`'s bare type assertion (command/exit.go:17-22) where callers use `errors.As`. Narrowed: no `words`/`homedir` package.

Remedy, each a few lines: `cmp.Or`; `model.ObjectID.Short()`; `model.ParseRunName`/`ParseCheckpointName`; a local `openEditor`; the second "Did you test" prompt folded into `askTested`; `errors.As` in `ExitCode`.

Roadmap: new.

## 3. Package size and splits

### 18. [P3] About 500 lines of `git` have no production caller since v2

`Transplant`, `ReplaceContribution` (internal/git/correction.go:14-133), `CaptureCheckout` with `checkoutIndex`, `captureFiles` and the `Checkout` type (internal/git/worktree.go:22-96, 119-216), `RequireCleanBranch` (internal/git/branch_clean.go:9-53), `CheckContributionBase`, `FirstCommitAbove` (internal/git/remote.go:88-122, 187-201), `AddedOrModifiedPaths`, `UnstagedPaths`, `CommitMessage`, `SingleParent`, `WithPushLock`/`WithRemoteBranchLock`, and the checkout-based `Rebase` (internal/git/history.go:319-337; the engine uses `Replay`) total 503 of 2,764 lines, several kept alive only by their tests, with messages naming v2 verbs (worktree.go:126). `make deadcode` passes `-test` (Makefile:50-51) and CI never runs it. Also uncalled: `engine.PortIndex()` (internal/engine/preparer.go:74-82), `Engine.Next`, `coord.Tail`, `store.Reader.Sessions`. Narrowed: `UpdateBranch`'s raw `CreatedAt` compare (records.go:126, against the millis-normalized compare at coordination.go:67) is a separate latent fix.

Remedy: one commit deleting the fourteen methods and the `Checkout` type with their tests, keeping `checkoutHead` and adding a `MoveCheckout` case for "no branch checked out"; reword `doc.go`; run `deadcode` once without `-test`, which the roadmap's maintenance rule should say.

Roadmap: item 5's note says nothing else is dead; the code disagrees.

### 19. [P2] `preparation` mirrors `portedit.Result`, and the engine edits Portfiles after preparation proved them

`preparation.Result` (internal/preparation/preparation.go:28-47) repeats `portedit.Result` (internal/macports/portedit/prepare.go:74-97) field for field, copied in a named literal at preparation.go:157, 166-173, where a forgotten field zeroes silently (commit `62abfd4a` made three edits for one new fact). `stealth.go:44-102` then calls `BumpRevision` and `StealthDistSubdir` on the prepared files and rewrites `PreparedTree` with `EditTree` only (130-136), so `Prepared` and `Fidelity` describe the pre-stealth tree and `Update` infers `Subject` and `After.Revision++` (internal/engine/update.go:179-184): the fidelity proof does not cover the tree the engine records. Narrowed: the aliases and re-exports stay; the branch-base gate (stealth.go:49-58) needs Git and stays in the engine.

Remedy: embed `portedit.Result` in `preparation.Result`, adding only `Files` and `PreparedTree`; fix `preparation/doc.go:6`, which names the deleted `Workflow`; move stealth detection and the two Portfile edits (stealth.go:60-95) behind a `portedit.Request` field like `KeepArchives`, so the checksums path proves the revbump under fidelity. Sequence the stealth half with the previous review's finding 6.

Roadmap: the alias was flagged in the 2026-09-17 and 2026-09-22 reviews; the stealth half is new.

### 20. [P3] `outdated` and `preparation` are thin packages over the same parts, with dead helpers

`outdated.Service` (internal/outdated/outdated.go:97-109) and `preparation.Service` (preparation.go:58-68) share four fields, both build a `portedit.Service` and bind upstream to a probe (outdated.go:137, 193-211; preparation.go:70-76, 101-125), and the engine calls `discovery()` twice (internal/engine/preparer.go:42; internal/engine/outdated.go:76). `Headline`, `Incomplete`, `OutOfDate`, `Hidden`, `Note` (outdated.go:35-93) have no non-test caller while `writeOutdated` recomputes them (internal/command/outdated.go:80-112); `engine/outdated.go:80` says "v2's survey". Narrowed: the fold is declined.

Remedy: delete the five helpers and the `Selection` alias; rewrite `preparation/doc.go`; one shared `workspace.Registry` across `preparer()`, `outdatedReader()` and `portReader()`. Ride with item 7.

Roadmap: new.

### 21. [P3] `macports`' root and `macos` carry mechanics most importers never use

`macports` (fan-in 21) holds `programs.go` (314 lines) and `platform.go` (281), read only by `eval`, `fetchguard` and `portindex` (internal/macports/eval/evaluator.go:98-147; fetchguard/effect.go:117; portindex/index.go:358-362); the shared contract is about 570 of 1,354 lines. `macos` (fan-in 9) is release vocabulary plus about 700 lines of guest setup whose only consumer is `tart/provision`, and there are three launchd plist writers (internal/macos/launchd.go:16-35, `KeepAlive` false and no `ProcessType`; internal/command/serve.go:232-278; internal/tart/provision/agent.go:15-30). Narrowed: the release-scope trio's placement was decided the same day; `installation` is target-agnostic by design; the guest-op files import only stdlib.

Remedy: one subpackage for `programs.go` and `platform.go`; `macos.LaunchdPlist` gains `KeepAlive`/`ProcessType` parameters so `serve.go`'s writer uses it. No `macos/guest`, no move of `installation`.

Roadmap: new; both fall under "not by size alone".

### 22. [P3] The history verbs' forward and recovery halves sit in two packages

tidy, rebase and restore (internal/engine/tidy.go, tidyplan.go, verbs.go:109-211; 1,217 lines) reach nine engine members, six of them `history.Transitions`' fields (internal/history/history.go:31-41). Their forward Git choreography (tidy.go:648-676; verbs.go:187-209; tidy.go:768-796) is recognized per kind by `settle` and `finishIndex` in `history` (history.go:190-273), so a change to a verb's Git shape needs a mirrored change there; architecture.md:65 says the transition "carries out" the change, but the engine does. Narrowed: moving the verbs relocates about 1,200 lines plus tests and forces five engine-wide helpers beneath both packages.

Remedy: finding 26's `Transitions.Make`, which puts ref creation beside its recognition; record the nine-member evidence under item 4 as the reason the verbs stay; file rebase and restore in their own engine files when touched.

Roadmap: prior-review-declined (item 4's bullet), re-opened with evidence for the person.

### 23. [P3] Two splits not worth taking now: `store`'s interfaces and `portindex`

`store.Reader` (24 methods) and `Tx` (25 more) are one contract on one `*tx` receiver across three files; `history` and `coord` each call nine methods, but `history.Settle` writes checkpoint, branch and journal in one transaction (internal/history/history.go:89-105) and `Session.Fenced` hands the whole `Tx` to the engine (internal/coord/coord.go:283-290), so split interfaces would be re-embedded at both consumers. `portindex`'s reading side (about 720 lines) touches nothing of the staging side; but it uses three constants from index.go:29-32, its importers already reach `git` directly, and item 6's report grows `eval` and `engine`, not `portindex`.

Remedy: delete `Sessions()` or leave it; take the `portindex` split only in a change that grows its reading side.

Roadmap: `store` prior-review-declined (2026-09-23); `portindex` new.

## 4. Missing concepts and new packages

### 24. [P2] The uncertain-commit read-back exists only inside `history`; two writers undo Git on it

Item 3's contract (write; on `store.ErrUncertain` read a witness back) is implemented three times in `history` with three predicates (internal/history/history.go:75-79, 106-110, 139-143) and nowhere else. sqlite wraps every non-conflict COMMIT error as `ErrUncertain` and passes a cancel through (internal/store/sqlite/sqlite.go:273-285, 325-341), so a Ctrl-C in the fsync window produces it. `Start` and `AdoptPullRequest` undo the Git branch and worktree on any `Update` error (internal/engine/branches.go:117-126, 493-503); if the commit landed, the row stays open, `BranchNamed` blocks the name for every non-merged state (internal/store/sqlite/records.go:49-51) and nothing removes the row once its worktree is gone (internal/engine/clean.go:239-241): a permanent ghost. `Update` and `Create` leave files applied and may lose the edit record (internal/engine/update.go:203-231; internal/engine/create.go:171-203). Narrowed: the coord lease leg does not starve a run.

Remedy, two commits: a `store.Recorded(ctx, s, repo, write, witness)` helper running `Update` and, only on `ErrUncertain`, a witness in a `View`; first `Start`/`AdoptPullRequest` (never undo on `ErrUncertain`; read the branch back), then `Update`/`Create` and history's three sites; a one-line retry of `Acquire` on `ErrUncertain`. Record the policy in `store`'s package comment.

Roadmap: extends item 3, whose contract was written for history only.

### 25. [P2] A cell of the evidence matrix is a bare `Outcome`; consumers re-ask the plan

`runEvidence` builds the matrix positionally, synthesizing `NotRun` for excluded cells and `Unmet` from the plan without an `Execution` (internal/engine/runner.go:605-623). Because "excluded" is a synthetic `NotRun`, `Excluded(plan, target, env)` is re-asked at seven sites (runner.go:395, 608; evidence.go:288, 361, 412; words.go:17; command/json.go:269) and `UnmetIn` re-looked-up for cells already Unmet at three (runner.go:406; words.go:20; evidence.go:451); `Remade` was added as a side channel `settle` reconciles (406-409). The `--also` exemption is spelled in `publicationProblems` (evidence.go:430-435) and `attentionFor` (internal/command/status.go:205), but `PassingBranches` counts `Also` targets through `Failed()` (internal/engine/servesubmit.go:47). `Diff` re-runs the `CommitTrees` and `ChangedPaths` that `BranchStatus` computed and dropped (internal/engine/diff.go:47-52; status.go:120-139). Narrowed: `BaselineWorthy` switches on Phase; `RunEvidence` and `treeEvidence` differ by design; no `BranchStatus` split.

Remedy: a `Kind` on the cell (Excluded, Unmet, NotRun, Remade, Recorded) set by `runEvidence` and updated by `dropRemade`/`fill`, as item 6's first step on evidence, which already moves `Counts`; one named `--also` predicate; `BaseTree` and `Changed` on `BranchStatus` for `Diff`, `Impact`, `ArchiveDiff`, `LinkedPorts`.

Roadmap: bound to item 6.

### 26. [P3] A branch's change and the checkpoint transition are choreographed by hand

Ten functions compute changed paths and scope against a base inline (internal/engine/status.go:120-139; submit.go:148-178; tidy.go:116-160; plan.go:71-80; review.go:81-101; branches.go:215-230, 466-481; update.go:379-391; tidyplan.go:193-223); `Adopt` and `AdoptPullRequest` are near-verbatim; `planSubmit` and `Review` run the identical assessment; submit's merge loop (submit.go:183-187) duplicates a `commitrules` error it already inspects. The Prepare, Step, Git change, Settle sequence is written in `applyTidy` (tidy.go:650-677) and `rebase` (verbs.go:187-210) while `Transitions` exposes no composite; plan staleness is written twice (tidyplan.go:197-213; tidy.go:608-617); `applySaved`'s gate (internal/command/review.go:296-298) repeats `ApplyTidy`'s (tidy.go:581-583). Narrowed: the per-verb differences are documented; a nine-field `BranchChange` through twelve functions is the god-struct shape the previous review warned against.

Remedy, each its own commit: `history.Transitions.Make(ctx, *Checkpoint, change func(ctx) error, message)` with rebase's kept-ref undo inside the change; `TidyPlan.StillApplies`; one `assess(ctx, repo, base, head, baseTree, tree, changed)` for `planSubmit` and `Review`; fold the two Adopt copies; delete `applySaved`'s gate and submit's merge loop.

Roadmap: a new angle on the previous review's finding 3, which landed without a single owner.

### 27. [P3] The evaluator's computed facts leave as option strings

`metadata_only`, `livecheck_standard`, `has_credentials`, `archive_compatible` and `base_version` ship in `PortInfo.Options` as "0"/"1" or raw strings (internal/macports/options.go:29-31; eval/evaluator.tcl:139-184); eleven sites compare literals (internal/macports/stub.go:32, 36; scope.go:23; fidelity/fidelity.go:270, 302; portsource/source.go:149; portsource/livecheck.go:25; portedit/archives/download.go:268-296; upstream/http.go:89), none through `PortInfo.Bool`. Only `metadata_only` swallows its probe failure (evaluator.tcl:139-151 defaults to 0 with no failure entry), so a failed probe on a stub skips `ResolveStub` and fidelity refuses with a misleading message. `decodeMetadata` attaches the typed fetch assessment and narrows it in the same block to an option string and an `OptionErrors` sentence (eval/evaluator.go:393-408) that `CheckPolicy` reads instead of `info.Fetch` (download.go:284-296) and `fidelity.Compare` diffs wholesale, line number included (fidelity.go:113-129). Two smaller instances: Base's version shipped per port for a User-Agent (http.go:86-91), and livecheck facts transplanted by key prefix (portedit/source.go:425-437). Narrowed: the other facts' readers check `OptionErrors`; the keys must stay in `Options` for `fidelity.Compare`; the fetch line-number risk is latent.

Remedy: record a failure for `dockhand.metadata_only` in Tcl's catch branch; route the six `metadata_only`/`livecheck_standard` sites through `PortInfo.Bool`; `CheckPolicy` reads `info.Fetch`, `decodeMetadata` stops writing the sentence, `Compare` gains a `Kind` check; carry the runtime on the bound probe; one `isLivecheckKey`. Fold the fetch and base-version parts into item 6's evaluation report.

Roadmap: the report is item 6; the `metadata_only` failure is new. Merges four confirmed findings.

### 28. [P3] Result vocabularies are checked in one provider, not at the write

Tart's `record` whitelists outcomes and checks neither the phase nor the tests vocabulary (internal/buildenv/tart/provider.go:631-642); script's `convert` checks both (internal/buildenv/script/script.go:200-224); `TargetResult.Validate`, which every write runs through `tx.RecordResult` (internal/store/sqlite/records.go:550-553), checks the failed-implies-phase rule and the outcome list only (internal/model/execution.go:185-202). Latent until the prefix provider. Narrowed: `Record` does validate via the store; no shared wire shape.

Remedy: add the Phase and Tests vocabularies to `TargetResult.Validate`, about eight lines.

Roadmap: new.

### 29. [P3] A port directory has no name; the "." and "_" rule is written nine times

"category/port" is derived by six rules with different fallbacks (internal/engine/scope.go:13-36; update.go:296-301; tidy.go:328-337; internal/macports/workspace/workspace.go:438-444; portedit/source.go:444-454; portindex/coverage.go:28-32), the Portfile shape four ways (context.go:135-138; workspace.go:186-192; selection/selection.go:53; eval/resolve.go:25-33), and the category rule at nine sites over three substrates (tree_check.go:25; resolve.go:47; portindex/selection.go:122; engine.go:207; create.go:230; verbs.go:53; scope.go:13; tidy.go:329; workspace.go:441). Narrowed: the impact examples do not reproduce; `model` cannot import `macports`.

Remedy, about thirty lines beside `ValidName`: `IsCategory(name) bool` and `PortDirectory(path) (string, bool)` replacing `portPath`/`groupOf`, `workspace.portDirectory` and `ScopeOf`'s `SplitN` core.

Roadmap: new.

### 30. [P3] Git without a repository runs in a temp dir a repository may enclose

`ListRemoteTags` runs `ls-remote` from a `Repository` rooted at `os.TempDir()` (internal/git/remote.go:228) although its doc promises no repository's configuration applies (211-213), and the env scrub strips `GIT_CEILING_DIRECTORIES` (repository.go:107). A verifier reproduced it: from a temp dir inside a repository with `url.insteadOf`, `ls-remote` was rewritten to the local path. Two worktree literals bypass `Open` (workspace.go:70; correction.go:27); `ExecutableVersion` builds its own Spec (version.go:29-43); `MaterializeInto` drives `exec.Cmd` directly (snapshot.go:126-144). Narrowed: the other three are cosmetic.

Remedy: run `ListRemoteTags` in a fresh `scratch.Dir` with `GIT_CEILING_DIRECTORIES` set to its parent through `command()`'s env argument, with a test enclosing `TMPDIR` in a repository carrying `url.insteadOf`; when touched, one `r.at(dir)` and one `defaultExecutable`.

Roadmap: new.

## 5. Data lost or unconsumed

### 31. [P2] No production code installs a progress reporter, and some reports are decisions

`progress.emit` returns when no reporter is on the context (internal/progress/progress.go:78-81); the only non-test `WithReporter` is tools/survey/main.go:107; `cmd/dockhand/main.go:15-17` and `command.Run` (internal/command/root.go:58-66, 192-196) install none. So the 48 emit sites under `internal/` write to nobody, including the "may take several minutes" line for a full PortIndex build (internal/macports/portindex/index.go:311-316). v2 had this sink with `-v`/`-vv`, deleted in `86813c9c`; design v3 §12 says progress goes to stderr. Four reports are the sole record of a decision: a port that requires a newer Go but declares no `go.toolchain_min`, and one whose declaration cannot be rewritten ("raise it by hand"), both then `return nil` (internal/macports/portedit/go_toolchain.go:65-67, 73-75); a git-fetched port's patches left unchecked (git_source.go:119-121), where `PatchProblems` carries only rejected patches; and Git crates left to online resolution (dependencies.go:223-224). `outdated` itself emits no count (internal/outdated/outdated.go:149-177, 193-211), and both callers discard the finished ports `Observe` returns on interrupt (internal/command/outdated.go:64-67; internal/engine/serve.go:445-449). Narrowed: provider milestones are journal events by design, so providers keep `Build.Progress`; the Cargo case is the port's pre-existing policy.

Impact: a person cannot tell a hung command from a slow one; a module-mode Go port whose minimum could not be rewritten passes check and serve submits it clean; the roadmap's outdated item cannot land without a sink.

Remedy: wrap ctx in `command.Run` with a reporter to `streams.Err` behind a persistent `-v`/`-vv`, porting the survey tool's sink, with `progress.Quiet` at `check.follow` when driving; a typed field beside `PreviousProblem` (`GoToolchain{Required, Declared, Problem}`) carried through `preparation.Result` and `engine.Update` as `PatchProblems` travels, and unchecked patches recorded as `patchcheck.Result{Checked: false}`; in `outdated.Observe`, a goroutine reporting the finished count every 15 s, and the partial report printed on cancellation. Whether a toolchain advice holds serve's auto-submit is the person's decision.

Roadmap: the outdated item is pending; the sink and the dropped decisions are new. Merges three confirmed findings.

### 32. [P2] Updates of ports with `go.vendors` or `cargo.crates` always lose the upstream comparison

The archive path honors `KeepArchives` and fills `Previous` (internal/macports/portedit/version.go:98-116); `prepareDependencyVersion` always builds a scratch store, never fills `Previous`, and appends Git-crate downloads (dependencies.go:175-180, 296-299). The engine sets `KeepArchives` for every `EditUpdate` with `CompareUpstream` (internal/engine/update.go:147-155), which `update` and `prepareOne` always pass, so `compareUpstream` reports "the versions have 0 and N distfiles, so they can't be paired" (update.go:444-446). `update` prints it; serve holds a serve-origin branch only on recorded Hold changes (internal/engine/status.go:140-150), which such a port can never produce, so the §11 hold is silently dead for this port class. Narrowed: Go ports with `go.offline_build no` take the archive path and compare correctly.

Remedy: keep the downloads `originalDependencySource` already fetches as `Previous` (dependency_source.go:34-41), in `s.Archives.Store(request.KeepArchives)` when set so the paths outlive the scratch `RemoveAll`, over the stripped `base` since `archives.Sources` refuses vendored ports; Git-crate downloads in a `Crates` field; a regression test that a `go.vendors` port with `CompareUpstream` yields no Problem.

Roadmap: new; the feature landed this cycle (`c4a91e80`).

### 33. [P2] Command output diverges from what the engine did

`Create` writes and stages the Portfile and records the edit and event, then refreshes checksums; on cancel it returns `(created, ctx.Err())` (internal/engine/create.go:192-208), the command returns before `emit` (internal/command/create.go:95-99), and the envelope carries an error with a null result although a port exists; a retry is refused by `refuseExisting`. `Adopt` returns before computing `Commits` and `Scope` on the already-tracked path and never sets `Scope` on the renamed path (internal/engine/branches.go:176-213) while the view emits both (internal/command/work.go:187). `rebase` counts with `CountCommits` before `Replay` drops commits master has (internal/engine/verbs.go:168; internal/git/history.go:307-310), and its up-to-date branch calls `SetBase`, which always journals "rebased X from master A onto A" (internal/history/history.go:159-174), even when the recorded base already equals master. Narrowed: the tidy `unambiguous:false` for a loaded plan is conservative and unread.

Remedy, each a few lines: emit `createdView` before returning the cancel error when `created.Port != ""`, the idiom check and revbump already use; compute `Commits` and `Scope` on the other Adopt paths or omit them; count after `Replay`; `SetBase` only when `current.Base != master`.

Roadmap: new.

### 34. [P2] The journal only grows, is read from zero, and is mostly observers

No `DELETE` touches `events` or `sessions` (internal/store/sqlite; schema/001.sql:145-157); `Engine.Events` loops uncapped (internal/engine/runner.go:785-798); check's journal starts at sequence 0 (internal/command/check.go:378, 423-434), as do watch's cursors (watch.go:101, 170). Each observer session writes a row plus two events (internal/coord/coord.go:117-131, 178-190); `watchLive` redraws every 30 s and each redraw opens two observer sessions (watch.go:25, 168-176; status.go:88, 134, 143-149, 362), roughly 17,000 rows a day that watch then filters out. `status --json <branch>` computes `judgedStatus` twice (status.go:56-65, 392-393). Design v3 §11 promises events pruned with runs. Narrowed: run-driver events are session-stamped through `Session.Emit`; kind constants and a level flag wait for a reader.

Remedy: `DeleteEvents(before)` and pruning of ended sessions on `store.Tx`, called from `Engine.Cleanup` with `CleanupAge`; check's journal reads from a start sequence taken at enqueue, watch from the tail; status, queue and watch open one observer session per invocation and pass it down; delete `coord.Tail` and `Reader.Sessions`.

Roadmap: new.

### 35. [P3] Serve's side files are per database while its leases and journal are per repository

serve writes `serving.json`, `outdated.json` and two stamps beside the database (internal/engine/serve.go:365-376, 427-434, 456-458, 574-578, 592-594), the stamp writes returning early so that day's cleanup or look is silently skipped (369-371, 430-432); status joins `serving.json` to the lease holder by PID (internal/command/status.go:365-369). One database registers many repositories while the leader lease and journal are per repository (coord.go:315-316; coordination.go:159), so two serves leading two repositories share the stamps and whichever stamps first makes the other skip its daily work. Narrowed: only the leader runs the daily work, so no same-repository race.

Remedy: three journal events with the lines serve already says (on lead, after the look, before cleanup runs), one `Reader.LastEvent(kind)`, status matching the newest serve.lead event's session to the leader's; delete `serveFile` and the four files.

Roadmap: new; design v3 §11 makes the database the only coordination medium.

### 36. [P3] Recorded fields with no reader, and two updates that silently keep a changed field

Persisted and never read: `Plan.Only/Also` (written at internal/engine/plan.go:60, absent from `planView`, internal/command/json.go:209-242, whose comment promises them "for status and the PR"), `Revision.Head`, `Lease.AcquiredAt`, `Event.Target`, `Edit.Port`, `Acceptance.At`; `check --json` drops `Omitted`, which the text prints (check.go:263-268). `UpdateBranch` omits `origin` and `UpdateExecution` omits `identity` with no equality check where `UpdateRun` refuses a changed request (internal/store/sqlite/records.go:112-131, 507-528, 390-408). `prepareOne` passes only `Newest` (internal/engine/outdated.go:197-205), so `Update` re-resolves the release outdated already found (update.go:141-146). `reuse.Inputs` leaves an unlocated active port's `Tree` empty (internal/reuse/inputs.go:39-46), so `TargetInputs.Complete` fails for it. Narrowed: `Observed`, `Identity` and `SourceDigest` have readers; `Session.Version` and `Event.Target` are required by design v3 §11.

Remedy: `planJSON` gains `omitted`, `only`, `also`; `UpdateBranch`/`UpdateExecution` refuse a changed origin/identity; `UpdateRequest` gains an optional `Release` while preparation still runs `CheckRelease`; record in the reuse note whether an archive digest alone identifies an unlocated port. Leave the single unread columns.

Roadmap: the provenance part is the "stored edits keep the release's provenance" item; `Inputs` is item 6; the rest is new.

### 37. [P3] Machine values are recovered from prose

`serveLine` folds the leader PID, `SubmitPassing`, queue length and stopped count into one sentence (internal/command/status.go:358-384); `writePrepared` decides by `strings.HasPrefix` on it (internal/command/outdated.go:219), and it is the `serve` field of status and queue JSON (json.go:372-388). `attentionView` maps the glyphs back to kinds (json.go:363-370); `diffView` derives `change` from `portChangeWords` (json.go:414-419); `cleanWords` re-parses `CleanStep.What` (internal/engine/clean.go:21-36; internal/command/clean.go:166-172); `Comments` re-parses `Finding.Where` (rules.go:183-184; internal/engine/review.go:170-174). Narrowed: `Run.Detail` is not the only failure record; `PortDiff` already has a `Kind`.

Remedy, in `command`: a `serveState` computed once with `words()` and a `serveJSON` view; a typed attention kind with a glyph method; a `changeKind(port)` the words derive from. Export `CleanStep`'s parts and give `Finding` `Path`/`Line` when touched.

Roadmap: new.

### 38. [P3] Pull-request facts are folded into one string and hand-converted

`model.PullRequest.Head` is "owner/repo:branch", split at four sites (internal/engine/submit.go:120-127, 342-353; internal/engine/clean.go:133-135, 319-321); the observation's words are cast back to forge constants (internal/engine/follow.go:59-73, 97-104); `PullRequestRef` is rebuilt at six sites. `pullRequestObservation` always sets `Found: true` and `Observe` passes a 404 through (internal/forge/github/pullrequests.go:32, 82-84), so `refresh`'s "#N was not found" path (follow.go:55-57) is unreachable. Narrowed: submit's re-Observe feeds `plan.Existing` regardless; mapping 404 to `Found=false` in `destination` would open a duplicate pull request.

Remedy: a `HeadParts()` method; one `PullRequestRef` helper; map a 404 to `forge.ErrNotFound` worded only in `refresh`.

Roadmap: new.

### 39. [P3] A failed identity read is recorded as "no origin" and later read as "remade"

`identitiesNow` keeps an identity only on nil error (internal/engine/evidence.go:181-193); the execution records the map's zero value "" (internal/engine/runner.go:333-337), also the designed value for a provider that cannot say; `current()` counts when `now == ""` but drops a recorded "" against a later `now` (evidence.go:320-322), so status says the image was "made again" (internal/command/status.go:213-215), and neither identity string is shown anywhere. The realistic case is a `ReadImageRecord` error while the image works (internal/tart/manifest.go:107-117). Narrowed: `ReleaseForPlatform` and `Resolve` failures also fail `Execute`; the other swallowed-error sites are cosmetic.

Remedy, about twenty lines with item 6: ask `Identity` directly at runner.go:333-337 and treat an error as the attempt's infrastructure failure; return the error from `identitiesNow`; add the recorded identity to `executionLogsJSON`.

Roadmap: item 6's identity work, whose error path was not designed.

### 40. [P3] The index cache's identity omits the variables a non-Mac indexer was given

`openCache` hashes `PlatformVariables` (internal/macports/portindex/cache.go:80-87), but off a Mac `buildPortIndex` writes `ModelVariables(platform, "")` to `index_vars` (index.go:358-370), which adds the toolchain variables from the facts table (platform.go:211-245); nothing in the identity tracks the dockhand binary, so a rebuilt dockhand with a changed facts row reuses old generations. Narrowed: exposure is a non-Mac development host.

Remedy, about ten lines: choose the describe function once in `openCache`, hash and store it, and pass `c.variables` into `buildPortIndex`; a test that `environment.json`'s variables equal the `index_vars` given to the stand-in indexer.

Roadmap: new.

### 41. [P3] Store error kinds and plan decoding: classified for no consumer

`ErrConflict` is minted at fifteen rule checks and every other driver error including SQLITE_BUSY becomes `ErrUnavailable` (internal/store/sqlite/sqlite.go:327-341); no non-test `errors.Is` consumes either. `build.Record` returns the store's error unchanged and the driver ends the execution as Infrastructure and spends an attempt (internal/engine/runner.go:485-514, 355-371). `RecordResult` decodes the whole plan to check membership (records.go:550-568, 191-201) and `decodePlan` keeps a legacy-body branch (226-281). Verifiers refuted the materiality of all three: the state checks are the store's documented contract; `busy_timeout` queues writers; each fenced write decodes once, beside a COMMIT fsync.

Remedy: doc notes on `ErrConflict`/`ErrUnavailable` and on `Build.Record`'s error; delete `decodePlan`'s legacy branch under design v3's pre-release licence.

Roadmap: new; a note only. Merges three confirmed findings.

## 6. General improvements

### 42. [P2] The script provider's `sh -c` has no process group, so a cancel orphans `port`

`script.go:140-147` runs the person's build with `exec.CommandContext` and no `SysProcAttr`, so Go's default cancel SIGKILLs `sh` alone; `dockhand cancel` and a stopping serve cancel that context from a store poll (internal/engine/runner.go:214-217, 251-260), and the runner retries while the orphaned `port` holds MacPorts' lock. `internal/tart/host/foreground.go:31-33, 72-79` already solves this for `tart run`. Around it: seven one-shots bypass `subprocess.Run` (setup.go:178 and command/serve.go:155 among them); `channel` makes its askpass directory under `os.TempDir()` (channel.go:127); `scratch.Stale`/`Sweep` are v2 leftovers with test callers only (scratch.go:113-134), and `scratch.go:8` names a `gc` that does not exist. Narrowed: a terminal Ctrl-C reaches the whole group, so the orphan arises on `cancel`, SIGTERM or a deadline.

Remedy: `Setpgid` plus a `Cmd.Cancel` signalling `-pid` in `script.go`, its own commit; one sentence in `subprocess/doc.go` on when exec may be used directly; delete `Stale`, `Sweep`, the legacy branch and the `gc` mention; move the askpass dir to `scratch.Dir`.

Roadmap: new.

### 43. [P3] Capture's moved-while-read check is off exactly when `--include` is used

`Capture` reads the working tree, overwrites `tree` with `WithFiles` when `Include` is set, then re-reads and compares only when `len(request.Include) == 0` (internal/engine/capture.go:96-114), because the augmented tree can never equal a plain re-read. Design v3 §7 states the retry-or-stop rule in the sentence after introducing `--include`.

Remedy: repeat the pipeline (WorkingTree, then WithFiles) and compare the two final trees, which also covers an included file changing after `WithFiles` read it; two tests via a small test-only hook.

Roadmap: new; a documented promise not kept.

### 44. [P3] Eighteen functions of 120 lines or more, three with seams worth taking

`PlanCheck` (230) and `server.run` (164) are findings 4 and 3. `PlanTidy` (164, internal/engine/tidy.go:110-273) applies combine, notes, subject, message, author and blocking rules per group (204-267), which `TidyPlan.combine` duplicates (tidyplan.go:64-110; the split-commit note at tidy.go:213 and tidyplan.go:90), and no test calls the extracted helpers directly. `DiscoverPort`'s candidate filter (latest.go:74-121) and `buildPortIndex`'s config writing and seed copy (index.go:325-389) are inline seams of the same kind. Narrowed: no length-budget test; `load`'s request mutation is its documented contract; `RevbumpLinked`'s per-dependent transaction is the partial-progress semantics.

Remedy, when touched: `PlanTidy`'s group loop as a helper `combine` also calls, with a direct test; `buildPortIndex`'s two helpers; `DiscoverPort`'s filter as one helper. If a signal is wanted, `funlen` at a generous threshold.

Roadmap: new.

### 45. [P3] GitHub credential lookup re-runs per anonymous request

`Client.API()` falls back to the anonymous SDK on `ErrNoCredentials` but records nothing, so every call re-enters `SystemCredentials.Token`, which execs `security` and, with `gh` installed, `gh auth token` (internal/github/client.go:40-56, 64-82; auth.go:40-66; keychain.go:28), serialized under `authMu` across outdated's workers. A host without `security` on PATH becomes a hard failure (keychain.go:80-82; auth.go:51-53). Narrowed: one fork per call without `gh`; the `security` rule is documented and bites only non-Mac hosts.

Remedy: an `anonymous` flag beside `authSDK`, leaving `AuthenticatedAPI` probing so a running serve picks up a later login; a GOOS guard on `keychain.Store` when non-Mac hosts become a goal.

Roadmap: new.

### 46. [P3] `serve --install` drops the environment it was resolved under

`settings`' three fields resolve flag, environment, default and never the file (settings.go:56-71); the other variables are read where used (`DOCKHAND_INDEX_MIRROR`/`_CACHE`, preparer.go:69, 87; `DOCKHAND_TART_HOME`/`TART_HOME`, runtime.go:19, 37; `DOCKHAND_SSH_DIR`, keys.go:23; `DOCKHAND_CONFIG`, `DOCKHAND_UPSTREAM`, `DOCKHAND_GITHUB_CLIENT_ID`). `serveAgent` resolves `options.Git` but `agentPlist` writes only `--tree`, `--db`, the flags and PATH (internal/command/serve.go:196-210, 252-263), though the help promises the agent runs with the flags given. Narrowed: the clock and cadence consolidation has no production consequence.

Remedy, about twenty lines: carry `--git`, write `EnvironmentVariables` for whichever DOCKHAND_* variables and `GIT_BIN` are set at install time, and print what was baked in; one documented table of the variables dockhand reads.

Roadmap: new.

## Suggested implementation order

Each step stands alone; the smallest safe step first.

1. **Lock the lazies and take the small correctness gaps.** `e.lazy` in the five assemblers; Capture's check with `--include`; the script provider's process group; `serve --install`'s environment. Closes the P1 half of finding 1 and findings 42, 43, 46.
2. **Move decision 29 into the engine and validate `--tests`.** Closes finding 2; finding 5's hoist can ride along.
3. **Install the progress sink and carry the dropped decisions,** with the Go/Cargo `Previous` fix and its regression test. Closes findings 31 and 32 and the roadmap's outdated item.
4. **One `store.Recorded` helper, adopted by `Start`/`AdoptPullRequest` first,** then `Update`/`Create` and history's sites; the journal's retention and read-from-tail; the create/adopt/rebase output fixes. Closes findings 24, 33, 34.
5. **The dead-code sweep,** with one `deadcode` run without `-test`. Closes findings 9, 18, 20 and parts of 23 and 42.
6. **The two drifted copies:** `livecheckNewest` and `AwaitSSH` with the guarded `Clone` and tolerant `Stop`. Closes findings 6 and 7.
7. **With item 6, as planning and `Counts` move:** the per-cell evidence `Kind`, `PlanCheck`'s named phases, the identity error path, the computed-facts tidy, one `Dependencies` value for `engine.Open`. Closes findings 4, 25, 27, 39 and the structural half of 1; finding 19's stealth half goes with the previous review's finding 6.
8. **Smaller items as their files are touched:** findings 3, 8, 10 to 17, 21 to 23, 26, 28 to 30, 35 to 38, 40, 41, 44, 45. Two decisions for the person sit among them: the `--json` branch shape (finding 10) and whether the history verbs move (finding 22).

## Validation and limits

**What the review did.** It was produced by a multi-agent workflow (Claude Fable 5.1, 234 agents) and spot-checked by hand. Eight package maps and nine path traces read the code at `a71fc67f` and recorded leads with citations. Twelve finders, two per question (one working from the maps, one cold from the code), returned 119 raw findings, merged to 45. A completeness critic then named seven gap probes over two rounds, which added 19 candidates. Each of the 64 candidates was checked by three independent verifiers: code accuracy (every cited line reopened, greps reproduced), design intent (against design v3, the architecture and principles pages, the activity notes and the roadmap), and materiality and remedy; 58 were upheld by a majority and 6 refuted. Their corrections are applied above: the 58 are folded into the forty-six findings here, several merged where they share a remedy, most with narrowed claims and lowered priority. Thirteen of the highest-priority claims were then re-read by hand before filing (findings 1, 2, 6, 7, 18, 19, 24, 30, 31, 32, 33, 34, 42, 43); all held. Line counts, the import graph and fan-in come from `wc`, `go list` and grep over non-test files. Two claims were reproduced by a verifier in the scratchpad (the `ls-remote` configuration leak, finding 30; the function-length ranking); no other runtime behaviour was exercised.

**What it did not cover.** Tests, docs and `tools/` were read only where a finding needed them; vendored code was not read; nothing was run against a Tart guest, GitHub or a MacPorts installation; the test suite was not run. Where a consequence is inferred from a code path rather than observed, the finding says so. Test-file citations were taken from the verifiers' notes.

**Refuted, with the reason.**
- *ghactions fork branches are a leftover kind the engine cleans by re-deriving.* The two leftover kinds already have homes, and the re-derived name is the provider's documented convention.
- *The planner builds a port whose pre-fetch hook rejects.* A rejected hook does not make a port unbuildable; eligibility reads `replaced_by`, `known_fail` and `supported_archs` by design.
- *In-engine callers fix the seam order.* The call sites show where the engine's verbs are composed, not a defect.
- *Engine is +434 since the review; sized seams would land it near 6,450.* Size is not the roadmap's standard, and the remedy re-argued splits declined with reasons.
- *Item 6 has grown the engine by +143; create `internal/plan` now.* The reuse logic landed in `internal/reuse`; the growth is wiring a `plan` package would not take.
- *Each verb's reader interface sits beside it.* Accurate and immaterial: `Engine` fields with one implementation each.
- Remedies withdrawn inside surviving findings: an engine "readiness verdict" type; a `BranchStatus` split; a shared provider wire shape; a per-transaction plan cache; a busy retry in `Store.Update`; a `words`/`homedir` package; typed `Attend`/`FinishedCheck` values; a per-engine remotes index; a 120-line budget test; a fold of `outdated` into `preparation`; `macos/guest`; a nine-field `BranchChange`; a `Descriptor` per provider. Claims withdrawn: the coord lease "starving the run"; `Observed`, `Identity` and `SourceDigest` as unread; a duplicate revision row from `Capture` and `baseRevision`.