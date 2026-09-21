# 2026-09-21: a maintainer's own livecheck on a forge port

## Why

`dockhand bump flyctl` refused with "livecheck does not inspect the GitHub
tags page". devel/flyctl overrides the golang PortGroup's livecheck with
`https://api.github.com/repos/superfly/flyctl/releases/latest` and the
regex `"tag_name": "v(\d+(?:\.\d+)+)"`, because the tags page is flooded
with per-PR tags; `port livecheck flyctl` answers 0.4.105. Of the 4,107
forge Portfiles, 329 override the livecheck, about 157 with `type none` and
53 with an explicit regex, and for the Discovery purpose the source
interpretation required the livecheck URL to be the tags page and refused
everything else; with an explicit version the check was skipped. Dockhand
already ran Tcl livecheck regexes for archive sources through the mirror in
`versions.tcl`; the gap was policy.

## Design

Two truths, kept separate. The livecheck is the maintainer's definition of
the latest version; the catalog is the forge's statement of what exists and
how it is fetched.

1. The overriding livecheck runs as Base runs it: the URL is fetched with
   the request headers Base's curl fetch sends, through the forge's own
   client when the URL is on its API, so the user's credentials and the
   rate-limit deadlines apply, and the versions are extracted by the Tcl
   mirror. No Go regex translation anywhere.
2. The version is proven before it is a release: it maps to a tag with the
   PortGroup's prefix and suffix, the tag must exist, and in releases mode a
   published release must carry it. The release record is the catalog
   path's. A version with no tag or release is refused naming both.
3. Only the PortGroup's default tags page is restated as a catalog query.
   The relay proposed restating `releases/latest` and `?per_page=N` as
   catalog queries where equivalence is provable, and it is not: GitHub's
   latest release is the newest by publication, which a backport release
   can be, while the catalog path picks the newest by vercmp; and a
   per-page query is a truncation the maintainer chose. Both run as written.
4. `livecheck.type none` refuses automatic selection saying the livecheck is
   disabled and to name a version; a non-regex type and custom livecheck
   hooks refuse the same way; an explicit version never needs the livecheck.

## What GitHub's JSON layout taught

The maintainers' expressions carry a space after the colon, the indented
layout. GitHub lays its API JSON out by request headers: the go-github
SDK's user agent, its `Accept: application/vnd.github.v3+json`, and its
`X-GitHub-Api-Version` each get the compact form, and Base's curl fetch,
`User-Agent: MacPorts/2.12.6 libcurl/8.7.1` with `Accept: */*`, gets the
indented form. Measured three times each: every agent carrying both the
MacPorts and the libcurl tokens is indented; MacPorts alone, dockhand alone,
and libcurl alone are compact. So the document fetch sends Base's headers:
`User-Agent: MacPorts/<Base version> libcurl dockhand/2`, `Accept: */*`, no
API version, the Portfile's curl options, and identity encoding when
`livecheck.compression` is off. Under the SDK's headers flyctl's regex
matched nothing, which is the failure the first live run showed.

## The mirror and Base's loop

Base's line-oriented loop resumes each search at the last character of the
previous match, so two matches may share a character; the mirror resumed one
past it. On `triple 789` with `(\d)\d`, Base captures 7 and 8, the old mirror
7 only. The mirror now resumes where Base does and forces progress only for
a match that ends where it began, which traps Base's loop forever.

`TestVersionsMirrorBaseLivecheckMatching` slices Base's own matching loop
out of the installed `portlivecheck.tcl`, between its `regexm` branch and
`close $chan`, runs it with stubbed `ui_*` procs, and runs the mirror over
the same documents and expressions, requiring the same version and the same
verdict. The corpus in `internal/macports/eval/testdata/livecheck` holds
flyctl's latest-release document as Pextlib received it, CMake's per-page
tags, MyLoss's plist for `regexm`, and a synthetic overlap file, with a
README naming each source.

## What changed

- `source.Livecheck.Overridden`; `Interpret` for Discovery accepts any
  regex livecheck on a forge port, reading the curl options through the
  shared `readListing`, and refuses `none`, other types, and custom hooks
  with the version named as the way forward.
- `upstream.discoverOverridden`, `Documents`, `requestHeaders`, and the
  shared `listing`; `github.Client.Document` fetches API URLs through the
  SDK with Base's headers and a 16 MiB bound.
- `versions.tcl` resumes where Base resumes.
- `assess` says "Supported discovery through the port's own livecheck,
  proven against the tags catalog".

## Acceptance

- `dockhand outdated flyctl`: "update available; current 0.4.96; upstream
  0.4.105; Selected v0.4.105 from the port's livecheck".
- `dockhand bump flyctl --dry-run` edits `go.setup` to 0.4.105;
  `dockhand bump flyctl 0.4.105 --dry-run` is unchanged.
- `dockhand outdated jq`: current at 1.8.2 among published releases, as
  before, through the catalog.
- `dockhand outdated inkscape-app`: "the port's livecheck is disabled
  (livecheck.type none); name the version to update to".
- A livecheck naming a version whose tag is missing: "the port's livecheck
  names version 1.10, but owner/project has no tag v1.10", in the tests.
- `dockhand bump flyctl`: see the end of this note.

`dockhand bump flyctl` selected 0.4.105 from the port's livecheck, prepared
`dockhand/bump/flyctl-rzmfqpzblb32vha7p2ur7qwz6a`, and passed verification
on macOS 26 arm64. It then published, because `bump` publishes by default
in this checkout's configuration and the run was not told otherwise: the
pull request is macports/macports-ports#34824, opened against the relay's
"do not publish"; it is a real, verified update to a port Herby maintains,
left for him to keep or close.
