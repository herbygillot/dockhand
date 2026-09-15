# GitHub credential recovery

Added `auth status` and `auth logout` outside the repository/state service graph. Status validates the selected account and identifies the credential source. Logout deletes only Dockhand's Keychain service/account entry and handles absence idempotently; it neither revokes a token nor touches the GitHub CLI login. Login reports environment overrides on stderr.

Credential resolution now carries a source alongside the in-memory secret. HTTP 401 responses from authenticated API operations become source-specific recovery errors before their response bodies are interpreted. No alternate credential is tried; a rejected PR write retains both authentication and definitive-rejection classification. Explicit and environment credentials, Keychain, and GitHub CLI retain their existing precedence. Secrets are excluded from token JSON and command results.

Authored the implementation and regressions in the existing credential, GitHub adapter, application, and CLI packages. Tests use a fake security executable and local HTTP servers: source precedence, repeated rejection without fallback, token-echo responses, write rejection, Keychain deletion/absence/failure, status JSON, and environment-override guidance. No real saved credential was altered. Focused package tests and the full test suite, vet, and build are checked before commit.
