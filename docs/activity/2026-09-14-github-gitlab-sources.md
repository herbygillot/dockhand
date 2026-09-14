# Separate MacPorts source conventions from forge access

## Outcome

GitHub and GitLab PortGroup interpretation now lives in `internal/macports/source`. It converts evaluated fields into one source specification containing the forge, normalized instance, repository, current version, tag prefix/suffix, catalog mode, and livecheck metadata. `upstream` consumes that specification for explicit resolution and automatic selection. It no longer owns GitHub-specific PortGroup parsing or URL methods.

`forge.Repository` now exposes exact-tag and complete tag-catalog observations. Release catalogs are an optional `forge.ReleaseRepository` capability because only GitHub's `github.tarball_from releases` path needs them. The small `upstream.Catalog` interface binds an interpreted instance and repository. Both concrete clients satisfy it directly, and `app` selects the client by source forge.

## GitLab support

`forge/gitlab` uses `gitlab.com/gitlab-org/api/client-go/v2` for exact tag reads and paginated tag catalogs. The adapter preserves missing-tag classification, rejects invalid or duplicate observations, and leaves request construction, encoding, API defaults, and pagination to the SDK. A path included in `gitlab.instance` is treated as the leading project namespace used by the MacPorts PortGroup; the API base remains the instance URL origin.

Automatic GitLab selection applies the evaluated Atom-feed regex to an Atom-entry-shaped candidate string and uses MacPorts `vercmp`, matching the GitLab PortGroup convention without fetching or parsing its feed. GitHub continues to apply its regex to archive-shaped candidate text. These values are matching inputs only; preparation downloads the evaluated MacPorts distfile URL.

Explicit resolution, automatic discovery, and source-change checks record and verify forge, instance, repository, tag, and commit. This prevents identical GitLab project paths on different hosts from sharing a durable source identity. Existing release checkpoints without the new identity fields are incompatible, which is acceptable while v2 remains prerelease.

The version editor and fidelity comparison now recognize `gitlab.setup`, `gitlab.version`, and `gitlab.master_sites`. GitLab releases, credentials, and pull-request publication are outside this slice. GitHub publication remains unchanged.

## Authorship and validation

The source interpreter, GitLab adapter, integration changes, tests, and documentation were authored for Dockhand v2. No comments or tests were copied from Dockhand v1.

Validation covered source interpretation, URL escaping, GitHub tag and release selection, GitLab Atom matching, SDK project-path encoding, SDK pagination, missing, duplicate, and malformed tag responses, explicit source identity, full GitLab version preparation, SQLite release validation, workflow release handling, the full Go test suite, `go vet`, a production build, and `git diff --check`. A real `bump cbonsai --diff` against the local MacPorts tree queried GitLab through the new path and correctly reported version 1.4.2 as current without opening state or changing the checkout. A second preview for `torsocks` successfully traversed the `https://gitlab.torproject.org/tpo` namespace mapping and reached preparation, where the existing custom-fetch guard refused that port as designed.
