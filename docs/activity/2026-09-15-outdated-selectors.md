# Maintainer and category discovery

Added repeatable `outdated --maintainer` and `--category` selectors. Alternatives within a field are combined, and different fields intersect. Explicit port arguments remain supported separately. Maintainers accept GitHub handles, Repology handles, emails, and MacPorts' domain:user notation. Exact metadata matching belongs to `macports/portindex`; `app` stages frozen source/index inputs and orchestrates observation; `cli` handles flags and output. No new package or database schema was needed.

The index is generated from committed local HEAD, cached in the OS user-cache directory by tree/platform/tool identity, and never taken from the working checkout. This uses the existing source-index machinery, including isolated MacPorts configuration. Discovery still creates no jobs, branches, publications, or SQLite database and downloads no source archives.

The selection reports missing Portfiles, unindexed declared subports, and unread metadata as unknown membership. Selected subports retain the existing primary-port-only probing limitation explicitly rather than being mislabeled with their parent's upstream result. Unknowns produce a nonzero exit after displaying the complete set of observations. Empty complete selections succeed.

Validation includes exact maintainer normalization, repeated/category intersection selection, cancellation, malformed metadata, omitted directories/subports, and a native MacPorts indexing/CLI exercise with committed fixtures and a mocked GitHub catalog. That exercise verifies dirty-checkout exclusion, index cache reuse, a passing primary-port observation alongside unknown subport/coverage results, no downloads, unchanged refs, and no state directory.

Final checks passed: `go test ./...`; `go test -race` across app, CLI, MacPorts index selection, workflow, verification providers/staging, and SQLite; `go vet ./...`; and `gmake build`. Built-command help exposes both the dependent image and metadata selector flags. The workflow race suite completed in 216 seconds; no race reports occurred.
