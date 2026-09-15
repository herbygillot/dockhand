# xplr through GitHub verification

Rebuilt `dockhand` from the current source and exercised the README's GitHub verification path against `~/Source/macports-ports`. The requested update was xplr 1.1.1 → 1.1.2. The checkout's personal fork remote is `herby`; `origin` is the upstream MacPorts repository. Existing untracked `aqua/rockxy/` and `editors/txt/` directories were preserved.

## Command and result

```sh
dockhand -T ~/Source/macports-ports bump xplr \
  --provider github --remote herby --upstream origin --publish --trace
```

The installed `gh` login authenticated as herbygillot. Dockhand's older saved Keychain credential was rejected, so the exercise explicitly supplied the `gh` credential through `GH_TOKEN` in a child process. The token was neither printed nor written to disk, and the saved credential was not changed. This behavior and the available user remedies are documented in the GitHub usage guide.

- Original job: `job_ATGGZL7EOHGSVXIM6BQD2QOWFZ`.
- Frozen upstream base: `6e6c4e936380329061b22afd7b103993c07dd326`.
- Candidate branch: `dockhand/bump/xplr-atggzl7eohgsvxim6bqd2qowfz`.
- Candidate commit: `795da804a8ab35827197a9d1c597713479323bec`.
- Upstream tag: `v1.1.2`, resolving to `d9b9609050b885af637f66d0e0330e6a03991b05`.
- Actions: [run 34989358751](https://github.com/herbygillot/macports-ports/actions/runs/34989358751), attempt 1.
- Publication: [macports/macports-ports#34689](https://github.com/macports/macports-ports/pull/34689), confirmed by Dockhand at 15:40:14 UTC.

Source preparation checked the original Cargo dependency declaration, regenerated it using the installed `cargo2port`, refreshed source checksums, and made a single contribution commit. All three workflow jobs, macos-14, macos-15, and macos-26, succeeded. The downloaded logs include `Building xplr` and `Installing xplr @1.1.2_0` for each runner. The PR correctly identifies workflow-policy evidence rather than claiming independently verified port phases or guaranteed test success. No executable-version output was used as evidence.

`--trace` downloaded and displayed all three completed job logs (about 2.4 MiB cached). The PR head matches the candidate commit. Publication created one PR.

## Recovery and shared-run exercise

The initial second observer exposed the state constraint described below. Both local drivers were detached with SIGINT, preserving their accepted jobs. The original job resumed with `dockhand wait` and adopted the same remote run/attempt; no replacement push or workflow run was created. It then completed publication.

The same Actions run was explicitly rerun through `gh run rerun` to test attempt identity. Two newly accepted `verify xplr --branch ... --provider github --remote herby` jobs adopted attempt 2:

- Primary observer: `job_DTYSY4C2WTLQQ7LLWPH6EQEJV7`.
- Secondary observer: `job_54RYTRBGFEHAXTDHIW33CBQU67`.

Canceling the secondary observer with `dockhand cancel <job> --wait` completed locally at 15:42:49 UTC, with the explicit detail that the remote run was not canceled. A subsequent GitHub query showed attempt 2 still in progress on all three runners; the primary observer remained active. The original publication job retained attempt 1 evidence throughout. The primary observer completed successfully at 15:46:44 UTC with attempt 2 passing on all three runners. A final state read confirmed the secondary observer remained canceled and the original published job still referenced attempt 1.

## Fixes found and validated

1. The database's `UNIQUE(provider, run_id)` constraint incorrectly rejected independent workflow submissions observing one remote run. Schema 13 removes that constraint, retains per-attempt and resource constraints, and preserves existing relationships. A full two-job integration regression now covers admission, cancellation isolation, and completion. See the [state fix report](2026-09-15-github-shared-submissions.md).
2. Status formatting used numeric format verbs after converting arguments to escaped strings, producing `%!d(string=...)`. The renderer and a focused regression were corrected. See the [formatting report](2026-09-15-github-status-format.md).

The live database was backed up before migration and passed the integrity check afterwards. The full test suite, vet, focused shared-run/migration race tests, status tests, and rebuild passed. The user's untracked `docs/reviews/` directory was left untouched.
