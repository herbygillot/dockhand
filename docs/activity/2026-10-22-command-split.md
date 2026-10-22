# Part 4, item 2: the command package split

The roadmap's part 4, item 2. A go/types cross-reference of
internal/command at 81f3ec00 found the hubs (settings, used by 23 other
files; `supportsJSON` by 18; `branchFlags` by 12) and the backward edges
a cut must remove first. Architecture checked the map and the order
(2026-10-22):

- **Packages**, under internal/command/:
  - `view`: the JSON shapes and the writers and words they share.
  - `run`: following and driving a queued check.
  - `host`: the Mac, its timings, and `machineHost`, the one place that
    imports tart, credential, keychain, and macos.
  - `settings`: Settings, open, places, the editor, `SupportsJSON`.
  - `branchflags`: the flags, the branch choice and selection, and
    `namedOrLatest`; it takes the engine and the streams, not Settings.
  - `cmdtest`: the shared test helpers.
  - The verb packages, each with what shares its engine calls: edit and
    revbump with `author`, rebase with `tidy`, archive with `clean`.
- **Order**: step 0 inside the package; then view, run, the shared three,
  cmdtest; then the verbs, smallest first, with status and check last.
  Each step goes green on its own.
- **Tests**: a verb's end-to-end tests become an external package
  importing root, each its own binary. The cross-cutting ones (rollout,
  stealth, baseline, json, helpreference) stay root's.
- **boundary_test** walks the subpackages and asserts the shape. No verb
  imports another verb, but bump imports author and status its own watch.
  Verbs import view, run, the shared three, term, and the engine. View,
  run, and the shared three import no verb.

## Step 0, inside the package

- review.go and its test are tidy.go and tidy_test.go, since they hold
  tidy and undo (reviewpr.go is the pull request review).
- The eleven methods on settings and host that verb files defined are
  functions taking them, context first: `githubLogin`, `githubLogout`,
  `cleanupAfter`, `imagesOf`, `providerLines`, `tartReadiness`,
  `githubReadiness`, `missingSteps`, `editorCommand`, `publishing`, and
  `serveOptions`.
- `runQueued` takes `config.Places` and a drain function in place of
  settings. Settings' `drain` starts the background drain.
- status --watch is given the command line's runner (`settings.command`)
  rather than calling `Run` itself. This is a scaffold: with watch in the
  status package, watch calls the status renderer directly, and the
  injection goes in step 5.
- **Two text dependencies typed in the engine.**
  - `PortDiff.Change()` gives `PortAdded`, `PortRemoved`,
    `PortRevisionOnly`, or `PortChanged`. impact tested diff's words
    (`kind != "changed"`), and the JSON replaced the words' spaces. Both
    now read the kind, and the JSON values are unchanged.
  - `SubmitPlan.Push()` gives `PushOthersPushed`, `PushNothingNew`,
    `PushReplaces`, `PushAdds`, or `PushCreates`, which `pushWords` words.
- `underUpstream`, a one-line wrapper, is gone; its callers call
  `engine.UnderUpstream`.

## Step 1: view

`internal/command/view` holds json.go and json_results.go (now
results.go), whole, and from the verb files the writers and words they
shared: attention and its kinds, `AttentionFor`, `RenderNext`,
`RenderSteps`, `SayNext`, `WriteNext`/`WriteNextFor`, `CheckState`, the
results, plan, push, and queue writers, and the word functions
(`PushWords`, `CheckWords`, `PortChangeWords`, `OrNone`, `HoldLegend`,
`ExperimentalWords`, `WritePlainHTTP`, `ComparisonLines`). The 67 names
command uses are exported, with `Attention`'s fields, `Mark`, and
`BatchJSON.Next`. `supportsJSON` stayed with settings, its owner next.
Local variables named `view` in command are `viewed`, so the package's
name is free there.

The moves were mechanical: a cutter for declarations with their
comments, a go/types renamer over view, and the compiler's own
"undefined" positions to qualify command's references, with nothing
hand-edited but the boundary test.

**The boundary test walks every package under internal/command** and
asserts the shape architecture asked for. The layers (view, checkrun, host,
settings, branchflags, cmdtest) import no verb and not the root. A verb
imports no other verb, except as `verbImports` names (bump → author).
The engine-boundary allowlist holds for all of them, its unused entries
counted across the tree.

## Step 2: checkrun

The run library is `internal/command/checkrun`, so that every command's
`run` variable keeps its name: `RunQueued`, `Follow`, `Report`,
`StopWords`, and what check and its tests read (`StoppedExit`,
`WriteBaselineResults`, `HintBaseline`), with the journal and the
failure writers inside. It's check.go's sixteen declarations reachable
from those, which a closure over the go/types cross-reference found
closed but for view.

## Step 3a: host

