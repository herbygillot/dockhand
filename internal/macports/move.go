package macports

import (
	"errors"
	"fmt"
	"strconv"
)

// The two ways an Identity can fail to be comparable. Both are refusals
// and not defaults: a comparison that guessed at a missing half of an
// identity would answer a question nobody could check.
var (
	// ErrNoVersion is an identity with no version. There is nothing for
	// VerCmp to order and nothing for the epoch override's string
	// inequality to read, so the whole predicate is unanswerable.
	ErrNoVersion = errors.New("macports: an identity without a version cannot be compared")

	// ErrEpochNotAnInteger is an epoch that is not a bare integer —
	// including the empty string, which is refused rather than defaulted
	// to base's 0. Rule 7: an identity nobody filled in and an identity
	// whose port declares no epoch must not be the same value, because
	// only one of them is a fact.
	//
	// Bare integers are what the tree actually holds: 520 epoch lines
	// across 477 Portfiles and every value one of them.
	ErrEpochNotAnInteger = errors.New("macports: epoch is not a bare integer")
)

// Identity is the triple base orders installs by. sweep/exclude.go:192
// already calls epoch "half of a MacPorts version identity"; this names
// the whole of it.
//
// Strings, because strings are what every reader of one has: base's own
// values are Tcl strings, a Portfile's are source text, and Version is
// only ever ordered by VerCmp, which is defined over bytes. Epoch is the
// single field read as a number, and Move is where that reading — and
// its failure — happens.
type Identity struct {
	Epoch    string
	Version  string
	Revision string
}

// Movement is how a port's identity moved between two evaluations, read
// the way base reads it. A pure observation over two values; it decides
// nothing.
//
// Compared is rule 7 on change.Drift's precedent: the zero value is "not
// compared", never "did not move".
type Movement struct {
	From, To   Identity
	Compared   bool
	Moved      bool // To.Version != From.Version — the STRING guard that opens the epoch override at all
	Cmp        int  // VerCmp(To.Version, From.Version)
	EpochMoved bool
	Upgrades   bool // would base upgrade an install at From to a tree at To
}

// Move computes a Movement. The error is a non-integer epoch or an empty
// version, and it comes back with Compared = false.
//
// UPGRADES TRANSCRIBES ONE SITE AND NAMES THE OTHERS, because base has
// two predicates and a draft of this design transcribed the wrong one.
//
// The site that INSTALLS is macports::_plan_upgrade,
// src/macports1.0/macports.tcl:5245-5252 at v2.12.6 (~/Source/macports-base;
// the installed copy reads the same). Quoted so the transcription can be
// checked without leaving this file:
//
//	# check installed version against version in ports
//	if {([vercmp $version_installed $version_in_tree] > 0
//	        || ([vercmp $version_installed $version_in_tree] == 0
//	            && [vercmp $revision_installed $revision_in_tree] >= 0))
//	    && ![dict exists $options ports_upgrade_force]} {
//	    if {$portname ne $newname} {
//	        ui_debug "ignoring versions, installing replacement port"
//	    } elseif {$epoch_installed < $epoch_in_tree && $version_installed ne $version_in_tree} {
//	        set build_override 1
//	        ui_debug "epoch override ... upgrading!"
//
// The `if` is the SKIP — base takes that branch when it does not want to
// install — and the elseifs under it are the overrides that rescue one.
// Dropping ports_upgrade_force (a flag, not a fact of the versions) and
// the rename arm (a different port, not a moved one), and dropping the
// four later elseifs that read variants, platform, C++ stdlib and
// rev-upgrade rather than the identity, what is left is:
//
//	skip     := vercmp(installed, tree) > 0
//	         || (vercmp(installed, tree) == 0 && vercmp(rev_installed, rev_tree) >= 0)
//	override := epoch_installed < epoch_in_tree && version_installed ne version_in_tree
//	upgrades := !skip || override
//
// Read what that says. An epoch DECREASE is inert: no branch consults a
// lower tree epoch. The override only rescues a skip: if vercmp already
// says the tree is newer, epoch is never asked. And "ne" is a string
// compare, so an epoch bump at an unchanged version string does nothing —
// which is why a downgrade must move the string and the epoch together.
//
// The draft transcribed macports.tcl:5128-5133 instead. That loop selects
// the NEWEST INSTALLED entry when several are installed — the input to
// this predicate, not the predicate. And port(1)'s `port outdated`
// (src/port/port.tcl:627-637) subtracts epochs in both directions, so an
// epoch decrease reads as "outdated" there; that is a REPORT, and it is
// the site whose comment ("first checking epoch, then version, then
// revision") misdescribes its own code. Three sites, one that strands
// installs, and it is this one.
//
// Revision is compared by VerCmp and validated by nothing, because that
// is what base does with it — the quotation runs revisions through the
// same vercmp as versions. A caller that hands over an empty revision
// therefore gets VerCmp's answer for an empty string, which is "older
// than 0"; supply base's evaluated revision rather than a zero field.
func Move(from, to Identity) (Movement, error) {
	m := Movement{From: from, To: to}
	if from.Version == "" || to.Version == "" {
		return m, fmt.Errorf("comparing %q to %q: %w", from.Version, to.Version, ErrNoVersion)
	}
	fromEpoch, err := readEpoch(from.Epoch)
	if err != nil {
		return m, fmt.Errorf("from %q: %w", from.Version, err)
	}
	toEpoch, err := readEpoch(to.Epoch)
	if err != nil {
		return m, fmt.Errorf("to %q: %w", to.Version, err)
	}

	// vercmp(installed, tree), in the Tcl's own orientation rather than
	// as a negation of Cmp, so the two lines under it read straight off
	// the quotation above. From is what is installed, To is what the tree
	// now holds.
	installed := VerCmp(from.Version, to.Version)
	skip := installed > 0 || (installed == 0 && VerCmp(from.Revision, to.Revision) >= 0)
	override := fromEpoch < toEpoch && from.Version != to.Version

	m.Compared = true
	m.Moved = to.Version != from.Version
	m.Cmp = VerCmp(to.Version, from.Version)
	// Integer inequality, not string: "01" and "1" are one epoch, and the
	// parse has already happened for the override.
	m.EpochMoved = fromEpoch != toEpoch
	m.Upgrades = !skip || override
	return m, nil
}

// readEpoch reads an epoch field as the bare integer base compares with
// `<`. Tcl's `<` on two integer-looking strings is a numeric compare, so
// this is the faithful reading and not a convenience.
func readEpoch(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("epoch %q: %w", s, ErrEpochNotAnInteger)
	}
	return n, nil
}
