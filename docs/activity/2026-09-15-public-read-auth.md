# Consistent GitHub public-read authentication

Preview and normal preparation now use the same application client constructor, including the saved Keychain credential source. Public SDK reads resolve available credentials before their first request instead of depending on a previous authenticated operation. A distinct no-credentials error allows anonymous public access without suppressing malformed credentials, Keychain access failures, cancellation, or rejected tokens. Explicit custom API endpoints remain isolated from implicit system credential discovery.

Validation: GitHub adapter and app suites passed. Regression coverage checks public reads with saved credentials, no credentials, inaccessible or malformed credentials, cancellation, and server rejection without anonymous fallback.
