# Continuous integration

GitHub Actions now checks Dockhand on a macOS runner for every push and pull request. The workflow reads the required Go toolchain from `go.mod`, uses `setup-go`'s module and build cache keyed by `go.sum`, and grants the job read-only repository access.

The job runs the project's existing `make test`, `make vet`, and `make build` targets. Tart-backed VM verification remains outside the default suite because it requires prepared host resources and is an integration workflow rather than a repository check.

The workflow has a twenty-minute timeout so an unexpected process cannot occupy a runner indefinitely. No new project dependency or lint tool was introduced.

All workflow and documentation in this change were authored for Dockhand v2.

## Validation

- `make test`
- `make vet`
- `make build`
- workflow syntax parsed as YAML
- `git diff --check`
