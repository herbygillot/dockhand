# Automatic version selection — 2026-09-13

## Scope and organization

Implemented the approved next slice: `bump <target>` without an explicit version selects an eligible upstream release, then uses the existing durable preparation and verification path. No new package, module dependency, table, or schema migration was needed.

- `upstream/latest.go` owns eligibility and current/update-available/unknown assessment. `Resolve` delegates omitted versions to this capability; explicit tag selection remains intact. `Discover` can evaluate an already-bound MacPorts context without submitting work.
- `forge/github/releases.go` supplies paginated Release and repository-tag observations. Exact-tag lookup and catalogs share the bounded HTTP helper in `http.go`; only exact-tag 404 responses mean a missing tag. Catalog failures discard partial results.
- `macports/versions.go` and its Tcl script apply evaluated livecheck regexes and MacPorts `vercmp`. `upstream` consumes this through `VersionSelector`; it does not implement another version-ordering scheme.
- `record.Release` adds optional `CurrentVersion` and `NoUpdate` fields to the existing JSON checkpoint. Accepted `JobSpec.Version` stays empty for automatic requests. The SQLite write contract requires an input version and restricts a no-update result to a completed automatic job without a candidate or result revision.
- `workflow` records selection before preparation. A current observation completes atomically with that checkpoint, without preparation, branch integration, or verification. Update-available jobs reuse the existing claimed preparation, guarded Git integration, and provider lifecycle. No HTTP call or Tcl evaluation occurs inside a database transaction.
- Application construction and previews wire the same release catalog and comparison capabilities. Cobra now accepts omitted versions and explains successful no-op behavior. README and the CLI, architecture, components, and state documents describe the implemented scope.

## Selection policy and limits

The initial automatic path requires a GitHub PortGroup convention matching the evaluated port version, a stable numeric version (digits and dot-separated numeric components), and regex livecheck against that repository's GitHub `/tags` URL. `livecheck.version` must equal the evaluated version. The tag prefix/suffix remain authoritative.

`github.tarball_from releases` considers published GitHub Releases. `archive` and `tarball` enumerate tags, so projects need not publish Releases; visible Release metadata still excludes drafts and marked prereleases. Textual prerelease versions are excluded even if GitHub metadata calls them stable. Explicit version selection remains the route for prereleases and other version spellings.

The evaluated Tcl regex is applied to candidate archive URLs, with its first capture required to equal the tag-derived version. This supports the usual GitHub pattern and compatible maintainer version-line filters. It does not execute arbitrary livecheck scripts or scrape page HTML. Custom URLs, disabled livecheck, regexes requiring surrounding HTML, and unsupported conventions require further work or explicit selection. Existing version-editor and single-archive limits still apply to updates.

Catalog collection requests 100 entries per page, with a maximum of 20 pages per endpoint, bounded responses, and deadlines. A short page establishes the observed end; hitting the bound without it is incomplete. Detected duplicate tags, malformed data, HTTP failures, no eligible matches, ambiguous newest versions, or failed selected-tag lookup remain unknown/needs-attention. Pagination is not an atomic GitHub snapshot: concurrent upstream changes cannot always be detected. The latest eligible version equal to or older than the source yields a successful no-op. That is one durable observation, not ongoing freshness monitoring or a passed build.

The selected tag is resolved to an exact commit before checkpointing. Resumed update jobs reuse the checkpoint and existing before/after tag checks. A no-op `wait` does not repeat discovery. CLI credential/configuration loading remains separate work; production CLI lookups currently use anonymous GitHub access. The default `--from-source=false` remains unchanged.

## Evaluator correction

A live jq preview exposed an existing double decoding bug. `syntax.DictValues` had already decoded each Tcl dictionary element, but the evaluator then passed every value through `syntax.ListValue` again. This removed the list quoting needed to preserve jq's `\d` regex when Tcl subsequently joined it, turning the capture into an incorrect pattern.

The evaluator now preserves raw evaluated Tcl values and raw option-error strings after one dictionary decode. Consumers interpret list-valued options at their boundary. This also means raw description options retain MacPorts' list quoting; the native evaluator expectation now reflects that contract. Regression tests cover regex escapes, literal braces, and error strings, and the CLI fixture uses a `\d` capture.

## Validation

Focused tests cover complete pagination, later-page failure, truncation, duplicates, malformed metadata, tag-only projects, stable/prerelease filtering, native numeric ordering, version-line restrictions, current/ahead results, ambiguity, and unavailable observations. Workflow tests cover both explicit and automatic checkpoint/restart paths through branch creation and verification, plus no-op completion, no provider calls, immutable observations, and failed completion checkpoints. CLI tests exercise previews without a database, automatic branch preparation, already-current success despite an unavailable image, reattachment without rediscovery, and discovery failure reporting.

Live checks used the installed MacPorts Tcl runtime and GitHub API:

- The real ports checkout's jq 1.8.2 preview selected `jq-1.8.2`, recorded `NoUpdate=true`, and produced an empty diff without a database. Output: `/private/tmp/dockhand-auto-jq-preview.json`.
- A disposable clone seeded with jq 1.8.1 revision 3 ran `bump jq --no-verify --json`. It automatically selected 1.8.2, downloaded and checked the archive, and completed a new contribution branch. The resulting entire Git tree exactly matched the original ports tree containing jq 1.8.2 revision 0. The seed branch and absent working index were preserved. Artifacts: `/private/tmp/dockhand-auto-live-791bfdxa`.
- The temporary binary was built at `/private/tmp/dockhand2-auto`; the user's existing local `dh2` binary was preserved.

`make test-race`, `make vet`, the temporary binary build, and `git diff --check` all passed. Native MacPorts tests ran, including the new regex and CLI cases. No live Tart VM build was repeated for this slice; workflow tests cover continuation into the shared verification path.

## Provenance

All new code and tests were authored for v2. Existing v2 exact-tag HTTP mechanics were extracted into a shared helper, and existing v2 tests were extended. v1 was consulted for context; no v1 comments or tests were copied. No dependencies were added.

GitHub API behavior was checked against the official [Release listing documentation](https://docs.github.com/en/rest/releases/releases#list-releases), [repository tag listing documentation](https://docs.github.com/en/rest/repos/repos#list-repository-tags), and [pagination documentation](https://docs.github.com/en/rest/using-the-rest-api/using-pagination-in-the-rest-api). Regex joining and version comparison were checked against the locally installed MacPorts `portlivecheck.tcl` and GitHub PortGroup.
