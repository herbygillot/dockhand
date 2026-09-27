# 2026-09-27: the roadmap's smaller items

Taken between items, each its own commit.

## Xcode archives are Apple's

Setup refuses an Xcode archive that `pkgutil --check-signature` doesn't call "signed Apple Software", before it makes or checks anything (`Provisioner.Signature`). The guest's `xip --expand` doesn't check signatures. The check on xcodes' downloads alone went with it ([note](2026-09-27-xcode-follows-the-buildbots.md)).

## One image descriptor

A release's images were named in four places: `tart/release.go`'s defaults, provisioning's `goldenName`, the provider's `baseImage`, `xcodeImage`, and `goldenImage`, and `tools/facts`' prefix match. They disagreed at the edge. For an image under another name, provisioning's golden copy was `<name>-golden`, and the provider's was `dockhand-golden-<rest>`.

`tart.Prepared{Release, Profile}` now names them all:
- `Name`: `dockhand-base-tahoe` or `dockhand-xcode-tahoe`;
- `Golden`: `dockhand-golden-tahoe` or `dockhand-golden-xcode-tahoe`;
- `Source`: the vanilla image;
- `ParsePrepared` reads a name back;
- `GoldenName` gives any image's golden copy, `<name>-golden` for one under another name.

Setup's check that an image holds what its name says reads the name with `ParsePrepared`, rather than by prefix. The profile is the facts table's (`macos.ProfileTools`, `macos.ProfileXcode`). The names themselves are unchanged, so existing images stand.

## What a Tart guest reported, kept whole

The architecture review of 2026-09-27 found three reductions where Tart's results were converted.

**Why a target stopped** was only a progress line. It is now the result's `Detail` (schema 17): MacPorts' last errors for a failed target, or the changed dependency that blocked one. `dockhand logs` shows it after the outcome ("harbor-cli failed at install: …"), and `--json` as `detail`. Schema 11 had dropped schema 10's detail, which only said what the plan now says, an unmet need; this one is the build's own.

**The guest's MacPorts and developer directory** were read and dropped, since `model.Observed` had no place for them. It now has one. The guest records `port version`'s own words, "Version: 2.12.6", which the provider reads with `installation.ParseVersion`. Tested on names it: "Xcode 26.6 17F42 · MacPorts 2.12.6 · tart: …".

**Tested on stood the latest report for every run.** `Evidence.Observed` took the newest nonempty report, even for results from an earlier check's run with other tools. `Evidence.Observations` now groups an environment's runs by what they reported:
- one line where they agree, as before;
- one each, naming its runs, where they don't;
- a run that reported nothing joins the one report where there is one, and otherwise stands as its own.

## `outdated` shows its progress

A large `--mine` was minutes of silence: 1,076 ports took three. `outdated.Service` now tells an optional `Progress` how many ports are looked up: once before the first, and after each, in order, never two calls at once. The engine's `OutdatedRequest` carries it, and `outdated` and `update --outdated` draw one line on standard error, "Looking up each port's newest release: 312 of 1,076", redrawn in place and cleared at the end. That happens only when standard error is a terminal, and never with `--json` (`Streams.errTerminal`). Serve's daily look passes none.

## One interpreter per port for its version comparisons

Each `SelectVersion` and `ExtractVersions` started MacPorts' `tclsh` and loaded the version script, about 43 ms each on this Mac, measured with five comparisons. A port's discovery makes several, so a 1,076-port `--mine` spent minutes of process time starting interpreters.

Now:
- The evaluator offers a `VersionSession` (`macports.VersionSessions`), one interpreter kept for the calls.
- `upstream.Service` opens one per port, for `DiscoverPort` and `Resolve`, and closes it after (`withVersionSession`).
- A comparer that can't keep one, or can't start it, is used as before.

The evaluator's one-call methods are thin wrappers over a session, so every comparison runs the same code. `TestAPortsComparisonsShareOneInterpreter` counts one interpreter, closed, for a discovery of several comparisons, and none started alone.

## An update's release outlives its process

The architecture review found the release an update chose, with its tag and upstream commit, only in the update's own response. The stored edit didn't have it, so nothing after the update could say where a version came from.

- An update's edit now keeps its `model.Release` (schema 18, `edits.release`): forge, repository, tag, and upstream commit, or that it came from the port's distfiles.
- `BranchStatus.Releases` reads each port's latest back.
- `dockhand status <branch>` shows it: "Release  jq 1.8.1, GitHub tag jq-1.8.1 of jqlang/jq at 1a2b3c4". The branch's `--json` lists it under `releases`.

`TestAnUpdatesReleaseIsKept` is the review's update, reload, and tidy/status test. Each `dockhand` command is its own process, so status reads the release back from the store, before tidy and after. The review's fuller report, with affected members, findings, and commit intent, waits until something reads it, as the roadmap said.

## The github provider keeps each runner's part

MacPorts' workflow runs a job per macOS release, and dockhand folded them into one result with one log, so which runner built what was lost. Worse, a port one runner didn't list got no verdict at all, and the check ended "did not finish". The workflow leaves a port off a macOS it doesn't support.

