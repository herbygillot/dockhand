# One verdict on a branch's state: branchstate (v0.5.0 part 4, item 1)

## The shape, settled with architecture

Mapping the six judgments found them in these places:
- status's `attentionFor`;
- serve's `PassingBranches` and `serveCandidates`;
- submit's ten or so `Blocking` sites;
- tidy's `Keep`, `Unambiguous`, `AppliesUnasked`, and `HeldForLook`;
- `Held` and `Concerns`;
- ship's gates. `goOn` is now engine/ship.go's `commitUnasked`.

Architecture's answer settles how they become one:
- **The package.** `internal/branchstate` is pure, over model and evidence. The engine gathers the `Facts`. `Evaluate(facts, action, mode)` gives one `Verdict`. The migration takes five commits.
- **`Next` is the verdict's, and typed.** It holds a verb, the branch, its arguments, and `After`: what must change before the same action can be asked again (a person's look, a check finishing, a person's edit). The contract test beside `Evaluate` says a refusal's Next names another verb or an After, never the same action asked again with nothing to wait for. The command renders a Next as a command line, so the help-reference test proves every rendered verb and flag exists.
- **Each kind's words go with the kind, in branchstate,** since serve, in the engine, says them too. The command adds layout only.
- **Facts that a read can fail or leave stale say unknown apart from false:** the pull request observation, the fork and push permission, a Git-fetched target's tag. Unknown never equals: serve-submit holds on it, and status says it's stale.
- **Mode is `Answered` alone** for consent. Origin is a fact, never a second reading of consent.
- **Done when** command/ship.go makes no judgment, and `attentionFor`, `submitNext`, `checkWords`, the Blocking sites, and `PassingBranches` are each a call plus rendering.

## Step 1: the check's standing, and the Next contract

`CheckStanding` and the active-check predicate (R3-2) move to `branchstate.Check.Standing` and `Check.Checking`. That's the one definition of the branch's checks, apart from baselines and runs asked to stop. `BranchStatus.Check()` gathers the facts, and its `CheckStanding()` and `Checking()` delegate to it. The command renders `branchstate.Standing` and imports branchstate, named in its boundary test. branchstate's own boundary test allows model and evidence alone.

`Verdict`, `Facts`, `Action`, `Mode` (`Answered`: asked, yes, or nobody), and the typed `Next` with `After` land with `Evaluate`, which says the standing so far. So does the contract test `TestARefusalsNextAdvances`, over a table of cases that's empty now; each step adds the refusals it brings.

## Step 2: attention and Next on the check's standing

status's `attentionFor` judged and worded every row in one function of the command, with `pullRequestAttention` and `submitNext` beside it. It's now `branchstate.Attend(facts)`. That returns at most one typed `Attention`:
- its `Kind` (failed, needs_you, or ready);
- one of 22 `Reason`s;
- the data its words need;
- a typed `Next`.

`Attention.Words(vocabulary)` holds each reason's sentence. The nouns are worded by their owners through `branchstate.Vocabulary`, which `engine.Vocabulary` implements with `DescribeEnvironment`, `Describe`, `Ago`, `RemadeWords`, and `HoldLines`, so branchstate imports none of them. `BranchStatus.Facts()` gathers the facts.

The command is now a call plus rendering:
- `attentionFor` maps each row's kind, words, and `renderNext(next)`. `renderNext` is the one place a Next becomes a command line. Every status test passes on it, its lines unchanged.
- `TestEveryNextVerbRenders` holds every verb in `branchstate.Verbs` to rendering as one.

Attend reads the check's standing (`Check.Standing`) where status re-derived it from the runs beside it. That settles the two disagreements the map found, which `TestAttentionReadsTheChecksStanding` pins:
- a baseline running beside a passed check hid status's row; it no longer does;
- a failed extra (from --also, or a revision-only port) read "passed; waiting for you to submit" while submit refused it. It now asks to be accepted, "submit --accept <port>".

## Step 3: serve's passing branches from the one definition

`PassingBranches` defined a branch that passed and is ready to submit inline, for serve and for `ship --passing`. That definition is now `branchstate.SubmitReadiness(facts)`, which says a branch is not a candidate, ready, or not ready. `PassingBranches` and, through it, `serveCandidates` call it. `Branch.Commits` joins the facts.

