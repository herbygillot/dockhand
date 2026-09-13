# Explicit version bumps — 2026-09-13

## Scope and behavior

Committed the preceding revision-driver work as `c750217` (`feat: run durable revision bumps through preparation and verification`), then implemented explicit version bumps through the same driver.

`dockhand bump <port> <version-or-tag>` now resolves a GitHub tag, records its peeled commit, edits the supported version source, resets revision, downloads the archive, refreshes checksums, checks evaluated fidelity, and creates a tracked contribution branch. `--diff` previews the same transformation without state or branch creation. `--no-verify` stops at the recorded branch; normal attachment waits for verification admission, and `--wait`/`--trace` follow completion. Existing job-ID `wait`, cancellation, and resident `start` continue this work. Generated branches use `dockhand/bump/<port>-<job suffix>`.

The selected GitHub prefix/suffix comes from evaluated source metadata. For jq, `1.8.2` resolves to `jq-1.8.2`; the exact tag also works. No generic `v` is guessed independently of the current convention. Explicit selection can choose an older version, while an unchanged version is refused. Missing tags, ambiguous matches, API failures, and observed tag movement are distinct failures. Ordinary CLI requests currently use anonymous GitHub access; embedded applications can supply `app.Config.GitHub.Token`. CLI authentication/configuration loading is still separate work.

## Organization and durability

- `upstream` resolves requests through a small `TagReader` contract and validates recorded observations. The GitHub adapter performs bounded exact-ref HTTP reads and peels annotated tags; it does not enumerate GitHub Releases or choose a latest release.
- `record.Release` preserves requested spelling, effective version, repository, tag, commit, and observation time. It is the resolved job input; `upstream.Release` remains candidate evidence for selection/discovery.
- `workflow` uses a consumer-owned `ReleaseResolver` interface, satisfied by `prepare.Service`, to checkpoint the release in a separate claimed pass before downloading. A later pass invokes preparation. Once recorded, the selection cannot be replaced or cleared. A resumed driver uses that checkpoint rather than selecting again.
- `prepare` keeps shared source loading/evaluation in `source.go`, version editing in `version_edit.go`, checksum edits in `checksums.go`, streaming HTTP hashing in `download.go`, and version orchestration/fidelity in `version.go`. Revision and version checks share the metadata comparator. No new package was needed.
- SQLite schema 4 adds one nullable JSON column on `jobs`. Existing rows and preparation/integration checkpoints survive migration. The existing row-based storage contract, repository scoping, and operation-specific Git exclusion remain in place.
- `app` and Cobra pass action/version through the existing binding, intake, attachment, and status mechanisms. Verification planning now accepts a version-bump result as well as a revision-bump result. `PreparationRequest.Action` is explicit.

Lookup and download work run outside database transactions. Claim owner/generation/expiry still fence result adoption. A failed checkpoint write cannot start preparation; cancellation or a newer owner prevents late lookup results from overwriting state. If an observation was never durably recorded, a later resolver may see a different tag commit, but no archive or branch has been prepared from that discarded observation.

Preparation checks the tag before and after editing/downloading, validates intermediate and final MacPorts evaluations, and publishes no successful candidate when checks fail. The tag check detects observed movement; it cannot make release assets immutable or prove their relationship to a commit. Checksums identify the bytes downloaded, and later MacPorts verification checks the declared archive against those values. Download bytes are streamed, not persisted in SQLite or retained in a cache. Preview diagnostics include the release, download metadata, and both evaluations; durable state retains the release and candidate, while Git retains the edited checksums.

## Initial limits

Supported source uses evaluated GitHub metadata with a matching port version and either one literal `version` or one literal `github.setup` version argument. A literal declaration feeding `$version` or `${version}` to `github.setup` is supported. A literal revision is reset to zero; absence is supported only when the evaluated revision is already zero.

