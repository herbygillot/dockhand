# 2026-10-24: Three sessions' field notes, the fixes for main

Prioritization placed these in 67f37e9d and e32059b7. Each rides v0.6.0.

## v0.5.0's docs

- docs/releases/v0.5.0.md: any command migrates the database, `status`
  included, and DOCKHAND_DB pointed at a copy tries a newer build without
  touching it.
- The README and the release notes get a known-issues row for the doubled
  squash trailer, marked fixed on main by the next item.

## tidy --squash --message writes a trailer once

The gnome-voice session's note 2. `squash` passed the given message's body
to `commitmsg.Compose` whole, so its trailer paragraph read as body, and
Compose then added the combined commits' trailers after it. Now Compose
splits the text's own trailer paragraph (`SplitTrailers`) and joins it to
the combined commits' trailers, each line once and in Compose's order. A
trailer only the message carries is kept. Test:
TestComposeWritesAGivenTrailerOnce.

## The grid says an earlier check's cell stands

The exiv2 session's notes. After a rebase, cells marked "(check-490)" said
nothing of whether they applied. They do: an earlier result fills a cell
only where nothing its build read has changed (reuse.Current).
`view.WriteResults` now writes one line under the results when any cell
carries another check's mark: "(check-490): an earlier check's result,
standing since nothing its build read has changed". Several checks are
named in one line. Test: TestTheGridSaysAnEarlierChecksCellStands, which
also checks that a grid without such cells has no line.

## A link scan that couldn't read says why, once

The exiv2 session's notes: `status -b` printed five "couldn't read what
…'s build links" lines, none with a reason. The bazel session's notes: a
result reused from another check read as unread.

- **The reason travels with the finding.** `evidence.LinkedBuild.Unread`
  says why a build's links weren't read. `LinkFinding.Reason` is the reason
  the target's first unread build gave. engine/links.go names three:
  - "its Portfile couldn't be evaluated there, so what it declares isn't
    known";
  - "its result is check-N's, which didn't read them", for a reused
    result. It already carries its origin's reading (reusedResult copies
    the links), so this says when that origin predates the scan;
  - "the guest couldn't read them, as its log says".
- **One line per reason.** `view.LinkLines` folds unread targets that share
  a reason into one line, standing where its first target would: "couldn't
  read what 5 targets link (exiv2, gpsbabel, libheif, darktable, and 1
  more): …". Targets with different reasons get a line each. The check's
  report and status's Links row both use it.
- **Tests:** TestUnreadLinksAreOneLineAReason (view),
  TestAnUnreadTargetCarriesItsReason (evidence), and the guest's reason in
  the engine's links test.

## revbump says what it waits for

