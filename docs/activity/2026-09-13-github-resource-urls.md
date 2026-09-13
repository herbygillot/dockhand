# GitHub URL and resource audit

Reviewed URL, filename, ref, repository-name, and resource-name construction across `internal`, following the GitHub-related values into discovery, preparation, publication, and Git. Compared those uses with the installed `go-github/v91` v91.0.0 source and the local MacPorts GitHub PortGroup.

## Changes

- `RepositoryInfo` now returns `Repository.GetCloneURL()` instead of assembling `https://github.com/<full_name>.git`. The existing remote parser checks that the supplied clone location identifies the intended GitHub repository before it can become a Git fetch target. Missing or inconsistent locations fail instead of being invented.
- PR observations retain `PullRequest.GetHTMLURL()` without constructing an expected `/pull/<number>` URL. Repository, number, branch, and commit checks remain; the web URL must be present. It is a display location, while subsequent SDK operations use the structured repository and number.
- Renamed `TagsURL` to `TagsPageURL` and `TagArchiveURL` to `TagLivecheckURL`, including contracts and callers. These names distinguish livecheck's browser-link convention from SDK API and download URLs. The interface documents this distinction.
- Exact-tag resolution constructs one full `refs/tags/<name>` value for validation, the SDK call, and response matching. `Git.GetRef` already removes the optional `refs/` prefix and escapes path segments.

## Remaining constructions and SDK equivalents

| Value | SDK capability | Decision |
| --- | --- | --- |
| Clone location | `Repository.CloneURL` / `GetCloneURL()` | Use the response field. |
| PR and release web links | `PullRequest.HTMLURL` and `RepositoryRelease.HTMLURL` | Use response fields; release observations already did. |
| Repository and fork-parent names | `Repository.FullName` and `Parent.FullName` | Already use response fields. Combining evaluated `github.author` and `github.project` before lookup remains input construction. |
| GitHub tags page | `Repository.TagsURL` is the REST tags endpoint, not the browser page | Retain `TagsPageURL` without adding a metadata request. |
| Archive-shaped livecheck candidate | `RepositoryTag.TarballURL`, `RepositoryRelease.TarballURL`, and `Repositories.GetArchiveLink` identify downloads | Retain `TagLivecheckURL` for the PortGroup regex's `archive/refs/tags/...tar.gz` shape. It is not used for downloading source. |
| PR head selector | SDK `PullRequestListOptions.Head` and `CreatePullRequest.Head` require `owner:branch` | Retain that input formatting; let the SDK serialize and encode it. |
| Tag ref namespace | `Git.GetRef` accepts the ref string and handles its URL encoding | Supply a full ref; no custom API path or escaping. List/release checks still validate tag names in that namespace. |
| Version prefix/suffix | No SDK knowledge of Portfile conventions | Keep `TagPattern` in upstream. |
| Distfile name and URL | `ReleaseAsset.Name`, `BrowserDownloadURL`, and `DownloadReleaseAsset` exist for API-selected release assets | The current preparer uses evaluated `distfiles` and `master_sites`, which may describe a source archive, a release asset, or another HTTP source. Preserve MacPorts' selection instead of substituting an SDK archive or asset. |
| Local worktrees, artifact files, VM names, and coordination keys | Dockhand/Git/Tart resources, not GitHub resources | Keep their construction in their owning packages. |

The checked MacPorts `github-1.0.tcl` assigns the tags page to `livecheck.url` and an `archive/refs/tags/...\.tar\.gz` pattern to `livecheck.regex`. Switching these candidates to REST tarball URLs would change which versions the same Portfile accepts. `GetArchiveLink` would also add a network lookup for a download location that this filtering step does not need.

No SDK helper was found for formatting GitHub browser tags-page links, `owner:branch`, complete Git ref names, or MacPorts distfile conventions. SDK endpoint construction and query/path encoding remain entirely in `go-github`. No new API call, dependency, package, schema, or CLI option was introduced.

## Validation

`go test -race ./internal/forge/github ./internal/upstream ./internal/workflow -timeout 180s` passed, including publication through the CLI. `CGO_ENABLED=0 make build`, `make vet`, and `git diff --check` passed. No live GitHub writes or VM builds were performed.

Regressions cover preserving a supplied clone URL's spelling, rejecting absent or mismatched clone metadata, preserving the SDK's PR web link without assuming its shape, and rejecting incomplete or wrong-repository PR observations. Existing coverage checks livecheck matching, escaped tag names, tag peeling, and publication through the CLI.

All changes and test updates were authored for v2. No v1 or SDK comments or tests were copied. Changes remain uncommitted with the preceding SDK migration and defaults cleanup.
