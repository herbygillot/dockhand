// Package github verifies committed contributions through GitHub Actions on a
// personal MacPorts fork.
//
// Provider validates the supported workflow and source, conditionally pushes the
// branch, and tracks matching runs and matrix results through durable execution
// records. Cancellation ends a request's tracking without canceling a shared
// Actions run. The package also owns resumable local log caching and its retention;
// the shared internal/github client supplies authentication and SDK transport.
package github
