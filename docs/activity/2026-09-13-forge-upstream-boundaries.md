# Forge and upstream boundaries — 2026-09-13

## Findings

The existing `forge` directory contained only the GitHub adapter. Remote tag/release types and missing-tag/completeness errors lived in `upstream`, forcing the adapter to import discovery policy. PR operation inputs and observations lived in `publish`, creating the same dependency in that direction. The GitHub repository-name validator also lived in `upstream`, while discovery constructed GitHub public URLs directly.

Prefix/suffix construction and recognition were repeated across explicit resolution, evidence matching, automatic discovery, and PortGroup checks. The GitHub release file contained both shared pagination and tag enumeration. Client configuration shared a file with the unfinished PR methods and consumer-package conformance assertions.

The generic GitHub `get` helper classified every 404 as a missing tag. In particular, a missing annotation encountered while peeling an existing ref could be treated as an absent candidate instead of a failed lookup. That could suppress an error during explicit version inference.

## Resulting ownership

| Location | Responsibility |
| --- | --- |
| `forge/repository.go` | Remote tag/release facts, repository access contract, and remote absence/completeness errors |
| `forge/pullrequest.go` | PR observations and desired remote-write inputs/preconditions |
| `forge/github/repository.go` | GitHub owner/name validation, repository binding, public tags/archive URLs |
| `forge/github/http.go` | API origin, version/media headers, authentication, redirect restrictions, bounded JSON decoding, and HTTP errors |
| `forge/github/catalog.go` | Shared bounded pagination for release and tag catalogs |
| `forge/github/tags.go` | Exact-ref lookup, annotated-tag peeling, tag enumeration, and tag-specific limits/classification |
| `forge/github/releases.go` | GitHub release response normalization and validation |
| `forge/github/pullrequests.go` | Existing explicit PR-operation stubs |
| `upstream/github.go` | Interpretation of evaluated GitHub PortGroup and supported livecheck conventions |
| `upstream/selection.go` | Candidate-version judgment and the shared tag-pattern operations |
| `upstream/resolve.go`, `latest.go` | Explicit/automatic selection, source consistency, and current/update/unknown assessment |
| `publish` | Publication policy and its consumer-owned operation interface |
| `app` | Concrete client wiring for both previews and durable workflows |

`upstream.RepositoryReader` binds one `forge.Repository` without network I/O. The repository owns its validated name, URL forms, and remote operations. All observations for one selection use that binding; callers no longer pass a repository string on each operation or supply tag and release readers independently. One GitHub client can bind several repositories without changing their request scope. No registry, new integration framework, or dependency was introduced.

`forge.Release` contains remote facts without a guessed Portfile version. `upstream.Candidate` adds the version being considered. `record.Release` remains the immutable durable selection; its representation and SQLite schema did not change. PR write inputs and observations moved to `forge`, while publication decisions stayed in `publish`. The concrete adapter imports neither `upstream` nor `publish`, and those capability packages import neither the adapter nor its HTTP configuration.

MacPorts option collection remains in the Tcl evaluator. Source edits to `github.setup`, archive/checksum preparation, and Portfile fidelity rules remain in `prepare`. These concern Portfile interpretation and editing, not the remote API. Similarly, state-store lookup errors remain distinct from remote-forge errors despite having similar names. Operational tag/catalog limits remain separate from upstream's overall selection deadline because they bound different work.

## Behavior and API changes

GitHub public URLs and protocol constants now have one owner. Archive URL construction preserves slash-containing tag names while encoding characters that would otherwise become URL query/fragment syntax. The API version, catalog limits, timeout values, stable-version policy, and supported source conventions remain as before.

Missing-tag classification now occurs only for an absent requested ref. The returned error preserves both `forge.ErrNotFound` and the original `github.HTTPError`. Missing annotations and catalog endpoints retain their HTTP failures and cannot count as missing-tag evidence. Incomplete catalog observations use `forge.ErrIncomplete`; upstream selection errors retain their own meanings.

Internal Go APIs changed intentionally: `github.Client.Repository` replaces client-level tag/catalog calls, `upstream.Service.Repositories` replaces separate readers, candidate matching uses `upstream.Candidate`, and publication boundaries use `forge.PullRequestInput`/`PullRequestObservation`. No compatibility aliases or duplicate structs remain. CLI syntax and stored workflow records are unchanged. PR execution is still unfinished.

## Validation

Existing exact-tag, annotation, redirect/credential, catalog-completeness, explicit selection, automatic selection, preparation, and CLI behavior tests were adapted to the new contracts. New regression coverage checks:

- Validation and URL construction without contacting GitHub, including malformed repository names and slash/special-character tags.
- Separate request scopes for multiple repositories sharing a client, with expected GitHub protocol headers.
- Missing requested refs versus failed annotation/catalog lookups, preserving HTTP evidence.
- Discovery using adapter-supplied URLs rather than hardcoded GitHub URLs.
- Failed, absent, or mismatched repository bindings before any tag lookup.

`make test-race`, `make vet`, `make build BINARY=/private/tmp/dockhand2-forge-refactor`, and `git diff --check` passed. The full suite includes native MacPorts-backed preparation, version-selection, and CLI tests. HTTP tests used local fixtures; no live GitHub or VM operation was needed. `go list` confirmed the intended package dependencies. A project scan found production GitHub web/API origins and protocol headers only in `forge/github`; remaining PortGroup names occur in their policy, evaluation, editing, or presentation contexts.

## Provenance

The refactor reorganizes existing v2 code and authors the repository boundary, shared pattern helpers, regression cases, package documentation, and this report. No v1 code, comments, or tests were copied. No Go dependency, database migration, or CLI flag was added. The existing local `dh2` binary was preserved; validation built to a temporary path.
