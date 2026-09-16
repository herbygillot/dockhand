// Package portedit prepares evaluated Portfile edits in disposable source
// workspaces.
//
// Service probes literal inputs to understand calculated versions, prepares
// version or revision bumps and checksum refreshes, and regenerates supported
// dependency blocks. Archive plans cover relevant metadata contexts, preserve
// independent release pins, and associate checksums through macports/distfiles. It checks the edited evaluation against the intended change
// and returns edits, commit intent, and fidelity diagnostics. Assessment shares
// the pre-download checks and reports untested stages explicitly.
//
// Callers supply an exclusively owned workspace and retain it for the lifetime
// of any VersionProbe, which must be used sequentially. Git object storage,
// branch integration, and workflow state belong to the caller.
package portedit
