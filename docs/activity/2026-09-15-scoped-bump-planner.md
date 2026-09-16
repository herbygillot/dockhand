# Scoped bump planning

Implemented the initial evaluator-driven planner after extracting `macports/eval` and adding declaration/fetch observations.

## Organization and behavior

- `macports/eval` owns fresh native/model sessions, source frames, and MacPorts fetch planning. `macos` supplies Darwin/product-version facts without expanding supported VM provisioning releases.
- `macports/portfile` locates exact source commands, normalizing Tcl's continued-line folding while refusing ambiguous ownership. `macports/distfiles` associates evaluated checksum groups with native fetch locations and stable declaration identities.
- `macports/portedit` probes actual version candidates, resets the selected literal revision, plans archive changes across relevant architectures/OS boundaries, protects independent pins, and verifies the final contents without tracing. Counterfactual numeric/substring/separator relations only generate candidates; actual evaluation decides validity. Unselected sibling changes remain refused.
- `upstream` and `record.Release` distinguish an explicit archive version from a forge tag/commit. Terraform preparation no longer requires a fabricated forge source. Automatic non-forge discovery remains separate work.
- `assess` uses the same source, context, archive, and fidelity checks. It accepts `--subport` for an explicit Portfile and selects indexed subports within their parent. JSON exposes evaluated contexts; preparation exposes modeled/native coverage separately from build evidence.

All new implementation and regression fixtures were authored for this architecture. V1 informed the platform-model review; none of its comments or tests were copied. No production dependency was added; numeric candidate mapping uses Go's standard `math/big` package.

## Validation

Native fixtures cover constant archive names with changed URLs, distinct architecture digests, source/binary OS branches, calculated subport versions, scoped revisions, older-OS pins, unchanged auxiliary archives, equal checksum values in separate declarations, changed declaration ownership, conflicting digests, ambiguous inputs, arithmetic/separator transformations, cancellation, profile isolation, and host-dependent modeled observations. Existing dependency generation and stored-Git-tree preparation tests remain in place.

The complete project suite, `go vet ./...`, and a fresh CLI build were exercised. Details of final reruns and survey results follow below.

## Real archive exercises

All exercises used disposable copies; the user's ports checkout and branches were not edited. The selected source corpus came from ports commit `87ff2b89b11d1666d39a51929499c5787bebecdf`. Deno and Helm were replayed from earlier committed Portfiles because the selected lines were already current: Deno before `3393469cc03e584134585e5b0d26808eb2104efa`, Helm before `50ca78222be61e6694c7bf2d717c642e4020c89b`.

| Target | Update | Archives refreshed | Complete preparation |
| --- | --- | --- | --- |
| deno | 2.9.5 → 2.9.6 | aarch64 and x86_64 ZIPs | Passed |
| gh | 2.100.0 → 2.101.0 | source tarball and older-macOS amd64 ZIP | Passed |
| terraform-1.16 | 1.16.0 → 1.16.2 | arm64 and amd64 ZIPs | Passed |
| helm-4.2 | 4.2.3 → 4.2.4 | arm64 and amd64 tarballs | Passed |

Every exercise downloaded real public archives, regenerated SHA256/RIPEMD160/size, and passed final uninstrumented evaluations. Terraform/Helm siblings and obsolete parents remained unchanged. The prepared Portfiles and download/digest reports are retained in the shared exercise workspace. These results establish archive preparation, not builds on every modeled platform. No port PRs were opened as part of this pass.

Smartmontools additionally passed candidate planning for the dotted Portfile version / underscore tag mapping. The obsolete Terraform and Helm parent ports correctly remain unsuitable archive-update targets; select an existing release-series subport.

## Repeated survey

Reused the exact 147-Portfile sample from the design survey: 140 stratified random choices (seed `20260915`) plus seven named controls, against the same committed ports tree. This is a comparison sample, not a full-tree success-rate estimate. The earlier structural census covered 20,087 Portfiles. The same 25-second per-Portfile budget was retained. Per-file outcomes are in [the comparison table](2026-09-15-bump-survey.tsv).

| Local assessment | Before | After |
| --- | ---: | ---: |
| input-found | 45 | 85 |
| unsupported | 101 | 54 |
| unknown | 1 | 8 |

Forty-two previously unsupported Portfiles now find an editable input with supported local archive declarations. Two previously input-found results become unknown because the expanded context checks expose an unmodeled OS condition or external host-state dependence. Additional unknowns include unresolved tag spelling, failed probes, host-dependent modeled evaluation, and one 25-second timeout for beets. None of these is treated as success.

The comparison took approximately 175 seconds versus the baseline's 96 seconds; median per-file time increased from 0.45 to 0.77 seconds. That reflects additional native fetch/declaration observations and modeled-context coverage. These numbers are survey diagnostics, not driver-cycle or build performance measurements. No extrapolated count of successful updates is claimed. Automatic discovery remains distinct: many new explicit-archive inputs still require a version supplied by the contributor. Terraform/Helm metaports remain unsupported in the primary-port sample; the explicit subports were exercised separately above.

## Final checks

- `go test -p 1 ./...`: passed, including existing CLI, workflow, dependency-generator, and stored-source integration suites.
- `go vet ./...`: passed; the CLI was rebuilt.
- `dockhand -T <ports-tree> assess terraform --subport terraform-1.16 --version 1.16.2 --json`: candidate-checked, without downloads or jobs.
- `dockhand -T <ports-tree> bump terraform 1.16.2 --subport terraform-1.16 --diff --json`: passed the complete application/upstream/preparation/Git validation path. Its diff changed only the selected series' patch input and two archive checksum groups. Coverage identifies arm64 as native and x86_64 as modeled metadata.
- The follow-up coverage-reporting check also passed. Raw survey inputs/results, public archive digests, prepared Portfiles, and CLI preview evidence are retained under the shared workspace's `surveys/2026-09-15-bump-coverage-after` directory; the original survey was preserved.

Known conservative limits include shared edits that would change unselected siblings, revision declarations that cannot be reset without touching a protected release, host/SDK-dependent conditions beyond the modeled facts, and checksum declarations whose runtime ownership cannot be established uniquely. Creating a new release series, coordinated Rust maintenance, and generic livecheck discovery remain separate work.