`internal/command/host` holds the Host (the Mac, or a test's stand-in),
its Timings, `MachineHost`, `TartImages`, and cleanup_unix.go's
`StartCleanup` and `StartDrain`, whole. Host's and Timings' fields are
exported, since serve, setup, status --watch, and the tests read and set
them; `os` is `OS`. Locals named `host` and `settings` in command are
`mac` and `shared`, so the packages' names are free.

## Step 3b: settings and branchflags

- `internal/command/settings` holds settings.go whole (now `Settings`,
  its fields and methods exported: `Open`, `Places`, `Options`, `Drain`,
  `Getenv`, `Signin`, …), `SupportsJSON`, and the editor
  (`EditorCommand`, `RunEditor`, `EditText`).
- `internal/command/branchflags` holds branchflags.go whole
  (`BranchFlags` with `Register`, `Resolve`, `Explicit`), and from
  author.go the authoring commands' choice (`BranchChoice`, its
  `Register`, which was a method author.go defined, `ChooseBranch`,
  `ChosenBranch`, `SettleSelection`, `NextIn`, `StartFor`, `NewNamed`),
  and from queue.go `NamedOrLatest`. `BranchFlags.Register` still takes
  the settings, for its completion's engine. It's a layer beside settings,
  which the boundary test allows.

## Step 4: cmdtest

`internal/command/cmdtest` is the tests' shared harness, moved out of the
_test.go files that held it: the world (`NewWorld`, `World`), the
stand-ins (`Stands`, `Hosted`, `Setenv`, `Chdir`, `UsePorts`,
`TestSettings`, `TestPlaces`), the runs (`Dockhand`, `RunFor`, `JSONOf`,
`Dig`), and the fakes (`Bumper`, `WithBumper`, `Versioned`, `WithScript`,
`OnePort`, `WithGitHub`, `SignedIn`, `MemoryStore`, `WithAuth`,
`WithOutdated`, `SyncBuffer`, `CheckedBranch`, `BumpOn`). Its only reach
into the root was `run`. The root exports `RunWith`, and
`settings.Invocation` is the command line it takes. A test binary's
TestMain is `cmdtest.Main(m, RunWith)`, which also isolates the
environment as the root's TestMain did. So cmdtest never imports the
root, and a verb's external tests and the root's own run the same command
line. `SignedIn`'s login field is `Account`, beside its `Login` method.
The boundary test holds cmdtest to the layers' shape, not the
engine-boundary list, since it reaches what the tests do.

The exported names' doc comments were renamed with them, across the new
packages.

## Step 5: the verbs, smallest first

Each verb is a package under internal/command with a `Command`
constructor, which the root registers. Its tests move with it, white-box
in the verb's own package, and a `main_test.go` external file hands
cmdtest.Main the root's RunWith. The internal and external test files
compile into one binary, so the white-box tests run the same command
line without importing the root.

- **explain, shell, config, serve.** `serveWords`, status's sentence for
  serve's state that serve's tests read too, is `view.ServeWords`.
  clean's test helper `cleanupStamp` is `cmdtest.CleanupStamp`. config
  imports internal/config under its own name, since a package's name
  isn't in its scope; the root imports `command/config`.
- **understand** (diff, impact, open: `DiffCommand`, `ImpactCommand`,
  `OpenCommand`), with archivediff_test. jq's dependents, a fake that
  author's tests share, is `cmdtest.JqDependents`.
- **clean, with archive** (`Command`, `ArchiveCommand`, `CleanupAfter`).
  The pull requests' refresh that status and clean shared splits by
  architecture's note. The stale-read rule is the engine's:
  `Engine.RefreshStalePullRequests` re-reads where the record is older
  than serve reads it, within 20 seconds. The words are view's:
  `WriteRefreshed`, what changed and what couldn't be read, with GitHub's
  refusal worded once, and `WriteChanges`. view reads nothing. Each verb
  calls the engine and view in a few lines of its own.
- **ship gets its own package**, by architecture's answer. It imports
  author, tidy, submit, outdated, and checkrun, and bump imports ship.
  ship composes the four stages each owner exports as its operation:
  `tidy.Decide`, `submit.Apply`, `submit.AskTested`, `outdated.Batch`.
  The pure writers ship reads (writeTidyPlan, describeWork,
  writeSubmitPlan, pullRequestWords, upstreamLines) go to view.
- **outdated** (`Command`, and `Batch`, which ship and update's
  --outdated compose). A batch's exit rule, `batchExit`, is
  `view.BatchExit`; ship's test helper `asMaintainer` is
  `cmdtest.AsMaintainer`.
- **create**.
- **setup**: auth.go, providers.go, setup.go, and work.go, which hold
  setup and its github and tart subcommands, adopt, path, and start
  (`Command`, `AdoptCommand`, `PathCommand`, `StartCommand`).
- **tidy, with undo and rebase** (`Command`, `UndoCommand`,
  `RebaseCommand`, and `Decide`, ship's tidy stage). Its writers
  (`WriteTidyPlan`, `DescribeWork`, `ModifiedBuildWarning`) are view's.
- **submit** (`Command`, and `Apply` and `AskTested`, ship's stages). Its
  plan's writers (`WriteSubmitPlan`, `PullRequestWords`, `UpstreamLines`,
  and their words) are view's, and so is author's `WriteUpstream`. The
  tests' `UnfetchedPrevious` and `Cells` are cmdtest's.
