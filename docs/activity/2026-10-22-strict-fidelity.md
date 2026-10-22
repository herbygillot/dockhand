# Strict fidelity (v0.5.0 part 3, item 3)

## The design, settled with architecture

The proposal went to the architecture thread, and its answer, read at da1b3d5e, settles the split:

- **`Prediction` and `Observe` live in `fidelity`, which stays pure.** It imports only macports, model, and tcl/syntax. portedit owns every evaluation and supplies the snapshots.
- **The version step is predicted at the real new version, not at a sentinel.** The evaluation with the version literal alone changed is the prediction for the version step. The full edit (checksums, revision reset, Go and Cargo blocks) must equal it plus what the edit says it writes. No follower list, fixed or observed, is needed. A sentinel would mispredict version-conditional Portfiles and component boundaries (1.9 to 1.10).
  - The trade-off: whatever the version's own evaluation changes, a dependency named with `${version}` among them, is taken as the version's. The change record still shows it.
- **A sentinel only where there's no real target value**, as for which subports a revision or checksums literal reaches. That's one evaluation per Portfile, cached in the probe's session.
- **`ReleaseScope` keeps only the partition into affected and protected.** Its judgments become typed findings, which `--shared-release` answers as a decision. `Observe` has three kinds of finding: a change not predicted, a predicted change missing, and a protected sibling moved. An evaluation that fails is "couldn't observe", never equal.
- **Retired together:** `VersionFollowers`, `writesVersion`, and `followsObsolete` with portedit's `followObsolete`. `belowSource` leaves the version rule and stays for the change record only if it still needs it.

## Step 1: one prediction type

`fidelity.Prediction` holds each port's expected comparable state and the changes it expects, in words. `Predict(before)` is the prediction of an edit that changes nothing, and `Expect` predicts a port's state.

`Observe(before, after)` is the report against the prediction. It checks the context (platform and target), the port set, each port's revision, and every field `Compare` compares, each side normalized by its own root.

`Revision`, `Checksums`, `ScopedChecksums`, the version rule, and portedit's `correctionReport` each now build a prediction and observe against it, where each had built its expectation inline and compared it its own way. Their behavior is unchanged except in two places:
- `Revision` and the version rule now also check the context, which `Equivalent` checked for the others.
- The version rule reports a revision that didn't reset as "expected 0, got N".

## Step 2: the version step is its own evaluation, and VersionFollowers goes

`ScopedVersion` is now the version step's prediction, observed:
- each member of the release the evaluation moved (`ReleaseScope`) is at the release's version, with its revision reset;
- git.branch is on the release's tag, for a member that fetches its own source;
- the selected port has the intended checksums;
- every protected sibling is as it was.

Whatever the version's own evaluation changes in a member that moves now counts as the version's change, however the Portfile derives it, where only the 14 options in `macports.VersionFollowers`, plus anything matching `writesVersion`'s substitution, could follow. Both are gone, and so is the old per-member `version()`. The dependency-block step is where architecture placed strictness, and it predicts from the version step's evaluation, never from its own after. Each step predicts from its own before plus what it writes:
- the checksums step moves the checksum fields;
- the revision step resets the revision;
- the block step starts from the archive step's prepared snapshot and moves only the blocks and what the Go and Cargo PortGroups fetch for them (distfiles, master_sites, extract.only, checksums), in the target and in every sibling that shares the release.

Before, the block step copied `VersionFollowers` into affected siblings and left the target unchecked. A collateral change the block edit makes is now refused. The new `collateral` case in `TestGoDependencyPreparation` pins it: a subport whose notes spell `go.vendors`. It fails if the siblings are predicted as their after.

Dependencies are the exception, and stay as before. A Portfile that adds `depends_lib port:other` when `$version eq {2.0}` makes a change for a person to make, not one that follows the version. `TestVersionPreparationRefusesCollateralChangesBeforeDownloading` keeps refusing it, and the fidelity unit test says it.

This is looser than before for options. A version-conditional `patchfiles` or `license`, or a path the Portfile derives from the version, now follows it. The change record still lists every field that moved. No port refused before is refused now: dependencies were never among the followers.

`belowSource` stays in `differences`. It normalizes where a port was evaluated, the build directory's hash, which every comparison of two evaluations needs, not the version.

## Step 3: the scope's judgments are findings

`fidelity.Scope` returns the release's partition into affected and protected members, plus a typed `ScopeFinding` for each sibling that moves. Each finding has one of three kinds:
- `SiblingMoves`: the sibling moves with the selected port's release. Shared authorization, `--shared-release`, answers it.
- `SiblingOwnVersion`: the sibling moves to a version of its own.
- `SiblingOwnSource`: the sibling fetches a source of its own, naming the field (git.branch, distfiles, master_sites, or checksums) that differs.

The last two have no answer yet. `ReleaseScope` is now `Scope` plus the refusal of any unanswered finding, with each finding's `String` the sentence it refused with before. The members are visited in name order, so the refusal for a family is the same on every run.

