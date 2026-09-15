# Live isolated dependent verification

Exercised the rebuilt CLI against provisioned `dockhand-base-tahoe`, MacPorts 2.12.6, Darwin 25 arm64, with the recorded shared capacity of two guests. No PRs were created and the user's ports checkout was not modified.

## Real cohort

```sh
dockhand -T ~/Source/macports-ports verify libmd --branch master \
  --dependents --provider tart --image dockhand-base-tahoe --wait
```

Job `job_CSWMH5RCHIW7P5R3C5Q6RIVGXY`, source commit `87ff2b89b11d1666d39a51929499c5787bebecdf`:

- `libmd`: passed in its own guest.
- `signing-party`: passed in a separate guest with the accepted `libmd` built/installed first.
- Both guests were released. The full frozen-source index took about 3m11s; subsequent staging reused it.

## Conflicts, external dependency failure, and restart

A temporary shared local clone added five synthetic `devel/dockhand-exercise-*` ports. Each has no distfiles or configure step, an empty build, and a destroot writing a fixture under `${prefix}/share`. The root has no dependencies. Dependents `a` and `b` both depend on the root, declare each other in `conflicts`, and install the same destination file. A third dependent requires both the root and an unrelated port whose `pre-build` raises an intentional error.

Job `job_LKNH5UAPO5SCP5IHSXVPLU7MGY`, source commit `7f2182cf669e8992c76c320d601b382cd64fce3c`, tree `a0d5107eebdd72ebd9941f0eb31a4013f62ac380`:

1. Ran `verify dockhand-exercise-root --branch master --dependents --provider tart --image dockhand-base-tahoe` without `--wait`.
2. The invocation returned after first admission, with the durable cohort and queued attempts visible.
3. A separate `dockhand wait job_LKNH5UAPO5SCP5IHSXVPLU7MGY` resumed the job through settlement.
4. Root and both conflicting dependents passed independently. The failure target failed in `dockhand-exercise-unrelated`, phase `build`, classified as a dependency failure. No inference that the root caused it was made.
5. The final job was failed with `3 passed, 1 failed`; the passing root did not override the cohort result. Successful guests were released; the failed guest/evidence remained retained by the normal diagnostic policy. The other guests continued despite that failure.

The temporary clone is retained at `/private/var/folders/l6/xhprvp0x6_q4239mpj1zp3_40000gn/T/dockhand-cohort-exercise-1z5ha57j` for inspection, along with the durable job records. Full-cohort publication rejection is covered by the existing fixture-forge regressions; no synthetic PR was submitted.

## Finding and correction

CLI progress repeated “waiting for provider admission” once per queued target. It now emits a single count for multiple queued targets, with a focused rendering regression. No scheduling or provider correctness defect was found in these runs. Separate per-target images, baseline comparison, downstream revision edits, and broader selectors remain follow-up work.
