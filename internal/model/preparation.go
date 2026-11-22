package model

import "strings"

// EditResult is what preparing one edit found and did (docs/designs/
// preparation-and-request.md, "The records", #194).
type EditResult struct {
	// Before and After are the port's version and revision on each side.
	Before, After string
	// Patches are the port's patches as the prepared Portfile declares
	// them, after anything the edit dropped or refreshed; nil where that
	// final declaration wasn't read, which is not a list with nothing in
	// it.
	Patches  *PatchCheck `json:",omitempty"`
	Findings []Finding   `json:",omitempty"`
}

// PatchCheck is the port's declared patches, with what the check found,
// read in one evaluation.
type PatchCheck struct {
	// Scope is the evaluation the list was read in (PatchScope); two
	// lists are compared only where their scopes are equal and known
	// (ScopesComparable).
	Scope string
	// Declared is every patch declared there; empty where none is.
	Declared []PatchResult
}

// PatchResult is one declared patch and what the check found of it.
type PatchResult struct {
	Name  string
	State PatchState
}

// PatchState is a declared patch's state as an update's check left it.
type PatchState string

const (
	PatchApplied   PatchState = "applied"
	PatchRejected  PatchState = "rejected"
	PatchUnchecked PatchState = "unchecked"
)

// Finding is one thing preparing an edit found: data, worded when it is
// shown, so a kind a reader doesn't know is shown by its name and detail.
type Finding struct {
	// Kind is a stable name: patch-rejected, plain-http, …
	Kind string
	// Subject is what it is about: the patch, the URL, the repository or
	// the pull request.
	Subject string `json:",omitempty"`
	// Fixed is the edit having dealt with it; otherwise it is left.
	Fixed bool `json:",omitempty"`
	// Detail are the tool's own lines, kept as captured: the hunks that
	// failed, a comment quoted.
	Detail []string `json:",omitempty"`
}

// ScopeUnknown stands for a part of a scope the evaluator couldn't name.
// A scope with one compares with no scope, itself included: two unknowns
// are never taken as equal.
const ScopeUnknown = "?"

// ScopeSeparator joins a scope's parts.
const ScopeSeparator = "\x1f"

// PatchScope is the scope a patch list was read in, from its parts in a
// fixed order (macports.PatchScope names them). A part left empty is
// unknown.
func PatchScope(parts ...string) string {
	named := make([]string, len(parts))
	for i, part := range parts {
		if part == "" || strings.Contains(part, ScopeSeparator) {
			part = ScopeUnknown
		}
		named[i] = part
	}
	return strings.Join(named, ScopeSeparator)
}

// ScopesComparable reports whether two patch lists' scopes let one be
// compared with the other: equal, and with no part unknown. Equal scopes
// are the standard for comparing, and no proof: a Portfile can read what
// no record names.
func ScopesComparable(a, b string) bool {
	if a == "" || a != b {
		return false
	}
	for _, part := range strings.Split(a, ScopeSeparator) {
		if part == ScopeUnknown {
			return false
		}
	}
	return true
}
