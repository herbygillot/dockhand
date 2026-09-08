// Package xcode answers which Apple toolchain a Darwin release expects.
//
// It is here rather than in the tart provisioner because it is a fact
// about the operating system, not about a virtual machine: it switches
// on the Darwin kernel version and knows nothing about how a guest is
// booted. Until this move it was reachable only through
// verify/tart/provision, so a second provider could not ask the
// question at all — and the question is not tart's to answer, because
// "which Xcode can Sonoma run" is true of a laptop, a lab machine and
// somebody else's hypervisor alike.
//
// Two facts and no verbs. Which Xcode a release SHOULD get and which
// Xcode a release CANNOT run are one table read from two directions,
// and they are kept together so that raising a bound and forgetting the
// recommendation beside it is a change to one file rather than a drift
// between two packages. What to do about the answer — pick an archive,
// push it into a guest, select it — stays with the provisioner, which
// is where the machine is.
package xcode

import "github.com/herbygillot/dockhand/internal/platform"

// bounds is the first Xcode version each release cannot run, by Darwin
// major — Apple raises the macOS floor partway through each Xcode line,
// so the bound is a minor, not a major. An absent entry means no known
// bound (the newest release runs the newest Xcode).
//
//	Monterey: Xcode 14.3 requires Ventura
//	Ventura:  Xcode 15.3 requires Sonoma
//	Sonoma:   Xcode 16.3 requires Sequoia
//	Sequoia:  Xcode 26.4 requires Tahoe 26.2
var bounds = map[int]string{
	21: "14.3",
	22: "15.3",
	23: "16.3",
	24: "26.4",
}

// Bound is the first Xcode version a release cannot run, and "" where
// this table knows of none.
//
// The empty answer is "no known bound" and never "cannot run anything":
// the newest release runs the newest Xcode, which is what makes the
// zero safe here — a caller comparing against "" skips the comparison,
// and there is nothing it could wrongly exclude.
//
// It is exported so the one caller that picks an archive can hold a
// candidate to the release's ceiling without carrying its own copy of
// the table. A second table would be the drift this package exists to
// prevent: the recommendation below and this bound are the same fact
// read from two ends, and a version recommended above its own bound is
// a guest that cannot install what it was told to.
func Bound(r platform.Release) string { return bounds[r.Darwin] }

// Recommended names the Xcode a release should get: the newest
// release-form version below its bound, and whether that is a cap
// rather than simply the newest that exists.
//
// The specific version matters greatly per macOS release — Apple raises
// the floor mid-line — which is why this is a table a guided workflow
// can print, not a "download the latest" suggestion.
//
// capped is the fact and not a decoration. false means this release has
// no ceiling and the newest Xcode is the answer, which is a different
// statement from "we have no recommendation": a caller that read the
// empty version alone could not tell the two apart, and would tell a
// person on the newest macOS that dockhand does not know what they
// need.
func Recommended(r platform.Release) (version string, capped bool) {
	switch r.Darwin {
	case 21:
		return "14.2", true
	case 22:
		return "15.2", true
	case 23:
		return "16.2", true
	case 24:
		return "26.3", true
	}
	return "", false // the newest release runs the newest Xcode
}
