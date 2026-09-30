# 2026-09-30: the assessment (item 9, step 2)

The [assessment design](../assessment-design.md)'s second step: `sourcecompare` says what changed, as facts, and a new package, `internal/macports/assess`, decides what that means for the port. Hold policy had been in three places: `sourcecompare`'s `Hold` and `proven`, the engine's build-system scoping, `pythonPins`, and `toolchainChange`, and `raiseGoToolchain`'s judgment. It's now in one.

## What moved

- **`sourcecompare.Change`** no longer holds anything. It says how a file changed: a license or build file added, removed, changed, only its copyright years, or only the project's version; a dependency added, dropped, moved, or a native-library crate new to Cargo.lock; a file truncated, unreadable, or including one not followed, and on which side; an archive whose project wasn't found. Each dependency change carries its name, both versions, and a Python dependency's declarations on both sides. D9's counting moved out with the rest of the policy.
- **The version-only rule** now requires the changed line to declare the project's own version, as its build system declares one (`project.DeclaresVersion`: CMake's `project(… VERSION)`, Meson's `version:`, `AC_INIT`, setuptools, DESCRIPTION, Gradle, MakeMaker). `find_package(SomeLibrary 1.0)` moving with the version holds (the update-workflow review's finding 2, batch 19).
- **`assess.Assess`** is pure. It takes the port as MacPorts evaluates it before and after, each pair's readings, each pair's context port, the versions, the Go requirement and what the edit did about it, and the observations it asked for. It returns the findings with their coverage:
  - D9 (proven manifests counted), D12 (build files), license years, markers (a requirement applying only elsewhere), and batch 9's scoping (a build system the port's PortGroups don't name, in the context that fetches the archive). All keep their words.
  - The scoping is covered as a policy, not a proof: relevance unknown, set apart under `portgroup-scoping`, with its reason (the critique's point 7).
  - A subdirectory an archive lacks is coverage, "read at its top", not a gap. Step 1 left that decision here, and a port's other distfiles lack its subdirectory too.
- **Python pins** are judged for every requirement in question: one that changed, or whose provider port changed. `assess.Wanted` names the observations needed, the candidate provider's version first and the base provider's only where the candidate's doesn't meet. `engine.assessUpstream` observes them, in the tree before the edit and after, until nothing more is wanted.
  - **Unmet:** holds, unless the base's provider didn't meet it either, which is said with ", as the base's didn't either" and holds nothing. Where the base's couldn't be told, the finding is of unknown baseline, and holds.
  - **Version unreadable:** now holds (batch 19).
  - **No provider named for it:** said, holding nothing ("and no port the Portfile depends on is named for it"). Unless the base's Portfile depended on a port that was and the candidate's doesn't, which holds (the design's fixture, a provider removed by hand under an unchanged manifest).
- **The Go minimum** is judged as it stands, `macports.GoToolchainCovers(final go.toolchain_min, go directive)`, whatever edit made it. `macports.PortInfo.GoModuleMode` and `GoToolchainCovers` are the Go PortGroup's facts, moved from `portedit`, which still raises the minimum with its fidelity checks. An ungated requirement holds unless the base's minimum didn't gate on as much either. Where the base's go.mod wasn't read, the baseline is unknown, and it holds.
- **Findings stay `model.UpstreamChange`,** so stored edits and every reader stay as they were, and gain:
  - a rule, which with the path and subject is the finding's identity (`Key`), not its message;
  - a class: introduced, present, or of unknown baseline. A finding read from the old version's side that couldn't be read is of unknown baseline.

  `model.UpstreamComparison` gains its coverage, and `--json` carries the rule, subject, class, and coverage.
- **Each archive pair** carries the port as the context that fetches it evaluates it, before and after (`portedit.ArchivePair`), for relevance and for where to read it.

## What changed for a person

Every existing comparison test passes with its words unchanged. They were moved to `assess`, where the policy is, and their helpers now run `Compare` then `Assess`. What's new is batch 19's and the design's:
- `find_package` and other lines that move with the version, but don't declare it, hold;
- a Python provider's unreadable version holds;
- a requirement no provider is named for is said;
- a provider the Portfile dropped holds;
- a Python pin or a Go requirement that the base had already is said without holding.

## Not in this step

- **The gate still reads each edit's comparison.** Assessing a revision against its captured base, keeping readings, and the gate's own concern record, gathering the release selection, commit rules, and the search for other pull requests, are step 3's.
- **Resolved concerns** are classed but not said.
- **A finding carries no context.** A change the pairs share is said once, as before.

## Proven

- **Checks:** the full suite, `go vet`, `make fmt-check`, `vendor-check`, `deadcode`, and `lint` pass.
- **New tests in `assess`:**
  - pins against the base: met, unmet as the base met, unmet as the base didn't either, the base unknown, unreadable, and a problem said;
  - `Wanted`'s two passes;
  - a provider removed by hand;
  - a marker for elsewhere;
  - the Go minimum raised, undeclared as the requirement rises, undeclared as at the base, by hand with the base unread, covering without an edit's word, and GOPATH mode;
  - coverage of what was set apart and of a missing subdirectory;
  - holds first within a manifest;
  - an unread base being unknown;
  - the build-file probe.
- **New tests elsewhere:**
  - `sourcecompare`: the facts of each change, and the version-only probe as a fact;
  - `project`: which lines declare a version;
  - the engine: a pin judged in the base's tree, with the fake port reader answering per tree.
- **Mutation testing:** every mutant of `assess`'s decisions and of the engine's wiring is killed. Three survivors were equivalent, redundant conditions, and were simplified away; the rest were gaps, closed by the tests above.