Now each result keeps its runners' parts (`TargetResult.Builders`, schema 19), and the verdict is derived from them:
- **Each part** has its runner, outcome, phase, tests, and log.
- **Failed** where any runner that built it failed, at the first one's phase, with the result's detail naming it: "on macos-15".
- **Passed** where every runner that built it passed.
- **Not built there:** a runner that listed the subports without the port. Its part is not run, and the rest decide.
- **No verdict yet,** as before, while a runner hasn't reached the listing (`ListsSubports`), or listed the port and never reached it, and when no runner built it.

`dockhand logs` shows each runner's part under a result ("build (macos-15) passed  …/build-macos-15.log", or "didn't build it"). `--json` has them as `builders`. For Tart, a run is one builder, and results carry none.

## Cleanup as decision 36 has it

v3's automatic cleanup ran only under `serve`, had no floor for free space, and left the vanilla images Tart pulled in its cache for good. Rebuilding every image today pulled each release's, 25 to 50 GB apiece.

**Tart's cache.** Tart's source settles how its cache works (`VMStorageOCI.delete`, `gc`):
- a `:latest` entry is a link to a digest entry;
- `tart delete` of either removes it, then collects a digest nothing links to any more, unless it was pulled by digest;
- a link left broken is removed.

Its listing gives each entry's access time, and that time moves on the digest that is opened, not on the tag that named it. dockhand's own listing shows it: Tahoe's `:latest` was last touched 72 hours ago, its digest an hour ago by today's rebuild.

So the Tart provider judges digests alone (`PruneCache`). It deletes one unused for 30 days (`engine.CacheUnused`), by name, and trusts the delete only by its absence from the listing (`host.Machine.DeleteCached`). Tart takes its tags with it. A tag, a running image, and an entry Tart gives no time for are kept. It is a new capability, `buildenv.CacheProvider`, which also says where the cache is.

**When it runs.** `engine.CleanupDue` decides for every process, from files and providers alone, so a command asks after closing the database:
- a day since the last pass (the stamp beside the database, as serve's was);
- or, at most once an hour, less than `cleanup.min_free`, 30 GB by default, free where the database or a provider's cache is.

`serve` asks it on each loop. Any other command, once its own work is done, starts `dockhand clean --automatic` detached, in a session of its own, writing to `cleanup.log`, and returns without waiting. The child asks again before it runs, since another process may have run it meanwhile, and stamps first. A command that finds space short says so on standard error.

**Tests.**
- The due check, day and space alike, with the hour's pause.
- A pass pruning a provider's cache.
- Tart's pruning rules, against listings with tags, running images, and missing times.
- A command starting the pass once, `clean --automatic` running it, and the setting turning it off.
- `min_free` parsing and refusals.

The executable in a command test is the test binary, so the tests replace the spawner.

## The pid-reuse case, proven on a Mac

A session is known by its PID and its process's start time, so a PID the kernel hands to another process isn't taken for the one recorded (`coord.System.Alive`). The unit test altered a start string; this was the real case, once, by hand:
- a `/bin/cat` was started, and its PID, 86355, recorded with its start, `darwin:1790548660.14297`;
- it was ended, and more were spawned until macOS gave out 86355 again: after 98,072 spawns in 2 minutes 54 seconds, as macOS numbers processes in order and wraps;
- the new process had started `darwin:1790548834.309875`, and `Alive` judged the recorded one dead.

## Tart's fixes, checked

Checked against openai/tart on 2026-09-27, with Tart 2.39.0 installed:
- **#1353,** a listing that races a delete: still open, so the retry stays.
- **#1346,** `tart exec` after the control socket fails: still open, so guests stay on SSH.
- **#1345,** a delete of a running VM said "does not exist". It was fixed by #1350, merged on 2026-09-26, hours after 2.39.0 was tagged: commit 8ac52501 is one ahead of the tag. The absence check stays until a release carries it.

Nothing is retired yet.

## Links into deleted code

Fifty-four links in historical documents pointed at files since deleted: v2's packages, and the paths v3 renamed. Most were in the reviews, which cite code by line. Each now points at the code as the document saw it: a GitHub permalink at the commit that added the document, where the file existed then, which it did for all 54. So a review's `provider.go#L94` still lands on the line it meant, rather than on the file's last version.

One more `](url)` stays: it is inside backticks, an example of link syntax.

## Several releases at once

A check of `tart:all` built its six releases one after another, and a check on Tart and github waited for one before starting the other. The runner now builds a check's environments together, where their providers can:
- **Each provider,** at most as many at once as it says (`buildenv.ParallelProvider`), in the plan's order. Tart says two, the Mac's VMs. A provider that doesn't say builds one at a time, in the plan's order as before.
- **Different providers' side by side,** so github's run and Tart's releases overlap.

Environments are taken from each provider's list by worker loops rather than racing for a slot, so the order stays the plan's. An error from one ends the others, as it ended the drive before. The driver's problems, now noted from several environments, are guarded (`driver.problem`).

**Tart's VM slots.** Two releases starting together could each see a slot free before either VM ran, and so take one slot twice, where the person's own VM holds the other. The provider now starts its VMs one at a time (`Provider.start`): from the slot check, through the clone and start, until Tart lists the new VM as running, at most 30 polls.

**Tests.**
- `TestEnvironmentsBuildTogetherWhereTheProviderCan`: each of two environments waits for the other to begin, which it never would one at a time.
- `TestReleasesTakeTheMacsSlotsInTurn`: a live fake Mac counts VMs until they stop, and with one slot free the two releases peak at one VM and one waits. With the start lock removed it failed five times in five, and with it passes five in five under the race detector.
