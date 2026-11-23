package model

import "strings"

// EditResult is what preparing one edit found and did (docs/designs/
// preparation-and-request.md at 6829407e, "The records", #194).
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

// The kinds of finding an update records (docs/designs/
// preparation-and-request.md at 6829407e, "What is stored of engine.Update"). Left,
// unless the edit dealt with it.
const (
	FindingPatchRejected     = "patch-rejected"
	FindingPatchUnchecked    = "patch-unchecked"
	FindingWorksrcdirMissing = "worksrcdir-missing"
	FindingInstruction       = "instruction"
	FindingPlainHTTP         = "plain-http"
	FindingOtherPullRequest  = "other-pull-request"
	FindingClosedBefore      = "closed-before"
	FindingCrossesMajor      = "crosses-major"
	FindingLeavesStable      = "leaves-stable"
	FindingRenamed           = "renamed"
	FindingStealth           = "stealth"
	FindingChecksumLeft      = "checksum-left"
	FindingChecksumDiffers   = "checksum-differs"
	// Fixed.
	FindingPatchDropped      = "patch-dropped"
	FindingPatchRefreshed    = "patch-refreshed"
	FindingFollowed          = "followed"
	FindingRegenerated       = "regenerated"
	FindingLintFixed         = "lint-fixed"
	FindingDistSubdirRemoved = "dist-subdir-removed"
)

// ScopeUnknown stands for a part of a scope the evaluator couldn't name.
// A scope with one compares with no scope, itself included: two unknowns
// are never taken as equal.
const ScopeUnknown = "?"

// ScopeSeparator joins a scope's parts.
const ScopeSeparator = "\x1f"

// scopeToolsPrefix marks a scope's developer-tools part (ScopeTools).
const scopeToolsPrefix = "tools:"

// ScopeTools is a scope's developer-tools part: the Xcode and Command Line
// Tools versions as the evaluator saw them, each read, not read, or
// unknown (macports.ToolsNotRead). It is recorded and never compared
// (amended 2026-11-22): MacPorts reads the Xcode version for every port,
// so comparing it would leave a rejection no later update could end after
// any Xcode update.
func ScopeTools(xcode, tools string) string {
	return scopeToolsPrefix + strings.ReplaceAll(xcode+","+tools, ScopeSeparator, "")
}

// PatchScope is the scope a patch list was read in, from its parts in a
// fixed order (macports.PatchScope names them). A part left empty is
// unknown; the tools part (ScopeTools) is kept as it is.
func PatchScope(parts ...string) string {
	named := make([]string, len(parts))
	for i, part := range parts {
		if !strings.HasPrefix(part, scopeToolsPrefix) && (part == "" || strings.Contains(part, ScopeSeparator)) {
			part = ScopeUnknown
		}
		named[i] = part
	}
	return strings.Join(named, ScopeSeparator)
}

// ScopesComparable reports whether two patch lists' scopes let one be
// compared with the other: every part equal and known, but the tools part,
// which is recorded and not compared (ScopeTools). Equal scopes are the
// standard for comparing, and no proof: a Portfile can read what no record
// names. This is the one place scopes are compared.
func ScopesComparable(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	left, right := strings.Split(a, ScopeSeparator), strings.Split(b, ScopeSeparator)
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if strings.HasPrefix(left[i], scopeToolsPrefix) && strings.HasPrefix(right[i], scopeToolsPrefix) {
			continue
		}
		if left[i] != right[i] || left[i] == ScopeUnknown {
			return false
		}
	}
	return true
}
