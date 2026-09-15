# Literal editors and bounded fetch

Moved revision and checksum source replacement into `macports/portfile`, together with their formatting/scope/ambiguity tests. Checksum editing consumes checksum values rather than download records. Evaluation and fidelity policy remain in `portedit`.

Added `fetch.Open` for successful bounded HTTP bodies, preserving caller contexts, clients, and redirect hooks while rejecting HTTPS downgrades. Callers retain limits, timeouts, content validation, hashing, and file ownership. Archive and PortIndex downloading use it; GitHub log caching keeps its existing SDK transport and resumable file lifecycle. No generic cache or file-management layer was added.

Validation: affected preparation/editor/index tests and race checks; new fetch tests cover declared and streaming size limits, exact-boundary and empty bodies, cancellation, redirects, HTTP failures, and reader errors.
