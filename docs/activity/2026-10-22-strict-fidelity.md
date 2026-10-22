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
