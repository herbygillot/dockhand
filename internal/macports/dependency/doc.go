// Package dependency handles generated MacPorts Go and Rust dependency blocks.
//
// It inspects editable declarations, reads manifests from source archives, invokes
// the optional go2port or cargo2port helper, and validates and applies generated
// values. Callers supply the archive and tool choices and integrate the resulting
// edits with Portfile evaluation and checksum verification.
package dependency
