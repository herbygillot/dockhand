# Stable and prerelease versions

Bumping Codex to `0.155.0-alpha.15` by explicit version went through without a word, although the automatic path would never have chosen it. The v1 design called this the beta-tag trap, and the architecture rulings settle the policy: a person who types a version is asking for a legitimate thing, so the human road never refuses one, and the crossing from stable to prerelease earns a warning said once. Only an unattended road, which does not exist yet, would hold that crossing for a person.

## Design

`upstream/releasever` is a pure leaf. `Classify` reports `Stable` for dotted or dashed integers, including calendar versions, `Prerelease` for spellings carrying a recognized marker (alpha, beta, rc, pre, preview, dev, snapshot, nightly, canary, next, unstable, milestone, cr, test, or a PEP 440 letter between digits such as `1.0b2`), and `Unknown` for everything else, so a patch letter like `1.0.2u` is neither admitted automatically nor called a prerelease. `LeavesStable(from, to)` is true only for a stable current version and a prerelease target.

The automatic paths in `upstream` now use the same classifier instead of a private regexp. Every release `Resolve` or discovery returns carries `Stability` and `LeavesStable` on `record.Release`, persisted with the job. Nothing reads them to refuse: the workflow's resolution detail, the `bump` preview, `assess`, and `status` say that the change takes the port out of stable.

## Validation

Classifier tests cover twenty-four spellings; a resolution test resolves `2.0-rc1` and `2.0` from a stable port and checks the recorded classification without any refusal. Live, `assess codex --version 0.155.0-alpha.15` reports "candidate-checked … prerelease: leaves stable", and `bump codex 0.155.0-alpha.15 --diff` prints "Warning: this takes codex out of stable; 0.155.0-alpha.15 is a prerelease. The explicit version is honored." before the diff.