`Evaluate(facts, ActionSubmit, mode)` refuses a branch that isn't ready, and its Next follows from the record and the check's standing:
- adopt, for a branch gone;
- status, for one already pushed;
- tidy, for nothing committed or files edited;
- wait, after the check finishes, for one checking;
- wait, for one stopped;
- logs, for one failed;
- check, otherwise.

The contract table now holds these eight refusals, and every one's Next advances. Submit's own blocks are step 4.

## Step 4: submit's blocks are typed, and the publication rule is branchstate's

submit appended a sentence at each of its blocking sites. Each now records a `branchstate.Block` of one of 15 kinds, through `SubmitPlan.block`:
- unconfirmed guesses, a merge commit;
- a check pending, a check of other files, no check;
- an unmet target, a remade target, an unchecked target;
- a failure the base shares, a failure, a failure not accepted;
- someone else's push, maintainers not allowed to edit, no write access;
- no title.

The block's words are `Block.Words`, the sentences submit said, word for word, and `Blocking` keeps them. The data each sentence names rides on the block, and the nouns go through the vocabulary, which gains `Unmet`.

The publication rule is branchstate's, with the acceptances it reads, moved from engine/evidence.go:
- `PublicationBlocks`, where `publicationProblems` built sentences;
- `AcceptanceProblem`, `AcceptedReasons`, and `BlockedByAccepted`;
- `EnvironmentFailures` and `StandingAccepted`;
- `AcceptedFailure` with its kinds and words.

The engine keeps aliases for `AcceptedFailure` and its kinds, and its `StandingAccepted` method reads the store and calls branchstate. The engine's tests read the rule through test-side wrappers that word the blocks as submit does. Every test passes unchanged, which shows the words didn't move.

`submit --json` gains `blocks`, each with its kind, its port, and its words. Still to come: the remaining sites' conditions (a merge commit, a missing title) are simple reads of facts the engine gathers as it plans. They move to an `Evaluate(submit)` over gathered facts with step 5, which settles one gate for ship and serve.

## Step 5a: consent read once, and tidy's gate in branchstate

How a step was answered was read in three places (R3-9):
- `shipOptions.Unattended`;
- `shipAll`'s own `answered != AnsweredAsked`;
- `tidyBranch`'s terminal test, which turned the answer into `HeldForLook`.

`branchstate.Mode` now carries `model.Answered`, where step 1 had a copy of its own, and `Mode.Unattended()` is the one reading of consent. Ship, both the engine's and the command's, reads it.

The plan's gate moves too. `TidyPlan.Gate()` gives the plan as `branchstate.Plan`. `Unambiguous`, `Blocking`, and `AppliesUnasked(mode, explicit)` are branchstate's, and `CommitUnasked(mode)` is the gate ship and serve commit through: dockhand's own edits only, where nobody looks. It replaces `TidyPlan.HeldForLook`, which reconverted the answer itself. tidy's command, verbs, and ship call it. The interactive review stays the command's: it asks, and it doesn't judge.

## Step 5b: one gate for every submission (R3-10, R3-5's single owner)

What lets a submission go was held in three places:
- `ApplySubmit` refused on `Blocking`;
- ship's `submitPassed` refused a moved commit and, where nobody looked, what held it;
- serve's `SubmitForServe` refused what held it.

`branchstate.Publish(publication, mode, vocabulary)` is now the one gate. A submission is refused when something blocks it, when its commit isn't the one it was bound to, or, where nobody looks, when something holds it. A person's own submission shows its holds and goes on. A refusal is a typed `branchstate.Refusal` of one of three kinds (blocked, moved, held), with its words, its holds, and a Next: submit again, after an edit or a look.

`ApplySubmit(ctx, plan, answered)` calls the gate, as R3-10 asked:
- submit passes `AnsweredAsked`, since it's a person's own;
- serve passes `AnsweredNobody`;
- ship passes what it was answered, binding its commit through the new `SubmitRequest.Bound`.

