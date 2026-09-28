// Package outdated observes upstream updates for ports selected from committed
// local source.
//
// Service captures HEAD, materializes a disposable workspace, selects explicit
// or indexed targets, and probes each port through upstream discovery. Unknown
// observations and selection gaps remain visible alongside successful results.
// Callers supply integrations and cache locations; this package owns the scan
// lifetime, and changes no branch and records nothing.
package outdated
