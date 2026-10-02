# 2026-10-02: literal segments of composed versions

The first piece of item 7's rest in the [roadmap](../roadmap.md): a composed version's literal text as an editable input.

## What changed

- **A composed word's literal text is a version candidate** (`portfile.Candidates`). llvm declares `version ${llvm_version}.1.7`, where the major is the port's name; only whole literals were candidates, so `19` was probed, changed the name, and nothing edited `1.7`. A word that isn't one literal now offers each literal segment holding a digit, without the separators that join it to what's substituted: `1.7` here, `0.12.1` in openjdk's `${feature}.0.12.1`, and `1.2` in a proc's `1.2.${patch}`. The candidate is probed as any other, so it's an input only where evaluation shows the source version moving with it; an update across a major, which is another port, has no edit and is refused.

## Measured

`tools/survey` over the 23 llvm Portfiles and openjdk 11, 17, 21, and 25 at efde56cae1f, before and after: 15 ports move from unknown to input-found, none regress. llvm 13 to 23 are covered; 3.3 to 12 now stop at their checksums instead, a declaration outside the observed contexts, which is another bucket.

openjdk counts as input-found, but its tag carries a build number in a second literal (`set build 1`, `jdk-${openjdk_version}+${build}`). A release changes both, and an edit is one literal, so a real openjdk update still finds no edit and is refused (inferred from `versionInputs.edits`, not run against GitHub). Coverage there is overstated by the survey until an edit can set two literals.

## Verification

- `TestCandidatesFindLiteralSegmentsOfComposedVersions`, and `TestForwardVersionProbing`'s llvm case, which fails on the old code.
- `TestLocalAssessmentKeepsFailedCounterfactualUnknown` guards both of its inputs, since `1.2` became one.
- With `DOCKHAND_TEST_MACPORTS_TCLSH`: the `macports`, `outdated`, `engine`, and `tools` packages; fmt-check, vendor-check, vet, deadcode, and lint, each read for its exit status.
