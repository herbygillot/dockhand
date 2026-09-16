// Package preparation connects immutable Git sources to Portfile editing.
//
// Service materializes disposable snapshots, resolves and rechecks upstream
// releases, invokes macports/portedit, and stores the edited tree as Git objects.
// It re-evaluates that stored candidate before returning edits and commit intent.
// Workflow owns durable preparation checkpoints, commit and branch integration,
// and subsequent verification or publication.
package preparation
