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
