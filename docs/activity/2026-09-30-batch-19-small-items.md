# 2026-09-30: batch 19's smaller items

Three of batch 19's items that don't wait on item 9's assessment, each in a commit of its own: release selection keeping its uncertainty (the [update-workflow review](../reviews/2026-09-30-update-workflow.md)'s finding 6), the URL probe off the critical path, and bump's search for other pull requests before the edit.

## Release selection keeps its uncertainty

Two of discovery's shortcuts said more than their evidence (finding 6). Its two probes are regression tests now, asserting the fixed behavior.

- **A newer version made earlier:** `predates` sets aside a tag that compares newer than the port's own but whose commit is older than the port's own tag's. dolt's `v040.15` and bat-extras' `v20200408` are such tags, but so is a release made on a branch, a tag made late, or a commit dated wrong. With nothing newer beyond what was set aside, discovery called the port current. Now:
  - **A new assessment, `upstream.Uncertain`,** beside `Current` and `UpdateAvailable`. It carries what was set aside (`Result.SetAside`: each tag, the port's version at it, the spelling `update` takes, and the tag it predates), and selects no release. A catalog whose every tag was set aside is uncertain too, where it had been an error.
  - **Setting aside stays,** so an update beyond a misspelled tag is still found and called an update, as bat-extras' `v2024.09.01` is past `v20200408`.
  - **`Resolve` with no version** refuses an uncertain port with `UncertainError`, so `update` and `bump` choose nothing.
- **Two points as proof of order:** `evaluateNewest` evaluated the two greatest captures and fell back to every candidate only on a reversal or tie between them. Now:
  - **The fast path is for a version that is its tag's,** the identity: at the port's own release (`port.Version == spec.SourceVersion`) and at the two captures it evaluates.
  - **Anything else has every candidate evaluated,** through the batched `evaluateCandidates`, and MacPorts' vercmp orders them. No comparator was added.
  - **The reversal check went:** where the version is the capture at both points, their order is the captures'.
  - broot's hundreds of tags are still evaluated twice.
- **Where it's followed:**
  - `engine.OutdatedPort.Uncertain`, with `SetAside` and `UncertainRelease` aliased for the command layer;
  - `PlanOutdated` plans no branch for such a port;
  - `outdated` lists it whether or not `--all` is given: the version with a `?`, and "update yq 5.0 after a look: v5.0 compares newer, but its commit is older than v4.44.1's". Its count ends "· 1 may have one, for a look", and a port looked at alone "may have a newer release". With `--json`, `uncertain` lists each version set aside, with `tag`, `version`, `source`, and `predates`;
  - `update --outdated` skips it, saying the same;
  - `update` and `bump` exit 3, changing nothing: "can't tell whether jq is current, so nothing was changed: …", and "If jq-1.9.0 is a release: dockhand update jq 1.9.0", or `bump` for bump. Their JSON reports only the error, as a refusal before anything is done does;
  - serve's daily look never prepares it and never counts it current. It says "serve: 1 port of yours may have newer releases, for your look: yq (dockhand outdated yq says why)", and records it in `outdated.json`, which status reads: "Your ports: 1 port may have newer releases, for your look, as serve found …".

`docs/usage.md` says it under `update`, `bump`, `outdated`, serve, and the exit codes.

### Proven

- **Regression tests:**
  - the review's two probes, one with a Portfile whose version is derived, as the rule is now scoped;
  - bat-extras' set-aside, now uncertain;
  - the engine's mapping and plan;
  - outdated's words and JSON;
  - `update --outdated`'s skip;
  - the refusals of `update` and `bump`, and an explicit version then going ahead;
  - serve's line and status's line.
- **Mutation testing:** every mutant of the new decisions is killed:
  - the identity test at the port's release, and at the evaluated captures;
  - the uncertain branch, and a catalog with every tag set aside;
  - `Resolve`'s refusal;
  - the engine's mapping;
  - serve's listing.

### Left as it is

- **A version that is its tag's everywhere dockhand looks,** at the port's own release and the two newest tags, and not below them, is taken for the identity. The review's exact probe maps 2.0 to 20.0 with the identity everywhere else, including the port's own 1.0, so no evaluation short of every tag shows it. Evaluating every tag of every port is what the fast path exists to avoid, as the review asks it be kept, so the probe's regression test derives the port's own version too.
- **A permanent misspelled tag makes its port uncertain whenever it's current,** as dolt would be with `v040.15`, until its livecheck filters the tag out. That is the roadmap's call: "uncertain instead" of current.
