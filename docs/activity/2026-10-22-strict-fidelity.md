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