`followsObsolete` stays. Which finding a moved obsolete stub raises, none, is a judgment about the stub, not something an edit predicts. portedit's `followObsolete` writes a stub's own literal, and the version step's prediction now takes what that write moves as the version's.

## Step 4: the manifest hook is a prediction, and an unchecked one a finding (R3-20)

A patch hook that edits a dependency manifest, as editors/fresh's post-patch edits Cargo.toml, is predicted to leave the dependency block as the lock gives it. The current version's block shows that where it's exactly what the current lock gives, and then the update goes on, as before. Where the block differs from the lock, the hook may have shaped it by hand, and the update is still refused, as an override is.

The one change is where the prediction can't be shown: the block is empty, or its module moved since. That refused before. Now the block is regenerated and the result carries `ManifestHook`, which the engine adds to the update's upstream comparison as a held `manifest-hook` change. A person's update shows it, and an unattended one waits for a look (D4). `manifest-hook` joins `HoldRules`, so `holds.allow` can name it. The new `patched-moved` case of `TestGoDependencyPreparation` pins it.

## Step 5: a sibling's own archives are the update's (xapian-core)

xapian-core was refused because xapian-bindings-ruby31, of the same Portfile, fetches its own distfile. Core, omega, and the bindings share one `version` and one master site, and each has its own checksums block. The update by hand fetched each archive at the new version and wrote each block. With `--shared-release` (which a main port's update implies), dockhand now does the same:
- **The scope.** `ScopeFinding.AnsweredBy` takes `SiblingOwnSource` under shared authorization, where the field that differs is distfiles, master_sites, or checksums. A sibling of its own Git source is still refused.
- **The plan.** `sharedArchiveOwners` replaces `checkSharedArchiveOwners`. A member none of whose checksum declarations are the target's is an owner. `planObservedArchives` plans each owner in every profile as it plans the target, through the same `observeContext`, now taking the member and recording it as `archiveContext.port`, and the same coverage, protected, changed, and paired sets. A member that shares some of the target's archives but not all is still refused.
- **The check.** `fidelity.ScopedChecksumsOf` predicts each owner's own checksums by port, and a member that shared an owner's checksums takes them with it. The apply step checks each context once, with every owner in it.
- **A checksum refresh** is the target's alone, so an owner's archives are left as they are.

Tests:
- `TestASharedReleaseFetchesEachMembersOwnArchive` has the xapian shape. Both archives are fetched, both blocks written, and a named subport without the flag is refused before any fetch.
- `TestSharedReleaseRefusesUnprovableChecksumOwners` becomes `TestASharedReleaseWritesEachMembersOwnChecksumsBlock`: a subport with its own block for the same archive gets its block written.
- In `TestAFamilyMemberThatFetchesNothing`, the select subport's update with the flag now moves its sibling's archive too.

`--shared-release` is the decision that answers `SiblingMoves` and, now, `SiblingOwnSource`. Each update makes it, and `Result.Scope` records which members moved, so no stored decision is needed: a checksum refresh doesn't ask for it again.

I ran a plan on the real tree at master 97ece80, with a scratch build and database: `update xapian-core --plan` says xapian-core is already at 1.4.32. The tree's Portfile has this shape, with one checksums block shared by every binding subport, which plans as one owner's archive written once. Proving it on a real release waits for xapian's next one, so that check goes to field testing.

## Step 6: what Observe finds has a kind, and revision sharing is observed

- **What Observe finds.** Each difference between an evaluation and its prediction is now also a typed `fidelity.Observed{Kind, Port, Field}` in `Report.Observed`, beside the sentence in `UnexpectedChanges`. There are three kinds, as architecture's point 4 has them:
  - `Unpredicted`: a change the prediction didn't make, in a port it changes, in the port set, or in the context.
  - `PredictedMissing`: a change the prediction made that didn't come out so.
  - `ProtectedMoved`: a port the prediction left as it was that moved.
  Callers still refuse on `Err()` until each says its finding.
- **Revision sharing, observed.** A revbump's siblings that share the revision line move with it, as the python PortGroup's subports share their stub's. `fidelity.Revision` took a sibling as sharing it where it stood at the selected port's revision and rose by one alongside. It now takes the ports given as sharing. `portedit.revisionSharing` observes them by writing a sentinel, 7919, in the edited literal's place (`sentinelEdit`) and evaluating the family once more. A port whose revision follows the sentinel shares the line (`fidelity.Sharing`). It costs one evaluation per revbump. `TestARevbumpOfASubportSharingTheRevision`, py-lmdb's shape, passes on the observation.

The checksums sharing in `Checksums` and `ScopedChecksumsOf`, for a sibling of the same version with the same checksums before, stays a rule. A shared release now plans each member that declares checksums of its own as an owner, so what's left is a refresh's subports that share their stub's one declaration. There the rule and an observation could differ only if the edit also wrote a sibling's own declaration to the same values, and a refresh writes only the spans the binding located for the target. A sentinel for it waits for a case that needs it.
