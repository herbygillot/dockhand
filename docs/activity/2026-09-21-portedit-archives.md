# 2026-09-21: portedit/archives

## Why

The [architecture review](../reviews/2026-09-21-architecture-and-organization.md)
measured `portedit.Service` as six fields and 36 methods over 19 files, and
its field usage drew a boundary: the download code used `HTTP`,
`MaxDownloadBytes`, and `DownloadTimeout` and nothing else did. The
[workspace design](../workspace-design.md) puts this split first because
it touches nothing under active work and needs no decision.

## What moved

`internal/macports/portedit/archives` holds what fetches an archive and
knows nothing about workspaces, edits, or results:

- `Client{HTTP, MaxBytes, Timeout}` replaces the three service fields;
  `Client.Store(directory)` replaces `Service.archives`, and `Store.Fetch`,
  `FetchFirst`, and `Refresh` keep their bodies.
- `Download`, with its kept path exported as `Path` because the patch
  check, the Go toolchain reader, and dependency source selection read it
  from the other side of the boundary; `Source` for a declared distfile and
  its location; `Sources`, `CheckPolicy`, `CheckFetchCredentials`,
  `LocalPatches`, `CheckChecksumSources`, and `ChecksumValues`.
- Errors wrap `portfile.ErrUnsupported`, which `portedit.ErrUnsupported`
  already was, so every `errors.Is` across the boundary holds.

`portedit.Service` is `{DependencyTools, Ports, Archives archives.Client,
Manifests}`. `commitEdit`, a `Result` method that had landed in the archive
file, is in `prepare.go`. `inertChecksumGroups` and `patched` stay, since
they serve the orchestration and need no client. The one external user of
`portedit.Download`, the preparation service's result, names
`archives.Download`; no alias.

## What did not move

The review's archive cluster counted `artifact_plan`, `artifact_apply`,
`artifact_assess`, and the two service methods in `checksums.go`. They take
the loaded workspace and produce a `Result`, so they are orchestration and
stay; the leaf is 409 lines, not 954. `portedit` is 3,867 lines after the
move.

## Evidence

The download and store tests moved with the code, the download helper
renamed so it does not shadow the client's method, and the fixtures that
set `HTTP` on the service set `Archives` instead. No behavior changed; the
full suite passes.