The exiv2 session's notes: an applied revbump of 15 ports sat silent for 120
seconds or more, where its `--plan` was instant. Measured in a scratch run:
each port after the first is evaluated in a tree holding the bumps before
it, so the port index is updated for that tree first ("Updating PortIndex
for tree … from 1 changed path"), about 3 seconds of an 11-second port.
That line was `-vv`'s, so at the default level nothing was said between
ports. A plan edits nothing, so it never pays for this.

portindex now says it once a command, at the default level, before the
first incremental update: "Updating the port index with the edits before
each evaluation, a few seconds each time". A full build said itself
already ("Building the PortIndex; this may take several minutes").

## A development build asks before it migrates a release's database

The exiv2 session's notes, with the gnome-voice session's note 4 and the
bazel session's. A main build's read-only `status -b` migrated the shared
database from schema 32 to 39. The installed v0.4.0 then refused it ("use
a newer build"), naming neither the build nor where to get it.

- **The rule turns on whose database it is.** Before migrating,
  `sqlite.Store.refuseReleaseMigration` reads the newest session's build.
  Where that's a release and this build isn't, it refuses unasked with
  `store.ReleaseUnderMigrationError`, before anything is kept or
  migrated. A release, in buildinfo's new `Released`, is a tag with no
  pre-release or build part; an rc, a pseudo-version, a modified build
  and devel aren't. A release build migrates as before, since that is
  the port's upgrade. A database a development build last opened is
  migrated as before too.
- **The person answers.** On a terminal, settings.Open asks, naming the
  release it locks out and where the copy goes (`MigrateRelease`). Without
  one, engine's `ReleaseUnderMigrationError` refuses, naming DOCKHAND_DB
  pointed at a copy to try the build.
- **A newer schema's refusal says whose it is.** `newerSchema` names the
  build that last opened the database, and when. It gives the way to that
  build (buildinfo's new `Way`: a release comes with the port's upgrade, a
  development build from its commit of the project), and the copy kept at
  this build's schema, which loses what was recorded since if it's put
  back. The words "newer than this dockhand supports" stay, which A5 reads.
- **Tests:** TestADevelopmentBuildAsksBeforeMigratingAReleasesDatabase,
  TestANewerSchemasRefusalNamesItsBuildAndTheCopy, and
  TestReleasedAndTheWayToABuild.

## A check off a terminal says each step, and a stalled log is said

The bazel session's notes, the costliest of them. In check-615, arowpp's
bazel client exited, but bazel's server kept the output pipe open, so
`port` waited and the VM sat idle for 36 minutes. rc5's foreground check
printed nothing after its plan, where v0.4.0 printed each step. Part 5 put
progress on the redrawn line, which off a terminal is never drawn.

- **Each step, once.** Off a terminal at the default level, a foreground
  check's journal (checkrun `steps`) prints what the redrawn line would
  show, a line each time it changes: "  ⋯ command: jq failed …". A
  stage's held journal, ship's or bump's, stays held.
- **A stalled log.** engine `watchStalls` watches an execution's log
  directory while it runs. When the newest log, the target building,
  hasn't grown for ten minutes, it says so once as a wait, which every
  level prints and status and serve read: "arowpp's log hasn't grown in
  10 minutes". It says it again only once the log has grown. The timing
  is the engine's (`stall`), so a test can shorten it.
- **`-d` is named.** The foreground notice ends "check -d runs it in the
  background instead". `check --help` and its `-d` line say serve runs a
  `-d` check, or, with no serve running, a background drain dockhand starts.
- **Tests:** TestCheckRunsHereWithoutServe checks the notice and a step
  line off a terminal. TestAStalledLogIsSaidOnce checks a stall said once,
  as a wait.

## A wait for the Mac's VMs or a check's capacity says whose they are

The bazel and exiv2 sessions' notes. A check waited on "macOS runs two at
most" or a provider's capacity without saying what held it, and `tart
list` didn't show dockhand's VMs, which live in its own Tart home.

- **Capacity.** engine `slotWords` names each holder's process and how long
  it has run: "check-1 (a foreground dockhand, pid N, devel, for under a
  minute) is using it".
- **The Mac's VMs.** Tart's `machine` interface gains `Whose`, dockhand's
  running VMs by name and a count of the personal Tart home's. buildenv's
  `Build` gains `Holding(ref)`, the running check whose execution the
  provider knows by that ref (store `ExecutionsReferred`). The wait reads
  "waiting for the Mac's VMs: 2 are running, and macOS runs two at most:
  check-616's dockhand-check-run-9-tahoe-1 and 1 of yours".
- **Reaching a VM.** check -v's "building in <vm>" line adds
  `TART_HOME=<home> tart exec <vm> …`. `status -v` lists each running
  check's VMs under the queue with the same, from engine `QueuedCheck`'s
  `VMs` and `TartHome` (compose sets the engine's `TartHome`); status's
  JSON carries them as `vms` and `tart_home`.
- **Tests:** TestAForegroundCheckKeepsToTheProvidersCapacity,
  TestAnAttemptSweepsAndWaitsForRoom, and
  TestTheQueueNamesARunningChecksVMsWithTheirTartHome.

## checksums writes a subport's declaration where it has none

Field testing on rc3, at the bazel session's note, which wrote a new
subport's checksums with curl and openssl. kb-old, a subport beside kb at
0.1.6, declared no checksums in its block, and kb's declaration names kb's
own archive, so `checksums kb-old` was refused: "no checksum declaration
for kb-0.1.6.tar.gz".

- **The refusal is typed.** distfiles `Bind` returns `Undeclared`, the
  archive and every archive the context fetches, with the same words.
- **The declaration.** portfile `DeclareSubportChecksums` writes
  placeholders in the subport's block, after its last fetch line, in the
  block's indentation, as create writes them: rmd160, sha256, and size,
  each archive named where there are several. It writes nothing where the
  subport has no one literal block, its block declares checksums, or its
  lines aren't one command to a line.
- **The refresh.** portedit `refreshChecksums`, on `Undeclared`, writes the
  placeholders and refreshes them as any declaration is refreshed
  (`declareSubportChecksums`). The family as written and as edited is
  compared, so nothing but the subport's checksums may move, and kb's stay.
  A main port with no declaration is still refused.
- **Left as it was.** A port whose declaration is unnamed, which Bind
  reads as covering a subport's other archive, was still refused by strict
  fidelity ("fixture.checksums changed"); the next entry writes the
  subport its own.
- **Test:** TestASubportsChecksumsAreWrittenInItsBlock.

## A rejected GitHub login said as one, not as GitHub unreached

D-C3 on rc5: after the login was revoked, a command said `couldn't reach
GitHub (Post "https://api.github.com/graphql": forge: authentication is
required: GitHub rejected the credential…)`. http.Client wraps the
transport's refusal in a `*url.Error`, which is a `net.Error`, so
`RateLimitError` took it as the network's, of a kind another try is
offered for.

- **Auth first.** github `RateLimitError` takes `forge.ErrAuthentication`
  before the network, and returns it without the request's `*url.Error`,
  kind Auth. A write publicationError already says as rejected keeps its
  `forge.ErrRejected`; publicationError wraps the converted error.
- **The network's alone.** `networkError` takes a `*url.Error` only where
  what it wraps is a DNS failure, a dial's, or a timeout.
- **The words.** `rejected()` is a particular of the sentinel
  (`refusal.Because`), so its words lead. dockhand's login reads as
  serve's: "GitHub rejected the login from dockhand's Keychain; run
  dockhand setup github to sign in again, or dockhand setup github
  --logout to remove it", Next `dockhand setup github`. Other sources keep
  their remedies.
- **v0.5.0.** README's known issues and the release notes gain the row.
- **Test:** TestARejectedLoginIsSaidAsThatNotAsGitHubUnreached, a 401 and
  a reset connection through the client.

## A subport under the port's unnamed declaration gets its own

Left by the entry above, placed at 13c3e330. A port with one distfile
usually declares its checksums unnamed, and Bind reads that declaration as
a subport's where the subport fetches one archive, as MacPorts does; the
refresh would have rewritten the port's line, and strict fidelity refused
it saying nothing of what to do.

- **Where the declaration is.** portfile `SubportPlace` says whether a
  command, by the ordinal Bind counts, is in a subport's block, before it,
  or after it.
- **The archive undeclared.** portedit `underPortsDeclaration`, in each
  context a refresh reads, takes a subport's archive bound to an unnamed
  group declared outside its block and not among the port's own distfiles.
  Before the block, it's `Undeclared`, and the subport's declaration is
  written in its block (the entry above), the port's line unchanged. After
  the block, which MacPorts would read over the block's, it's refused:
  "move fixture's checksums line above the subport fixture-old block, then
  dockhand checksums fixture-old writes its own".
- **Test:** TestASubportUnderThePortsUnnamedDeclarationGetsItsOwn, with a
  subport fetching the port's own archive, which gets nothing.
