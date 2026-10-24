# 2026-10-24: Direct dependencies to their newest minor releases

The person asked whether dockhand's libraries have updates, and took the
recommendation: the minor updates now, on main for the release after
v0.5.0, and the two major-version moves to prioritization.

## Updated

- golang.org/x/crypto 0.55.0 to 0.58.0, mod 0.38.0 to 0.42.0, oauth2
  0.36.0 to 0.37.0, sync 0.23.0 to 0.24.0, and sys 0.47.0 to 0.49.0.
- modernc.org/sqlite 1.58.0 to 1.61.0. Its bundled SQLite goes from 3.53.4
  to 3.54.1, and modernc.org/libc from 1.75.6 to 1.79.0. A database file
  written by either opens in the other: SQLite's file format doesn't change
  between its minor releases.
- github.com/github/go-spdx/v2 2.7.0 to 2.8.0.
- Indirect, as `go get` moved them: dustin/go-humanize 1.0.1 to 1.1.0,
  ncruces/go-strftime 1.0.0 to 1.1.0.

`make vendor` re-vendored them.

## Not updated, and why

- **github.com/google/go-github:** v91 to v93 is two major versions, with
  API changes.
- **gitlab.com/gitlab-org/api/client-go:** v2.64.0 is the last of v2, and
  the module moved to v3 (v3.34.0).

Both go to prioritization. BurntSushi/toml, cobra, testify, licensecheck,
and jlaffaye/ftp are current.

The full gate passes, and the corpus, 14 of 14 in 3 min 10 s.

# The harness: a fixture port that's current

Prime-time on rc5: B4 failed because its Go fixture port, dblab, was
already current, after MacPorts updated it to 0.53.0 (983bca05f8f).
`update dblab --new` rightly made no branch, and the row went on to run
`check -b null`. D-C4, F5, F7, and D-I5 build on the same fixture update.

- **dh_setup:** an `update ... --new` that made no branch of the row's own
  (`own_branch`) now leaves the row "not run". It says the fixture port is
  current and to name one that's due, and returns, so no row goes on
  without a branch. Every row's setup update goes through it.
- **full.sh:** before any row runs, it asks the installed dockhand,
  where one is, whether ACCEPT_GO_PORT and ACCEPT_RUST_PORT are due
  (`outdated --json`). If either is current it stops, naming it. Picking
  the fixture from the ports that are due is left out: the full stage's
  rows need a small Go and Rust port of a known shape, which is a person's
  choice.
- The harness self-test and path-check pass.

# go-github v93 and GitLab's client-go v3

Prioritization measured both by building main against each, and placed
them in ef6b5eca, a fix for main riding v0.6.0.

- The import paths move, from `github.com/google/go-github/v91` to `/v93`
  in 12 files, and from `gitlab.com/gitlab-org/api/client-go/v2` to `/v3`
  in 5. go.mod and vendor/ follow, with golang.org/x/time going to v0.16.0
  alongside client-go. The only other change: client-go v3 marks its
  `Ptr` helper deprecated in favour of Go 1.26's `new(expr)`, which
  staticcheck flagged, so forge/gitlab's two calls use `new`.
- The full gate passes, and so does the corpus, 14 of 14 in 2 min 39 s.
- internal/macos's TestAGuestCopysDigestIsRead runs the guest's BSD
  `stat -f` on the host, so it now skips off darwin, where it failed on
  Linux on main too.