Ship's `submitPassed` now asks the engine and words the refusal as ship says it, with the same ExitAttention and words as before. Its `Once it's fine:` line is `renderNext`'s. The draft rule after a failed check is `branchstate.SubmitsDespiteCheck`.

The engine's tests call `ApplySubmit` with `AnsweredAsked`. Every test passes unchanged otherwise.

## An unchecked target's block says where

This was the exiv2 session's note 7, the part c66c9394 left undone. submit's block for a changed target with no result where it's required said only that "these files have not been checked in every required environment". It now names each place: "ufraw has changed, but these files haven't been built on macOS 26; macOS 13, which check-4 planned and stopped before."

- The block carries its `Unbuilt` places (`branchstate.Place`), read from the target's not-run cells.
- Outside build.default's profile, a target is asked for only because a check planned it there. The evidence now keeps that check on the cell: `Cell.PlannedIn`, set where `outsideProfile` finds a check that planned the target, which `evidence.Plan`'s merge had lost.
- Tests: `TestOutsideTheProfileOnlyWhatAChecksPlannedIsRequired` checks the cell's check, and `TestAnUncheckedTargetsBlockSaysWhere` the words. Two engine tests now read the named environment.

## R3-24: the two readings of a pull request share their precedence

The REST inspection (forge/github/inspect.go) and the GraphQL reading (reading.go) each decided what a review's state and a check's outcome count as. They agreed only by being copied: a waiting first-time contributor's run is pending, and a request for changes holds over an approval. `precedence.go` now holds `review` and `check`, which both readings call. `TestBothReadingsCountAlike` holds the table in both spellings, REST's lowercase and GraphQL's uppercase. CI's classification by cause will read what these give.

## Refusals: one shape, a kind each (the first step)

Architecture settled where the shape lives: a leaf package, `internal/refusal`, which imports nothing of dockhand's, so the packages below the engine that refuse can fill it. It holds:
- **`Refusal`**: its kind, its words, what was kept, and what moves it on. It's an error, reached with `errors.As` through any wrapping. It's made only by `New` or `Wrap`, which require a kind, so a refusal without a kind can't exist.
- **`Kind`**: network, upstream-not-yet, upstream-missing, auth, unsupported, environment, busy, attention, check-failed, interrupted, failed. Each kind's doc says who gives its Next: the producer where the remedy is unconditional and its own, the engine or branchstate where it depends on the branch. `Kind.Exit` is the one table of exit codes, which `term.ExitCode` reads after an explicit exit error. `Kind.Transient` says another try may meet it, and `refusal.Transient(err)` reads it.
- **`Next`, `Verb` and `After`**, moved down from branchstate, which now aliases them. `Next` gains `External`, for a remedy that isn't a dockhand command. `refusal.Verbs`, with `VerbSetup` added, is the one vocabulary of verbs, and the command's test holds each to rendering.

branchstate's publish gate now returns a `refusal.Refusal`: a blocked or moved submission is `Failed`, and one held for a look is `Attention`, whose Next is submit after a look. Its own refusal type is gone. Ship renders the refusal by its kind.

Next in this item:
- one Transient, with the retry rule reading `refusal.Transient` alone and the five shapes deleted (R3-6);
- the producers below the engine filling kinds and Nexts (R3-31);
- the command's string surgery and "Kept:" composers gone (R3-38).

## R3-6: one reading of a failure another try may meet

Five shapes said "worth trying again": `buildenv.ErrInfrastructure`,
`fetch.Transient`, distfetch's mirrors behind, GitHub's rate limit, and the
`Transient` bool an upstream comparison stored. Each is now a refusal of its
kind, and `refusal.Transient` is the one reading.

- `refusal` gains `Infrastructure` and `Limited` kinds, both transient.
  `refusal.Transient` reads only refusals (and joins of them); the
  method-based fallback is gone.
- `buildenv.Infrastructure(format, ...)` replaces `ErrInfrastructure`; the
  script, GitHub Actions and Tart providers call it.
- `fetch.Open` wraps a transport failure, a 429 and a 5xx as `Network`;
  `fetch.Transient` is deleted.
- GitHub's rate limit is `Limited`, and a `net.Error` there `Network`.
- distfetch's "mirrors haven't caught up" is `UpstreamNotYet`.
- The engine's retry rule is `case refusal.Transient(err)`.
- `model.UpstreamComparison` stores `problem_kind` in place of
  `transient`; a recorded `"transient": true` reads as `network`.
  `portedit`'s `PreviousKind` carries the kind through preparation.

## R3-31: producers below the engine fill kinds and Nexts

The sentences with remedies the architecture review listed below the
engine now each say their kind, and the producer gives Next only where the
remedy is unconditional and its own:

- `forge.ErrAuthentication` is a refusal of the `Auth` kind, so every error
  wrapping it is; `ErrNoCredentials` and a rejected credential too, their
  remedies (several ways to sign in) staying words.
- A GitHub login that has ended (an earlier dockhand's format, six months
  unused, GitHub refusing to renew it) is `Auth` with Next `setup github`,
  its words only what happened: "the GitHub login expired after six months
  of inactivity". A locked Keychain and an expired device code are `Auth`,
  their remedies words.
- A Tart image missing is `Environment` with Next `setup tart <release>`
  (`setup tart` for `tart:all` with none); the words say whether setup
  restores it from its golden copy or creates it.
- An archive's 404 is `UpstreamMissing`, `notFoundWords` kept as the
  diagnosis; its Next is the engine's.
- `portfile.ErrUnsupported` and `portsource.ErrUnsupported` are
  `Unsupported` refusals, so portedit's manifest-hook refusal and the
  others wrapping them are; `portfile/move.go`'s substituted github.setup
  now wraps `ErrUnsupported`.
- `store.ErrLiveUnderMigration` is `Busy`, with no Next: waiting for the
  processes or stopping them is the person's choice.
- coord's heartbeat sentences stay: they're progress reports, not refusals.

The command line says a producer's Next last: `sayNext` in `command.run`
appends "Next: dockhand setup github" (rendered by `renderNext`, as status
renders a branch's) to a refusal that carries one, unless the error is an
exit error a command composed with its own. The setup line for an ended
login (work.go) reads the refusal's words and Next, where it cut
`ErrAuthentication`'s text off the error (one of R3-38's three surgeries).

Left, by the grep: `portedit/stealth.go` trims `ErrUnsupported`'s words off
an error for its own sentence, below the engine; it goes with R3-38's
`author.go` composer, which does the same at the command.

With it, by architecture's answer on R3-38: `Refusal.Next` is a list of
steps, `[]Next`, each one a dockhand command or a remedy in words
(`External` keeps its one meaning), in order; `Then(steps...)` sets them,
and `Refusal.Advances` holds where some step moves on. The command line
says one "Next:" line a step. And the 404 becoming `UpstreamMissing` showed
that `KindOf` must read joins: upstream's 404 beside a mirror's 502 is
worth another try, so `KindOf` takes the first transient kind among the
outermost refusals on each path, else the first, and `Transient` is
`KindOf(err).Transient()`.

## R3-38: no error is read as a verdict, and none is cut

- **Particulars of sentinels.** `Refusal.Because(format, ...)` is a
  sentinel's particular: of its kind, `errors.Is` reaching the sentinel and
  each `%w`, said with the sentinel's words first, its `Words` its own.
  `portfile.ErrUnsupported`, `portsource.ErrUnsupported`,
  `fidelity.ErrMismatch` (now `Unsupported`), `observe.ErrInconclusive`
  (`Unsupported`) and `buildenv.ErrDockhandFault` (`Failed`) are refusals,
  and their 190-odd `fmt.Errorf("%w: ...", sentinel, ...)` sites (tests'
  fakes too) are `sentinel.Because(...)`, every message as it was.
  `refusal.WordsOf(err)` reads the first error that has its own words
  (`InPortGroup` and `AmbiguousDeclaration` gained `Words`). It replaces
  every `strings.TrimPrefix(err.Error(), sentinel.Error()+": ")`:
  author.go's byHand, portedit's `unsupportedReason` and `reasonWords`,
  distfiles' `Unlocated`, fidelity's `ScopedVersion`, and the runner's
  dockhand fault. None is left in internal/.
- **Update's refusals are the engine's** (`engine/refused.go`):
  `Engine.Update` composes once what didn't go, with Kept from
  `UpdateRequest.Started` (the branch the command started for it) and
  `Update.Started`. `byHand` maps its steps from what refused it by type
  (`InPortGroup`, `ChecksumsToWrite`, `Unlocated`, the action), and the
  steps are `Next`s: the by-hand step in words, then `dockhand edit
  <port>`. An uncertain release is an `Attention` refusal, its two ifs as
  steps, `bump` where nobody looks. author.go's `byHand`,
  `uncertainUpdate` and inline "Kept:" are gone; `setAsideWords` moved to
  the engine as `SetAsideWords`. A batch's line for a port says its steps
  too.
- **A check's outcome is a refusal**: failed (`CheckFailed`, Next `logs`),
  waiting for a look (`Attention`), stopped (`Interrupted`; one no longer
  followed has Next `wait`). ship reads `KindOf(checked).Outcome()`
  (`Kind.Outcome`: the kinds that exit other than 1) for what a draft
  submits despite, and says "nothing was submitted" by wrapping it, where
  it appended to an `ExitError`'s message.
- **outdated's reason is the engine's**: `OutdatedPort.Reason()`, the
  problem's first line, which the engine's grouping and the command's
  "no port could be checked" both read.

The output moves "Logs:", "Edit with:" and the uncertain release's ifs to
"Next:" lines, one per step. The grep at the close: no "Kept:" composed in
command, no `TrimPrefix` or `Cut` on an error's words in command (or below
it), and author.go's three composers gone.

## CI failures classified by cause, with Next offering the fix

garage #35174's macos-15 job failed fetching jni-sys-macros-0.4.1.crate,
mirror.sjtu.edu.cn answering 403 after static.crates.io timed out, where
a check on the same commit passed, and status said "CI ✗". Its attempt-1
log, read through the API, is the fixture. Placement as architecture
answered:

- **`buildlog.FetchFailed`** reads MacPorts' "Error: Failed to fetch
  <file>: <reason>" and the "DEBUG: Fetching <url> failed" line before it
  for the site; `FetchFailure.Words` says "fetching
  jni-sys-macros-0.4.1.crate (mirror.sjtu.edu.cn answered 403)". An
  exception to decision 32, as the "Command failed:" reading is: changed
  wording leaves the failure unclassified, and nothing is judged by it.
- **The forge contract** gains `FailedJobs(repository, head)`,
  `JobLog(repository, job)` and `RerunFailed(repository, run)`
  (`forge.CIJob`), implemented in forge/github (ci.go) on the documented
  endpoints only: a head's workflow runs, a run's jobs, a job's log link
  (GitHub's line timestamps stripped), and re-running a run's failed jobs.
  A log gone or withheld is `forge.ErrNotFound`; a re-run without write
  access `forge.ErrRejected`. The fake keeps `Failed`, `Logs`, `LogsRead`,
  `Rerun`, `RerunErr`.
- **Schema 37**, `ci_failures`: one row per job's attempt, keyed by
  branch, job and attempt, with the head, run, check name, cause (`fetch`
  or `unclassified`), file, URL, status, reason, and when it was read;
  never the log. `model.CIFailure` validates it; the store's `AddCIFailure`
  and `CIFailures(branch, head)`.
- **The engine's refresh** (`classifyCI`, engine/cifailure.go) reads, for
  an open pull request whose checks fail, each failed job not yet
  classified, once: a log gone is unclassified, with why, and not asked
  again; a network or allowance failure is asked at the next refresh.
  `BranchStatus.CI` carries the head's rows to branchstate.
- **branchstate's CI row** says each check's latest classified failure,
  "#35174 MacPorts CI failed fetching … on macos-15", by the vocabulary's
  `CIFailure` (`engine.CIFailureWords`), and its Next is `VerbRerunCI`
  where every failed check's latest failure is a fetch; otherwise the
  checks' page, as before. `LatestCI` keeps each check's latest attempt.
- **`engine.RerunCI`** asks GitHub to re-run each failed run at the head;
  without write access it's an `Auth` refusal whose step is to ask a
  maintainer, or push, which runs CI anew.
- **The command** is `dockhand retry --ci -b <branch>`, as architecture
  recommended and Herby chose ("extend `retry`", 2026-10-22 07:51Z),
  spelled in one place (renderNext and retry's flag).

## Offline said as offline, and the network's holds kept (item 1's rest)

- **GitHub not reached** is `refusal.Network` with words "couldn't reach
  GitHub (connection reset by peer)", the network's own reason read by
  type (`net.DNSError`, `net.OpError`, its `os.SyscallError`, a timeout),
  where Go's whole request line led. git's own network failures stay in
  git's words: git documents none of them, and reading its stderr would
  be another decision-32 exception nobody asked for.
- **A guest's network fault upstream** is `refusal.Network`, not
  `Infrastructure`; both are transient, so the runner tries it again as
  before (274d0c86).
- **C2's rest, schema 38** (`network_holds`): what a submission plan found
  that took the network (moved Git sources, other open pull requests, or
  not knowing) is kept by branch and tree, replaced whole by the next plan
  of that tree, with a row of no origin where it found none. `PlanSubmit`
  records it (a failure to keep it fails nothing the plan says), and
  `BranchStatus` adds it to `Held`, through the hold list, with
  `NetworkHeldAt` saying when; status still reads no network.
