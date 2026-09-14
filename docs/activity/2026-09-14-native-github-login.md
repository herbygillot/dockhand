# Native GitHub login

## Scope

Implemented `dockhand auth login` as a repository-independent GitHub OAuth device flow. The command no longer requires users to install `gh` or manually create and copy an API token. GitHub application registration remains an external release prerequisite: the registered OAuth client ID must be embedded in a release build or supplied by environment or flag during development.

## Implementation

- Added `credential` contracts for device authorization and secret storage, plus a macOS Keychain implementation. The Keychain adapter writes through `security -i` over standard input with a base64 storage marker; the token never appears in the child process arguments.
- Added a GitHub device-flow adapter backed by `golang.org/x/oauth2`. The library owns polling intervals and device-flow error behavior. Dockhand requests `public_repo`, then uses the existing `go-github` client to validate the authenticated account before storage.
- Added repository-independent application wiring that stores one Dockhand credential for `github.com` and returns only the host, account, and storage description.
- Added `dockhand auth login`, `--client-id`, and `--no-browser`. `DOCKHAND_GITHUB_CLIENT_ID` supplies the development default; `make GITHUB_OAUTH_CLIENT_ID=<id>` embeds the registered client ID in a build. Interactive instructions use stderr, while the final result supports the global JSON mode.
- Added the saved Keychain credential between environment variables and the optional `gh` fallback. API credentials remain separate from Git push credentials and never enter SQLite or durable workflow records.

All code and tests were authored for v2. No v1 comments or tests were copied.

## Validation

Tests cover OAuth request scope, device presentation, polling, identity validation, account-safe results, Keychain missing/error distinctions, stdin-only secret writes, credential precedence, CLI help, state independence, interactive output, and JSON output. An isolated temporary macOS keychain confirmed the `security -i` storage form and was deleted immediately afterward.

`go test ./...`, `make test-race`, `make vet`, `go mod verify`, `CGO_ENABLED=0 make build`, and `git diff --check` passed. A build with `GITHUB_OAUTH_CLIENT_ID=fixture-client` also confirmed linker embedding and was replaced by the ordinary build afterward.
