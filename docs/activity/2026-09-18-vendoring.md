# Vendored dependencies

The module's dependencies now live in `vendor`, written by `go mod vendor` after a `go mod tidy` that changed nothing. A build, test, or vet needs no module download and no network; Go uses the directory automatically because `go.mod` names a Go version past 1.14. The directory is 150 MB, almost all of it the pure-Go SQLite driver's `modernc.org` packages, which carry a C library translated per platform.

Changing a dependency now means editing `go.mod`, running `go mod tidy && go mod vendor`, and committing both; a `vendor/modules.txt` out of step with `go.mod` fails the build, which is the check that the tree is self-contained. The `deadcode` make target, which runs a tool by version through `go run`, is unaffected. The development notes say so.

## The Makefile

The Makefile exports `GOFLAGS=-mod=vendor`, so its build, test, and vet targets fail loudly on a missing or stale vendor directory instead of quietly downloading modules. `make vendor` tidies and refreshes the directory; `make vendor-check` runs `go mod tidy -diff` and vendors into a temporary directory to compare against the committed one, modifying nothing, which is the check for a review or CI step. The `deadcode` target clears the flag, since `go run` of a tool by version fetches it as a module of its own, which the vendor mode forbids.

CI runs `make vendor-check` before the tests, so a pull request whose `go.mod` and vendor directory disagree fails on that step rather than on a confusing build error later.
