# A weekly survey on GitHub's Intel runners holds the modelled context to the real Mac

A feature for v0.10.0, [#111](https://github.com/herbygillot/dockhand/issues/111), placed with the canary checks at the person's word ("Ok, do H as well", 2026-11-10 13:23Z, in the architecture thread, after asking whether a CI job like it exists: none does; CI's intel job, facts-runners.yml and the master notices are its pieces). The issue says where it stands; this file is the design, section 3 and item H of architecture's model-from-snapshots assessment of 2026-11-10, read at main 2a472cde9.

## Why

The modelled context is checked against a real Mac only when someone runs two whole-tree surveys and a comparison by hand. The Intel-versus-model survey of 2026-11-09 (an AWS Ventura Intel Mac's native record against the M1's modelled Darwin 22 x86_64 record) found three gaps that had been there since the model was built: the minor release ([#105](https://github.com/herbygillot/dockhand/issues/105)), the C++20 compiler choice ([#106](https://github.com/herbygillot/dockhand/issues/106)), and the host reads [#108](https://github.com/herbygillot/dockhand/issues/108) and [#109](https://github.com/herbygillot/dockhand/issues/109) close. None of the evaluator's tests could have seen them, because the tests assert what dockhand synthesizes, not what Base computes on the release. The step back is to make reality the test: [#107](https://github.com/herbygillot/dockhand/issues/107) holds the model to every committed probe, and this makes the surveys a standing check rather than an exercise.

## The check

Weekly, on macos-15-intel and macos-26-intel, as a notice beside base-master and decision 10's drift report.

- One workflow, the sibling of `facts-runners.yml`: install MacPorts, check out macports-ports at one commit for both runs, survey a slice natively (the runner's own Darwin 24 or 25 x86_64 with its Xcode) and the same slice modelled from the table for that release, then diff field by field. Two pieces are missing: a `tools/survey -model` flag that models this Mac's own release instead of reading it natively, and the field comparison of architecture's `cmp.py` ported into `tools/survey -compare` for records, ignoring `source_date_epoch`, the options hash and `${build.jobs}` fields. The survey's own record (`-db`) and `-sample` exist.
- Scope: a rotating slice, the whole tree over about six weeks on a 4-core runner (the Intel Mac took 9 h 55 min for 42,552 ports on 12 cores), plus every port that differed last week. A difference is a model bug with its cause in the exec rows.
- Three standing checks then measure three different things: the guest's drift report (the table against a real image's facts), the Linux survey (what the table can't answer: `closed` reads; part of the Linux design, not yet placed), and this one (what the model answers wrongly). The Tart images on the M5 can run the same comparison for arm64 through `tools/heavy` later.

## Notes on placement, from main 79a06a53

- `tools/survey -compare` compares two journals today (compare.go); the record comparison is a second form of it, over two `-db` records, pairing a modelled evaluation with the native one of the same port, release, architecture and developer tools, as cmp.py pairs them, and counting outcome differences and field differences apart.
- `-mode` already chooses what each port is evaluated against (current, installed, host). Whether "modelled as this Mac's own release" is a new `-model` flag or a value of `-mode` is implementation's call; the assessment names `-model`.
- cmp.py ignored `dockhand.base_version` as well. In the Ventura comparison every paired port differed in `code:unstable:source_date_epoch` and `code:options`, which this comparison ignores; the real differences (dependencies, variants, `build.args` and the like) were in 17 ports or fewer each.

## Size and docs

Medium, inferred. `tools/survey/README.md` gains `-model` and the record comparison. No usage docs change: nothing a person types.
