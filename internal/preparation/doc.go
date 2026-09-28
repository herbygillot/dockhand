// Package preparation connects immutable Git sources to Portfile editing.
//
// Service materializes disposable snapshots, resolves and rechecks upstream
// releases, invokes macports/portedit, and stores the edited tree as Git objects.
// It re-evaluates that stored candidate before returning edits and commit intent.
// Its Result is the editor's whole, with the edits as Git file edits; the
// engine writes them into a branch's working files, and tidy commits them.
package preparation
