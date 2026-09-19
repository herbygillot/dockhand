// Package fidelity compares MacPorts evaluations of a Portfile before and after
// an edit and reports whether only the intended metadata changed. It knows
// nothing about editing, downloads, or workspaces: callers supply the
// evaluated snapshots, the selected port, and what they expected to change.
package fidelity