The download/edit slice requires one untagged distfile, one direct HTTP(S) master site, and one unnamed literal checksum command with sha256 and optional rmd160/size. HTTP responses, redirects, time, and size are bounded; empty/HTML/error bodies fail preparation. MacPorts exposes evaluated fetch customization and credential presence, without returning fetch usernames/passwords. Custom fetch targets/hooks, credentials, patchfiles, multiple or named archives/checksums, and vendoring are refused. Fidelity covers the selected platform/variant context and requires unchanged sibling metadata and dependencies.

Automatic latest-version selection, broader calculated version expressions, selected-subport version updates, standalone checksum refresh, publication, working-tree capture, and broader selectors remain unfinished. Missing verification setup preserves a prepared branch and records needs-attention, using the existing behavior.

## Validation

Passed `go test -race ./...`, `go vet ./...`, the CLI build, and `git diff --check`. Tests cover:

- Exact/prefix-inferred tag lookup, annotated tags, ambiguous or missing matches, rate-limit/server errors, response identity/size checks, redirect credential isolation, and cancellation.
- Native MacPorts preparation for literal and `github.setup` versions; revision reset and checksum refresh; refusal of calculated source, conditional fetch hooks, credentials, collateral dependencies, and sibling changes; tag mutation during download.
- Exact-body SHA-256/RIPEMD-160 hashing, the independent RIPEMD-160 `abc` vector, archive redirects, empty/HTML/error responses, both announced and streamed size overflow, and checksum source ambiguity/format preservation.
- Durable checkpoint before preparation, restart with no resolver available, continuation through verification, immutable release observations, failed checkpoint writes, live competing drivers, expired claims, and cancellation with a late resolver result.
- Populated schema-3 migration preserving an already prepared candidate, alongside the existing older-schema migration tests.
- Full CLI preview and tracked branch creation using native MacPorts, HTTP fixtures, and real SQLite; source/index preservation and reattachment without another download.

The new verification-continuation test exposed a remaining revision-only action switch in `verify.PlanSingle`; it now admits both bump actions. Existing preparation, workflow, state, and Tart tests passed with the change.

Live checks:

1. A preview against the actual ports tree at `11f22962ec196b85d04fd400eb13d7c1751b5c47` selected jq `1.8.1`, confirmed tag `jq-1.8.1` at commit `4467af7068b1bcd7f882defff6e7ea674c5357f4`, downloaded the archive, and changed only the setup version/checksum values.
2. A disposable shared clone seeded jq `1.8.1_3` and ran the compiled CLI with `bump jq 1.8.2 --no-verify --json`. Job `job_UGGKRCHXJEHITGS4LFLLSIMAL3` completed on branch `dockhand/bump/jq-uggkrchxjehitgs4lfllsimal3`, selecting upstream commit `34f7186b86743a083a589741b6cea95293524108`. The entire prepared tree exactly matched the original current ports tree, confirming revision reset and all checksum values. The fixture source branch and absent working index were preserved. Artifacts: `/private/tmp/dockhand-version-live-7oc486e3/`.

No new live Tart build was run for this slice. Verification continuation was tested through the common engine; the preceding committed slice already exercised its real Tart build and cross-process completion. The source ports checkout was not changed by the live version-bump checks.

## Provenance

All new code, tests, and comments were authored for v2. Existing v2 preparation, Git objects, state, driver, and verification mechanisms were extended. V1 checksum code was consulted for context; no v1 code, comments, or tests were copied. The only added dependency is `golang.org/x/crypto v0.55.0` for MacPorts-compatible RIPEMD-160 checksums, matching the version already used in v1.

Consulted primary references: [GitHub exact references API](https://docs.github.com/en/rest/git/refs#get-a-reference), [Go RIPEMD-160 package](https://pkg.go.dev/golang.org/x/crypto/ripemd160), the installed MacPorts fetch/target implementation, and the ports tree's GitHub PortGroup. The implementation remains uncommitted for review.
