# Package documentation pass

Added or revised package-level documentation for all 44 Go packages, including
`cmd/dockhand` and the developer `tools/stateperf` command. Each package now has
one overview in `doc.go`; earlier package comments were moved out of implementation
files to avoid duplicate documentation.

The overviews describe current responsibilities, collaborators, and ownership
contracts. Particular attention went to workflow versus state, application
composition versus CLI/process lifetime, Portfile editing versus Git candidate
storage, forge adapters versus upstream policy, and Tart control versus
provisioning and verification. Preserved the state transaction restrictions and
workflow claim/call/record contract while removing stale implementation-status
wording. Publication documentation distinguishes PR planning/writes from the
workflow's guarded Git pushes and full-cohort authorization.

This pass covers package overviews, not documentation of every exported symbol.
No declarations, executable statements, tests, or dependencies changed. The
comments were authored against the current implementation; no v1 comments were
copied.

Validation:

- Go parser and documentation inspection found exactly one package comment in
  `doc.go` for each of the 44 packages, with the expected package/command synopsis.
- `go list` exposed all 44 documentation synopses.
- Go token comparison against HEAD confirmed that existing files changed only
  in comments; new Go files contain only package declarations and comments.
- `go test ./... -run '^$'` compiled every package and its tests successfully;
  behavioral tests and live provider exercises were not rerun for this comment-only pass.
- `gofmt` and `git diff --check` passed.
