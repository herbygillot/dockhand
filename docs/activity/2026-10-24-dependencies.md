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
