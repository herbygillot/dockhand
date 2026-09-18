# Vendored dependencies

The module's dependencies now live in `vendor`, written by `go mod vendor` after a `go mod tidy` that changed nothing. A build, test, or vet needs no module download and no network; Go uses the directory automatically because `go.mod` names a Go version past 1.14. The directory is 150 MB, almost all of it the pure-Go SQLite driver's `modernc.org` packages, which carry a C library translated per platform.

Changing a dependency now means editing `go.mod`, running `go mod tidy && go mod vendor`, and committing both; a `vendor/modules.txt` out of step with `go.mod` fails the build, which is the check that the tree is self-contained. The `deadcode` make target, which runs a tool by version through `go run`, is unaffected. The development notes say so.
