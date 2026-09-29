# 2026-09-28: the certigo run's fixes

The hugo exercise's certigo run ([review](../reviews/2026-09-28-hugo-bump-exercise.md#certigo-with-a-hands-on-binary-test); verified in the roadmap's Reviews). Findings 2 to 7, each in its own commit. Finding 1, running what a port installs, is under the roadmap's Later.

## A subject kept beside a person's edit (finding 2)

certigo's update was still uncommitted when the person fixed its version string by hand, in the same Portfile. tidy then proposed "(needs a subject)", with a ✗, though dockhand had recorded "certigo: update to 1.18.1" for that update. The recorded subject is used only while the port's changes are exactly dockhand's edits, one after another. A person's change breaks that, and there was no commit to take a subject from, since the update was uncommitted. Deriving one from the change to the Portfile found no `version` line, since certigo's version is in `go.setup`.

Now, where no commit gives the subject, dockhand's edits of the directory give it while every line they wrote still stands in the final files. It's noted, "subject from dockhand's edits, which the other changes leave standing", beside "has changes dockhand's commands did not make; review them", so the proposal is reviewed, and it carries no Generated-By. What names the edits is what names them when they stand alone (`chainSubject`): a new port's subject, else an update's, whatever edits followed.
- Lines dockhand didn't write are the person's to change.
- Once a line it wrote is gone, as when the person takes the version back, the subject is theirs to give.

That is a comparison of lines, not a reading of the Portfile. Reading a declared version, `go.setup`'s included, is still the private-helper review's finding 2, inside item 6, where tidy's other readings of a Portfile move.

`TestAPersonsEditBesideAnUpdateKeepsItsSubject` covers the kept subject and its note, a line dockhand didn't write, and a version taken back. Its chain is an update followed by a revision bump, whose subjects differ. The existing tidy test's update and checksum refresh record the same subject, so it hadn't pinned which edit names the chain. Five mutations each fail a test.

## Upstream changes worded alike in JSON (finding 3)

Under the text's Upstream heading, a change's leading "upstream: ", or "upstream's ", is left off, since the heading says it. The JSON of `update` and `submit` kept it, though its changes sit under an `upstream` key. They're now worded as under the heading.

The comparison's messages keep the word where they stand alone: in what holds a submission, and in the edit records already stored, which a view would have to word either way. So the view words them, rather than the comparison.

`TestUpstreamJSONWordsChangesUnderItsKey` covers a dependency's message and a license file's. Its mutation fails it.

## `cancel` and `wait` with no check named (finding 4)

`cancel` with no argument failed with cobra's own "accepts 1 arg(s), received 0", as `wait` did, which the sshuttle run found. `logs` already took the branch's latest check when none was named. Now `wait` and `cancel` do too, in a branch's worktree: a branch has one check at a time, so its latest is the one queued or running, if any is. Elsewhere, each says to name a check, and what it takes in a worktree. A check named is still the one used.

`TestQueueWaitCancelAndLogs` now cancels the branch's queued check and waits on its next without naming them. It waits on an older check by name while a newer one exists, and checks each command's words outside a worktree. Four mutations each fail it.

## A check stopped before it started says so (finding 5)

`check --replace` over a check that was only queued said "Stopped check-15; what it finished is kept.", though nothing had run. `cancel` said the same in its own words, "check-15 canceled; finished results are kept.", and `wait` on such a check reported "check-15 stopped; finished results are kept".

Now:
- **`check --replace` and `cancel`** know what the check was before cancelling it. One only queued is "Canceled check-15 before it started.", and one running is "Stopped check-15; what it finished is kept.", the same words from both.
- **Reporting a stopped check,** as `wait` and a foreground check do, says "stopped before anything finished" unless the check itself recorded a result. Whether it did is the evidence's to say (`Evidence.Recorded`): one of its results came from its own provider runs, not only from earlier checks of its files, whose results the evidence also carries.

Tests:
- `TestAStoppedCheckSaysWhatItLeft` and `TestACheckRecordedWhatItsOwnRunsDid`;
- the replace and cancel tests of queued checks, now expecting the new words, and a wait on a cancelled check.

Nine mutations each fail a test. Three were caught only by the direct tests: the command tests stop only checks that recorded nothing, with no earlier results.

## Serve's state as fields (finding 6)

`status --json` and `queue --json` gave serve's state only as its line, "serve: running (pid 34857) · queue: 1 run", which a script had to take apart. Beside the line, wherever it's shown, they now carry it as fields under `serve_state`: `running`, `pid`, `opens_pull_requests`, `queue` and `stopped`. The line is written from them. `outdated`, which matched the line's start to learn whether serve runs, reads the field.

What the fields say is the engine's to read (`Engine.ServeState`), as a branch's status is:
- the leader's lease says whether serve runs, and as which process;
- the queue and its stopped checks are the runs';
- whether serve opens pull requests is what the leading serve said of itself in `serving.json`, while that file's PID is the leader's.

That last comparison was the command's, as `LastServing`'s comment asked of its callers. It's the engine's now, and `lastServing` is private.

A reading that fails now fails `status` and `queue`, as judging their stopped checks already did, where before it said "serve: not running". `outdated` leaves its hint out when it can't tell, since the hint comes after its work is done.

Tests:
- `TestServeStateSaysWhoLeadsAndWhatStopped`: a check whose process still runs it and one whose process ended, a leader, an earlier serve's `serving.json`, and the leader's own;
- `TestServesStateIsFieldsAndItsLine`, for the line and the fields;
- the JSON of `queue`, and of `status --all` over a stopped check.

Seventeen mutations each fail a test.

## The work's progress kept with the run (finding 7)

check-16 spent five minutes building a PortIndex. serve's log said "Building the PortIndex; this may take several minutes", but whoever followed the check with `wait` saw nothing. `wait` shows the run's journal, and only a provider's progress was journaled. What the work reported through its context went only to the terminal of the process driving it.

Now what the work reports as an environment builds, for a person following it (progress's info level), is the run's progress too, named for the environment as a provider's is. The reports that can come then are the port index's, as the provider stages the one its guest takes:
- building it;
- seeding it from the mirror, or indexing in full when the mirror has none;
- waiting for another process indexing the same tree.

What's behind the scenes, verbose and debug, isn't kept.

To see the reports, the driver adds an observer to each environment's context (`progress.Observe`), and keeps them through the context without it, so keeping one can't report to itself. What a run keeps is the engine's to decide, for every provider alike, so the engine now imports `progress`, named in its boundary with why. An observer sees a report at the level it was made. `Quiet` lowers a foreground check's info reports to verbose for its own terminal, but it doesn't lower what the run keeps: the journal is the same whoever drives the run. So a foreground check now shows these lines too, through the journal it follows. With `-v`, it also shows them as the work's own reports.

Tests:
- `TestObserversSeeReportsAtTheLevelMade`: levels under `Quiet`, an observer beside another, reports made outside one, an observer without a reporter, and concurrent reports;
- `TestWhatTheWorkReportsIsTheRunsProgress`: a provider's info report during a quiet drive is the run's progress, named for its environment, and its verbose one isn't kept.

Nine mutations each fail a test; the one removing the observer's lock fails under `-race`.
