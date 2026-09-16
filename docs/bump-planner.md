# Evaluator-driven bump preparation

The initial targets are existing Terraform/Helm release-series subports, Deno's architecture archives, and gh's source/binary branches. Full Rust maintenance is outside this work. Frozen release lines and independently pinned auxiliary sources must remain unchanged unless explicitly selected.

## Responsibilities

- `macports` owns bound source contexts and neutral metadata/reader contracts.
- `macports/eval` implements native Tcl sessions, compatibility checks, selector resolution, metadata decoding, and MacPorts version comparisons. Declaration provenance, native fetch observations, and explicitly modeled contexts also belong here. Observation does not authorize an edit.
- `macports/portfile` and `tcl/syntax` identify precise source spans and preserve formatting.
- `macports/portedit` selects candidate edits, establishes the affected and protected sets, and validates the complete plan.
- `macports/distfiles` owns artifact/checksum associations, stable declaration identities, and precise digest spans. Native fetch interpretation stays at the evaluator boundary; HTTP transfers stay in `fetch`.
- `upstream` owns discovery and release selection. Explicit-version preparation must not require a recognized forge source. Discovery, complete preparation, and actual builds remain separate capabilities.
- `workflow/preparation` owns disposable workspace lifetime and storing the completed tree. Tcl and checksum policy do not move into workflow.

Concrete evaluator dependencies belong in application wiring. Capabilities consume small interfaces, not evaluator constructors. Keep shared types in `macports` to prevent dependencies from cycling through the concrete implementation. No aliases or forwarding constructor are retained in the parent package.

## Implemented sequence

1. Extract the native evaluator without changing metadata or selection behavior. Move its tests and embedded scripts with it. Preserve source/path validation and native platform enforcement.
2. Establish declaration ownership and artifact observations. Represent exact source locations, context, evaluated filename, fetch locations, and checksum declarations. Filenames alone are not artifact identities. Use MacPorts' site/tag interpretation rather than reconstructing mirror semantics in Go.
3. Build scoped edit plans using syntax, runtime observations, and counterfactual probes. Test the actual requested candidate and reject ambiguous edits. Reset only revisions that belong to the updated release. Keep upstream references, editable inputs, and evaluated versions distinct.
4. Collect the relevant alternate contexts and preserve independent pins. Native evaluation remains the verification authority. Modeled contexts are explicitly identified and cannot establish that a build ran on another OS. Host-dependent execution that defeats the model must remain a reported gap.
5. Complete Deno preparation, existing Terraform/Helm subport updates with explicit versions, and gh's conditional artifact coverage. Creating new release series is a different operation. Expand discovery separately.
6. Repeat the survey and prepare concrete candidates before claiming additional support. Compare archive-complete preparation, discovery, and build coverage separately.

Alternate contexts must be request-scoped and isolated: do not change a shared evaluator's default platform or allow one modeled context to leak into another. Keep `Reader.Evaluate` native; introduce modeled observation through an explicit separate operation with recorded overrides and limitations.

A plan records the selected release, exact edits, affected artifacts/contexts, protected declarations, and unresolved requirements. Apply it in a disposable workspace, then evaluate the final result without tracing. `assess` and `bump` must use the same planning mechanisms; assessment reports unperformed download/helper/build stages explicitly.

## Preservation and coverage fixtures

- Terraform/Helm: a selected series moves while its siblings and obsolete parent remain unchanged.
- Deno: both architecture archives move although their filenames do not contain a version.
- gh: a shared release controls source and binary archives in different macOS branches.
- iTerm2: modern release edits preserve independent older-macOS version/revision/checksum pins.
- libusb: stable and development subports remain independently selectable.
- love-0.7: the main archive can move while pinned GLee auxiliary files remain unchanged.
- smartmontools: dotted version text maps to an underscore-separated upstream reference.

Use focused fixtures to establish these behaviors; real Portfiles validate that the model generalizes. Equal checksum values in different declarations, overridden values, unchanged filenames with changed URLs, and version changes that activate new branches need explicit regressions. A changed observation is evidence of impact, not automatic permission to broaden the requested update.

## Survey evidence

The 2026-09-15 design survey counted 20,087 committed Portfiles at ports commit `87ff2b89b11d1666d39a51929499c5787bebecdf` and ran 147 preparation assessments, including 140 random selections within four structural strata.

Among 4,706 Portfiles with explicit GitHub/GitLab/Go source clues, 634 had selected complex structures. Of 60 random examples from that group, 14 were rejected only by checksum handling, 26 found an input, 19 had other unsupported checks, and one was inconclusive. Of 45 examples with direct version/checksum declarations but no source clue, nine failed only source interpretation and another 26 also failed the direct-HTTP(S)-site restriction. Fetch failures skipped checksum checks.

These counts describe observed obstacles, not promised future successful updates. The census counts Portfiles rather than subport names, does not exhaustively traverse arbitrary generated Tcl, and does not establish archive availability or release/build success. It supports prioritizing scoped checksum ownership and native fetch planning alongside generalized version probing. The raw survey and harness are retained outside the application checkout in the shared workspace's `surveys/2026-09-15-bump-coverage` directory.

No new production dependency is required for the first stages. Keep the existing parser and native Tcl evaluator; evaluate other parsing or property-testing libraries only against a demonstrated gap.

## Current behavior and limits

The initial implementation supplies each of the six stages above within the supported cases below. `assess` and `bump` share scoped planning. `assess terraform --subport terraform-1.16 --version 1.16.2` checks an explicit archive version; `bump terraform 1.16.2 --subport terraform-1.16 --diff` also downloads the affected archives and previews the completed edit. No forge identity or Git commit is manufactured for HashiCorp's archive source.

Candidate generation remains deliberately bounded by supported literal transformations and actual forward evaluation. It is not a general Tcl inverse or taint engine. Multiple independently edited version inputs, dynamically generated source declarations, new release-series creation, coordinated Rust/bootstrap updates, and automatic discovery from arbitrary livecheck pages remain outside the implemented preparation scope. Changes to unselected sibling metadata are still refused; a shared multi-subport update needs an explicit contribution scope.

Modeled observations use fresh interpreters and keep native runtime facts separately. Their profiles cover the native host, relevant arm64/x86_64 choices, and nearby literal Darwin comparisons, including multiline expressions and comparisons assigned to variables. They do not claim full host virtualization or build coverage. Unresolved boundaries and uncovered checksum groups remain visible; observed host processes, external file reads/sources, filesystem queries, and directory enumeration during modeled Portfile evaluation are reported as gaps. The model does not certify arbitrary PortGroup execution or enumerate every possible variant/environment.

Tests and real archive exercises are recorded in [the implementation report](activity/2026-09-15-scoped-bump-planner.md). The survey comparison distinguishes input discovery from complete archive preparation and actual verification.

The [planner hardening report](activity/2026-09-15-planner-hardening.md) records missed-context regressions, context-wide revision resets, host-access guards, and targeted probing. Unresolved Darwin-major reads are refused individually; a recognized comparison elsewhere in the same command does not establish their coverage. This source analysis still does not resolve arbitrary dynamically constructed Tcl or certify every PortGroup/environment dependency.
