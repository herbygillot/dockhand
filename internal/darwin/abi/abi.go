// Package abi decides whether a change moved a port's binary interface,
// by comparing the shared libraries two builds installed.
//
// It lives under darwin because everything it reasons about is Mach-O:
// install names, compatibility and current versions, architecture
// slices, rpath-relative names, and the convention that strips numeric
// components off a versioned .dylib to recover the logical library. A
// package called `abi` at the top level would promise a generality this
// does not have — the day somebody asks whether it handles ELF, the
// name should answer before the code has to.
package abi

import (
	"sort"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/artifact"
)

// The ABI measurement: what moved between two installations of one
// port, stated in words a reader can check against the same two
// commands.
//
// Everything here compares values the environment produced and nothing
// else. What otool printed is parsed where the capture happens, in
// internal/macports/build, so a judgment about a break is made over
// rows rather than over text — and the tests below are transcribed from
// real captures rather than invented, because a fixture written to
// match a comparator agrees with it forever.
//
// Three properties do the work, and each of them is a false positive
// this package would otherwise produce every single run.
//
// The unit is the LOGICAL library and not the file. brotli installs
// libbrotlicommon.1.2.0.dylib, libbrotlicommon.1.dylib and
// libbrotlicommon.dylib, and all three announce the one install name
// /opt/local/lib/libbrotlicommon.1.dylib — so a comparison keyed by
// path reports three libraries where MacPorts installed one, and a
// symlink that moved reads as a library that vanished.
//
// A logical library is a SET of install names and not one. xorg-libXaw
// publishes libXaw.6.dylib and libXaw.7.dylib side by side, both from
// the one port; a map keyed by logical name with last-write-wins would
// report "libXaw.6.dylib → libXaw.7.dylib" on a rebuild that changed
// nothing, and propose revbumping every X11 dependent in the tree.
//
// What cannot be compared is said rather than guessed. A universal file
// whose slices disagree — a real lipo of a 2.0.0 x86_64 slice onto a
// 3.0.0 arm64 one has been built and captured — is two libraries under
// one path, and collapsing them to one arch invents a measurement. An
// @rpath install name carries a build hash in the one real case in this
// machine's own prefix, so comparing it reports removal-plus-addition
// on every build forever.

// Verdict is which of three things happened to the ABI check.
//
// The words are this package's own and they are no longer a note's
// finding kind. They used to be both — the constants read "abi-change",
// "abi-unchanged", "abi-unavailable" and were written straight onto a
// record.Finding.Kind — and the durable enum has since closed around
// the three kinds a note actually carries, of which a bare measurement
// is not one. What the record keeps is the cohort proposal the
// measurement fed (record.KindABIDependents), with this measurement's
// own sentence quoted into its criterion, so the words here answer a
// reader and never a codec.
type Verdict string

const (
	// Changed means the measurement was made and something a
	// dependent binds to moved.
	Changed Verdict = "changed"
	// Unchanged means the measurement was made and nothing moved. It
	// is a real result and not an absence: an up-front cohort refuted by
	// measurement rests on it, and the PR body has to state it.
	Unchanged Verdict = "unchanged"
	// Unavailable means the measurement could not be made. It is
	// never a stand-in for unchanged, and it always names which
	// unavailability this was.
	//
	// The direction of the danger is worth being exact about, because
	// the obvious reading is backwards. compare() asks only about the
	// libraries the BEFORE side published, so an absent before side
	// compares as NOTHING MOVED — a confident all-clear over a
	// measurement that never happened, which would silently retire the
	// cohort question for every port whose baseline could not be taken.
	// An absent after side is the other error, and it is the loud one:
	// every library the before side published reads as removed. Both are
	// refused here rather than concluded.
	Unavailable Verdict = "unavailable"
)

// ChangeKind is what moved. The distinction that matters is which of
// them is a load-time break, and it is not the one a reader expects:
// dyld requires the loaded library's compatibility version to be at
// least what the dependent recorded, so an INCREASE is not a break and
// a DECREASE is.
type ChangeKind string

const (
	// InstallNameMoved is the plain break: dependents recorded the old
	// name and dyld will look for it.
	InstallNameMoved ChangeKind = "install-name"
	// LibraryRemoved is an install name the new installation no longer
	// publishes at all.
	LibraryRemoved ChangeKind = "library-removed"
	// CompatNarrowed is a compatibility version that went backwards. A
	// dependent that recorded the higher one no longer loads.
	CompatNarrowed ChangeKind = "compat-narrowed"
	// CompatWidened is a compatibility version that went forwards. It is
	// reported and is not a break — every dependent that loaded before
	// still loads. Whether it should still earn a revbump proposal is a
	// maintainer's ruling and not a measurement.
	CompatWidened ChangeKind = "compat-widened"
	// Unmeasurable is a file this comparison declines to draw a
	// conclusion about, named so a human can. It is never a break: the
	// whole point of saying it is that nothing was measured.
	Unmeasurable ChangeKind = "unmeasurable"
)

// Change is one thing that moved, or one thing that could not be
// looked at.
//
// Note the names: Change, not ABIChange, because abi.ABIChange
// stutters. The same applies to every symbol here — the package name
// carries the subject, so the type says what it is.
type Change struct {
	// Library is the logical library — the install name with its
	// trailing version components stripped.
	Library string
	// Subject is the install name the clause is about, or the file path
	// for one that could not be attributed to an install name at all.
	Subject string
	Kind    ChangeKind
	// From and To are the two values compared: two install names for a
	// move, two compatibility versions for a compat change, the removed
	// name and nothing for a removal.
	//
	// An Unmeasurable clause is the one that carries neither: it puts
	// the reason nothing could be read into From, because the clause it
	// renders is "<subject> not measured: <reason>" and a second field
	// holding a sentence nobody compares would be a field that means
	// two things.
	From string
	To   string
	// Break says a dependent built against the before side can no longer
	// load. It is a narrower question than "worth a revision bump", and
	// deliberately so — the proposal weighs the breaks, and the rest is
	// reported for a human to weigh.
	Break bool
}

// String is the clause the criterion states, and it is the sentence a
// reviewer checks with otool by hand.
//
// A compat clause names its logical library rather than its install
// name: the criterion can carry several libraries, and a bare pair of
// version numbers in a list of them says nothing about which one moved.
func (c Change) String() string {
	switch c.Kind {
	case InstallNameMoved:
		return "install name " + c.From + " → " + c.To
	case LibraryRemoved:
		return c.From + " removed"
	case CompatNarrowed:
		return c.Library + " compatibility_version " + c.From + " → " + c.To + " (narrowed)"
	case CompatWidened:
		return c.Library + " compatibility_version " + c.From + " → " + c.To + " (widened)"
	case Unmeasurable:
		return c.Subject + " not measured: " + c.From
	}
	return c.Subject
}

// Source is where the baseline came from: the provenance half of the
// criterion, and the thing that makes "nothing moved" mean anything.
//
// The three words are the provider's own — a Manifests answer carries
// them — and they are spelled again here rather than imported, because
// the ruled import direction gives this package one edge and it goes to
// artifact. The values are what the provider writes and the record
// stores, so they must not drift: the test beside this file imports the
// provider vocabulary for the sole purpose of asserting these three
// constants equal it, which is the one place the two spellings are
// allowed to meet.
//
// It is a type and not a bare string so that a caller crossing the seam
// converts deliberately. Nothing here recovers a fact by reading words:
// each constant is compared as a value, and an unrecognized one is
// neither archive nor banked and refuses like every other unknown.
type Source string

const (
	// Archive is the honest before: the merge-base Portfile staged and
	// installed binary-only, so what was measured is the version the
	// change is leaving rather than whatever the environment's own
	// frozen tree happens to hold.
	Archive Source = "archive"
	// Banked is a measurement already taken for exactly this Portfile
	// blob on exactly this platform, kept rather than repeated. It is a
	// stronger claim than an archive, because it was measured here
	// rather than unpacked from a publication.
	Banked Source = "banked"
	// NoBaseline is no baseline, which is a refusal and not a zero.
	// Every use of it carries a Reason naming why, because the sentence
	// it produces says the check was unavailable and a reader must be
	// told which unavailability this was.
	NoBaseline Source = "none"
)

// Input is everything one ABI measurement turns on, read by the
// caller and handed over as values.
//
// The two manifests arrive as artifact values and not as the provider's
// own answer, and that is the whole of why this package can be read
// with no provider behind it: an installation is a fact about a build,
// the shapes for it left verify so the record and the analyses could
// name one, and what remains provider-shaped — where the baseline came
// from, why there is none — crosses as a Source and a sentence.
//
// Described is separate from an empty pair of manifests on purpose. A
// provider that cannot describe an installation and a build that failed
// before it installed anything both arrive as nothing, and they have
// different remedies — one is a different provider, the other is a
// fixed build — so the result has to be able to say which happened.
// Inferring it from an empty manifest would pick one and be wrong half
// the time.
type Input struct {
	// Port is the headline, used where the manifests carry no name of
	// their own — which is exactly the unavailable case, where they may
	// not exist at all.
	Port string
	// Portdir is where it lives, for the cohort exclusion that rests on
	// it: a dependent living in the headline's own directory is a port
	// this change already edits.
	Portdir string
	// Described says the environment was asked for a manifest at all:
	// the provider declares it can describe an installation and the
	// request asked for one.
	Described bool
	// FromSource says the installed side ignored any binary archive and
	// built. It rides the criterion because "measured against what was
	// published" and "measured against what this branch built" are
	// different claims about the after side.
	FromSource bool
	// Baseline is the installation the change is measured against, and
	// nil when there is none. The nil is real and is never a zero
	// standing in for a measurement: which absence it was is what Source
	// and Reason say.
	Baseline *artifact.Manifest
	// Candidate is what this verification produced — the after side.
	Candidate *artifact.Manifest
	// Source says where the baseline came from. It is never empty when a
	// provider was asked for a manifest at all: "we did not look" and
	// "we looked and there was nothing" are different answers, and only
	// the named ones are answers.
	Source Source
	// Reason says why there is no baseline, in the environment's own
	// words, and is empty when there is one. It is quoted verbatim,
	// because "none" alone is the shape of a guess: a port that did not
	// exist at the merge base, an archive that was never published and a
	// capture that was cut off are three different facts with three
	// different remedies.
	Reason string
}

// Result is what the measurement concluded.
type Result struct {
	// Port is the headline the measurement is about. It travels on the
	// result because the proposal that reads this result states it —
	// dependents.Propose takes nothing but this value, the comments, the
	// rows and a cap, so a headline the caller kept to itself would be a
	// headline the cohort could not name.
	Port string
	// Portdir is the headline's own directory, carried through from the
	// input for the same reason and one more: the cohort needs it and
	// may not go and look, because a dependent that lives in it — a
	// subport of the changed portdir — is a port this change already
	// edits.
	Portdir string
	Verdict Verdict
	// Changes are what moved and what could not be looked at, sorted by
	// library so two runs over one pair of manifests read the same.
	Changes []Change
	// Why is the measurement in words: what moved, between which two
	// versions, from which kind of before, on which platform. It is
	// quoted verbatim into a commit body and a pull request — it becomes
	// the cohort's criterion unchanged — so it is produced here with the
	// judgment rather than reworded downstream.
	Why string
}

// Broken reports whether anything a dependent binds to moved.
func (a Result) Broken() bool { return a.Verdict == Changed }

// Unmeasured names the files the comparison declined to draw a
// conclusion about. A caller that wants to know whether "unchanged"
// covered everything asks this: it did not, wherever this is non-empty.
func (a Result) Unmeasured() []Change {
	var out []Change
	for _, c := range a.Changes {
		if c.Kind == Unmeasurable {
			out = append(out, c)
		}
	}
	return out
}

// Broke lists the install names this measurement says a dependent can
// no longer rely on: the ones a link proof is worth taking against.
//
// It is the whole installation narrowed to the part that moved, and the
// narrowing is the judgment. A headline publishes several libraries —
// brotli, openssl and xorg-libXaw all do, so this is the ordinary case
// and not a corner — and a proof taken against every name it publishes
// answers "did this dependent link ANYTHING of yours", which is true of
// nearly every dependent and evidence for nothing. Under a heading that
// says these ports were revbumped because a library moved, a line
// naming a library that did not move is a claim the measurement does
// not support.
//
// One name per kind of break, each of them the name a dependent's own
// binary would carry: the new name for a rename (the dependent has been
// rebuilt against it, so that is what its otool now records), the
// subject for a narrowed compatibility version, and the old name for a
// removal — a dependent still recording a name this installation no
// longer publishes is proven broken, and that recording is the proof.
func (a Result) Broke() []string {
	seen := map[string]bool{}
	var out []string
	add := func(name string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, name)
	}
	for _, c := range a.Changes {
		if !c.Break {
			continue
		}
		switch c.Kind {
		case InstallNameMoved:
			add(c.To)
		case LibraryRemoved:
			add(c.From)
		case CompatNarrowed, CompatWidened, Unmeasurable:
			// CompatNarrowed is the only one of these that can carry a
			// break, and its Subject is the install name a dependent
			// recorded. The other two are listed so a kind added later
			// fails the build here rather than falling into a default that
			// guesses which field names a library.
			add(c.Subject)
		}
	}
	sort.Strings(out)
	return out
}

// Limits is what this measurement cannot see, in the words that say
// so wherever it is quoted.
//
// The constant and the Result method beside it share a name and that is
// deliberate: the words are one sentence, and the method is only the
// question of whether THIS reading is entitled to say it. Losing the
// ABI prefix is what put them in one namespace, and a second spelling
// for one sentence is exactly the drift the constant exists to stop.
//
// It is a constant rather than a conclusion because it is true of every
// reading. otool answers about install names and version fields, so a
// library can keep both while dropping a symbol, and a break that lives
// entirely in a header or in a plugin's own contract leaves no trace in
// either. The mechanical criterion is necessary and never sufficient,
// which is why nothing is ever included on its authority alone — and
// why the sentence belongs beside the criterion in a commit body and a
// pull request rather than in this repo's documentation, where the
// person weighing a proposal would not be reading it.
const Limits = "this criterion is necessary and not sufficient: an install name and a compatibility " +
	"version can sit still while symbols are removed, and a break confined to a header or to a " +
	"plugin's own contract is invisible to otool"

// Limits is the caveat this measurement travels with, and it is empty
// where there was no measurement.
//
// A check that could not run has nothing to be insufficient about: its
// criterion already says nothing was measured, and adding that the
// measurement cannot see everything would dress a refusal up as a
// finding with a reservation.
func (a Result) Limits() string {
	if a.Verdict == Unavailable {
		return ""
	}
	return Limits
}

// Delta measures what moved between the installation a change is
// leaving and the one it produced.
//
// It never returns "nothing moved" for a comparison it could not make.
// The three ways it cannot are told apart by name, because their
// remedies differ: an environment that cannot describe an installation
// needs a different provider, a build that installed nothing needs
// fixing, and a missing baseline can be earned.
func Delta(in Input) Result {
	a := Result{Port: portOf(in), Portdir: in.Portdir}
	if why, ok := unavailable(in, a.Port); ok {
		a.Verdict, a.Why = Unavailable, why
		return a
	}

	before, beforeBad := publishedBy(in.Baseline)
	after, afterBad := publishedBy(in.Candidate)
	a.Changes = append(compare(before, after), append(beforeBad, afterBad...)...)
	sortChanges(a.Changes)

	if len(before) == 0 && len(beforeBad) > 0 {
		// Every library the before side had was declined as unmeasurable —
		// a universal file whose slices disagree, an @rpath name — so
		// compare() had nothing to compare and found nothing, which is
		// byte for byte what it finds when nothing moved. Unmeasurable
		// clauses never carry Break by design, so the verdict loop below
		// cannot tell the two apart, and publishing "nothing moved" for a
		// reading in which nothing was read is the substitution the
		// missing-baseline case refuses, pointing the other way.
		a.Verdict = Unavailable
		a.Why = "ABI check unavailable: nothing " + a.Port +
			" publishes could be compared — " + unmeasuredPhrase(a.Changes) + platformPhrase(in)
		return a
	}
	if why, ok := lopsided(in, a.Port, before, after); ok {
		a.Verdict, a.Why = Unavailable, why
		return a
	}

	a.Verdict = Unchanged
	for _, c := range a.Changes {
		if c.Break {
			a.Verdict = Changed
			break
		}
	}
	a.Why = criterion(in, a)
	return a
}

// lopsided refuses a comparison where one side describes an
// installation and the other describes no library at all.
//
// It is not the same refusal as a missing manifest, and that is why it
// is here rather than folded into unavailable(): both sides parsed,
// both sides named files, and one of them published no install name.
// The tart guard upstream only rejects a capture with no version AND no
// files, so a sweep whose otool sections came back empty — otool
// unusable in the guest, registry paths that are not on disk, a batch
// cut short, all of it with stderr sent to /dev/null — arrives here
// looking like a complete answer. Concluding from it is a confident
// verdict in either direction: an empty before reads as nothing moved,
// an empty after as every library removed.
//
// Two installations that both publish no library are not lopsided. A
// port with no dylibs at all is a real port, and "nothing moved" is the
// true answer for it.
func lopsided(in Input, port string, before, after published) (string, bool) {
	switch {
	case len(before) == 0 && len(after) > 0:
		return "ABI check unavailable: the baseline for " + port + " describes " +
			fileCount(in.Baseline) + " and no library, so there was nothing to compare against" +
			platformPhrase(in), true
	case len(after) == 0 && len(before) > 0:
		return "ABI check unavailable: the installation describes " +
			fileCount(in.Candidate) + " and no library, so " + port +
			" could not be compared — every library the baseline publishes would read as removed" +
			platformPhrase(in), true
	}
	return "", false
}

// fileCount says how much of an installation was described, so a
// refusal about an empty library list is not confused with a refusal
// about an empty manifest.
func fileCount(m *artifact.Manifest) string {
	n := 0
	if m != nil {
		n = len(m.Files)
	}
	if n == 1 {
		return "1 file"
	}
	return strconv.Itoa(n) + " files"
}

// unmeasuredPhrase names what was declined, in the clauses' own words,
// so a refusal says which files it could not read rather than that some
// existed.
func unmeasuredPhrase(all []Change) string {
	var parts []string
	for _, c := range all {
		if c.Kind == Unmeasurable {
			parts = append(parts, c.String())
		}
	}
	if len(parts) == 0 {
		return "the baseline publishes no install name"
	}
	sort.Strings(parts)
	return strings.Join(parts, "; ")
}

// portOf is the port this measurement is about, preferring what the
// caller asked about over what the environment reported.
//
// Either manifest will do as a fallback, because a port that named
// itself is a better answer than none: the port rides the criterion and
// the cohort's own decline sentence, and a missing one makes both read
// as though the tool had lost track of what it was doing. The caller
// passes it in practice; this keeps the sentence whole for a refusal
// that reached here before anybody had filled the field in.
func portOf(in Input) string {
	switch {
	case in.Port != "":
		return in.Port
	case in.Candidate != nil && in.Candidate.Port != "":
		return in.Candidate.Port
	case in.Baseline != nil:
		return in.Baseline.Port
	}
	return ""
}

// unavailable says whether the comparison can be made at all, and in
// which of the three ways it cannot.
//
// The order is the order a reader would ask in, from the widest refusal
// to the narrowest, so the sentence names the first thing that was
// missing rather than a consequence of it.
func unavailable(in Input, port string) (string, bool) {
	if port == "" {
		port = "the port"
	}
	switch {
	case !in.Described:
		return "ABI check unavailable: this environment cannot describe an installation, so nothing was measured — a provider that implements a manifest is what would answer" +
			platformPhrase(in), true
	case in.Candidate == nil:
		return "ABI check unavailable: the build produced no installation to measure" +
			platformPhrase(in), true
	case in.Source == Banked && in.Baseline == nil:
		// A different bug from a missing archive, and it must not read as
		// one. "banked" says a measurement for this Portfile blob on this
		// platform was already taken and kept; a nil beside it says the
		// caller that claimed it never handed the value over. The remedy
		// is in this repository and not on a mirror, so the sentence says
		// so rather than sending a reader to fetch something.
		return "ABI check unavailable: the baseline for " + port +
			" was recorded as banked and no banked manifest was supplied, so there is nothing to compare against" +
			platformPhrase(in), true
	case in.Source == NoBaseline || in.Baseline == nil:
		return "ABI check unavailable: no baseline for " + port + baselineWhy(in) + noBank +
			platformPhrase(in), true
	}
	return "", false
}

// noBank is the tail a missing baseline carries: what would have to
// exist for the comparison to be possible, and the fact that nothing
// produces it yet.
//
// It replaced a remedy — "`dockhand verify <portdir>` on the primary
// branch banks one" — that named a command which changes nothing. There
// is no bank: no ledger keyed by Portfile blob and platform, no reader
// for one, and no writer, so a person who ran that command would meet
// the identical refusal afterwards and have no way to tell that they
// had been sent in a circle. A refusal that names a step which does not
// work is worse than one that names none, and this whole step's
// character is that a decline says what it actually knows.
const noBank = " — the comparison needs a measurement of the version being left, and nothing banks one yet"

// baselineWhy quotes the environment's own account of the missing
// baseline. "none" on its own is the shape of a guess — a port that did
// not exist at the merge base, an archive that was never published and
// a capture that was cut off are three different facts — so a provider
// that said why is repeated verbatim.
func baselineWhy(in Input) string {
	if in.Reason == "" {
		return ""
	}
	return ": " + in.Reason
}

// published is what one installation offers a dependent: per logical
// library, the compatibility version each install name announces.
type published map[string]map[string]string

// publishedBy reduces one manifest's rows to what can be compared, and
// names what cannot.
//
// The reduction is per PATH first, because a path is what otool was
// asked about and a universal file is several libraries under one name
// in the filesystem. A path whose slices disagree about their install
// name or their compatibility version is not one library with one
// answer, and no arch's answer is more true than the other's.
func publishedBy(m *artifact.Manifest) (published, []Change) {
	if m == nil {
		return published{}, nil
	}
	byPath := map[string][]artifact.Dylib{}
	var order []string
	for _, d := range m.Dylibs {
		// An empty install name is not a library. The capture leaves
		// executables out for this reason already; a row that arrives
		// with one anyway describes nothing to compare.
		if d.InstallName == "" {
			continue
		}
		if _, seen := byPath[d.Path]; !seen {
			order = append(order, d.Path)
		}
		byPath[d.Path] = append(byPath[d.Path], d)
	}

	out, bad := published{}, []Change(nil)
	conflicts := map[nameIn]string{}
	for _, path := range order {
		rows := byPath[path]
		name, compat := rows[0].InstallName, rows[0].CompatVersion
		split := false
		for _, d := range rows[1:] {
			if d.InstallName != name || d.CompatVersion != compat {
				split = true
			}
		}
		switch {
		case split:
			bad = append(bad, Change{
				Library: LogicalLibrary(name), Subject: path, Kind: Unmeasurable,
				From: "its architectures announce " + archNames(rows),
			})
			continue
		case strings.HasPrefix(name, "@"):
			// @rpath, @loader_path and @executable_path are resolved by
			// the dependent and not by this name. The one such library in
			// this machine's own prefix carries a build hash in its file
			// name, so a comparison would report removal-plus-addition on
			// every build of it forever.
			bad = append(bad, Change{
				Library: LogicalLibrary(name), Subject: path, Kind: Unmeasurable,
				From: "its install name " + name + " is resolved by the dependent",
			})
			continue
		}
		lib := LogicalLibrary(name)
		if out[lib] == nil {
			out[lib] = map[string]string{}
		}
		if had, seen := out[lib][name]; seen && had != compat {
			// Two files in one installation announcing the same install
			// name with different compatibility versions. brotli's three
			// libbrotlicommon files agree, and they must: taking whichever
			// the capture happened to list last would make the comparison
			// depend on a file ordering, and half the readings of that
			// would be a break that is not there.
			conflicts[nameIn{lib, name}] = had + " and " + compat
			continue
		}
		out[lib][name] = compat
	}
	for at, versions := range conflicts {
		delete(out[at.lib], at.name)
		bad = append(bad, Change{Library: at.lib, Subject: at.name, Kind: Unmeasurable,
			From: "this installation announces it with two compatibility versions, " + versions})
	}
	return out, bad
}

// nameIn is one install name under one logical library — the key a
// disagreement inside a single installation is recorded against.
type nameIn struct{ lib, name string }

// archNames states a universal file's disagreement in its own terms,
// so the finding says what was seen rather than that something was.
func archNames(rows []artifact.Dylib) string {
	var parts []string
	for _, d := range rows {
		arch := d.Arch
		if arch == "" {
			arch = "an unnamed architecture"
		}
		parts = append(parts, arch+" "+d.InstallName+" ("+d.CompatVersion+")")
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

// compare weighs the two sides, per logical library.
//
// Only libraries the BEFORE side published are asked about. A logical
// library the new installation added breaks nothing: no dependent can
// have recorded a name that did not exist.
func compare(before, after published) []Change {
	var out []Change
	libs := make([]string, 0, len(before))
	for lib := range before {
		libs = append(libs, lib)
	}
	sort.Strings(libs)

	for _, lib := range libs {
		was, is := before[lib], after[lib]
		gone, added := missing(was, is), missing(is, was)
		switch {
		case len(gone) == 1 && len(added) == 1:
			// One name out and one name in, under the one logical library:
			// the rename. It is the only pairing this package will make,
			// because it is the only one with a single reading — two out
			// and two in could be any matching, and a guessed one would
			// report a break between libraries that never related.
			from, to := gone[0], added[0]
			out = append(out, Change{Library: lib, Subject: to, Kind: InstallNameMoved,
				From: from, To: to, Break: true})
			out = append(out, compat(lib, to, was[from], is[to])...)
		default:
			for _, name := range gone {
				out = append(out, Change{Library: lib, Subject: name, Kind: LibraryRemoved,
					From: name, Break: true})
			}
		}
		// Every name both sides publish, whatever else happened to the
		// library around it. A set that merely gained a major — the two
		// libXaw majors xorg-libXaw ships side by side — reaches here
		// with nothing gone and nothing to say.
		for _, name := range shared(was, is) {
			out = append(out, compat(lib, name, was[name], is[name])...)
		}
	}
	return out
}

// compat weighs one library's compatibility version between two builds.
//
// A version this package cannot read as numbers is reported as
// unmeasurable rather than compared as text: "10.0.0" sorts before
// "2.0.0" as a string, and real libraries reach two digits — this
// machine's own libImath announces 30.0.0 — so a text comparison would
// call an ordinary upgrade a break in exactly the cases that are not.
func compat(lib, name, was, is string) []Change {
	if was == is {
		return nil
	}
	order, ok := compatOrder(was, is)
	if !ok {
		return []Change{{Library: lib, Subject: name, Kind: Unmeasurable,
			From: "its compatibility_version moved " + was + " → " + is + ", which is not a version this check can order"}}
	}
	kind, broke := CompatWidened, false
	if order > 0 {
		// Backwards. dyld requires the loaded library's compatibility
		// version to be at least what the dependent recorded, so this is
		// the direction that stops a dependent loading.
		kind, broke = CompatNarrowed, true
	}
	return []Change{{Library: lib, Subject: name, Kind: kind, From: was, To: is, Break: broke}}
}

// compatOrder compares two Mach-O version strings numerically,
// answering positive when the second is lower than the first.
func compatOrder(was, is string) (int, bool) {
	a, ok := versionFields(was)
	if !ok {
		return 0, false
	}
	b, ok := versionFields(is)
	if !ok {
		return 0, false
	}
	for len(a) < len(b) {
		a = append(a, 0)
	}
	for len(b) < len(a) {
		b = append(b, 0)
	}
	for i := range a {
		switch {
		case a[i] > b[i]:
			return 1, true
		case a[i] < b[i]:
			return -1, true
		}
	}
	return 0, true
}

// versionFields reads a dotted version as numbers. An empty string is
// not a version: a library whose compatibility version was never
// captured has not been shown to have moved.
func versionFields(v string) ([]int, bool) {
	if strings.TrimSpace(v) == "" {
		return nil, false
	}
	var out []int
	for _, f := range strings.Split(v, ".") {
		n, err := strconv.Atoi(f)
		if err != nil || n < 0 {
			return nil, false
		}
		out = append(out, n)
	}
	return out, true
}

// missing lists the keys of one set that the other does not hold,
// sorted so a rename is reported the same way twice.
func missing(from, other map[string]string) []string {
	var out []string
	for name := range from {
		if _, ok := other[name]; !ok {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// shared lists the keys both sets hold.
func shared(a, b map[string]string) []string {
	var out []string
	for name := range a {
		if _, ok := b[name]; ok {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// sortChanges orders the clauses by library, then by the order the
// kinds are reasoned about, so a library's rename is stated before the
// compatibility version that came with it.
func sortChanges(all []Change) {
	rank := map[ChangeKind]int{
		InstallNameMoved: 0, LibraryRemoved: 1,
		CompatNarrowed: 2, CompatWidened: 3, Unmeasurable: 4,
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].Library != all[j].Library {
			return all[i].Library < all[j].Library
		}
		if rank[all[i].Kind] != rank[all[j].Kind] {
			return rank[all[i].Kind] < rank[all[j].Kind]
		}
		return all[i].Subject < all[j].Subject
	})
}

// LogicalLibrary reduces an install name to the library a dependent
// means when it needs one: the base name, without the trailing
// version components that a rebuild moves and a soname bump does not.
//
// Only dot-separated components that are entirely digits are stripped,
// and that is measured rather than chosen. Over the 317 distinct
// install names in this machine's own prefix the rule collides exactly
// once, on the two libXaw majors that one port genuinely publishes
// together. Stripping trailing digits instead would turn libxml2.16
// into libxml — a name that matches nothing on either side, so a real
// libxml2 break would be reported as two unrelated libraries. Stopping
// at a component that is not all digits is what keeps
// libMagickCore-6.Q16 whole, so a Q16 → Q17 move reads as the break it
// is, and what keeps libpcap.A from becoming libpcap.
//
// It reduces the install name and never the path. p11-kit-proxy.dylib
// announces itself as libp11-kit.0.dylib, so the file says one library
// and the linker says another, and only the linker's answer is what
// dependents recorded.
func LogicalLibrary(installName string) string {
	base := installName
	if i := strings.LastIndexByte(base, '/'); i >= 0 {
		base = base[i+1:]
	}
	base = strings.TrimSuffix(base, ".dylib")
	for {
		i := strings.LastIndexByte(base, '.')
		if i <= 0 || !allDigits(base[i+1:]) {
			break
		}
		base = base[:i]
	}
	return base
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// criterion is the measurement in one sentence: what moved, and between
// which two builds.
//
// The provenance half is not decoration. "Nothing moved" measured
// against a binary archive of the version being left and "nothing
// moved" measured against a banked capture are different claims, and a
// comparison against the branch's own build measured against itself
// always says nothing moved — which is the one failure mode of this
// check that produces no error and no empty field.
func criterion(in Input, a Result) string {
	var clauses []string
	for _, c := range a.Changes {
		clauses = append(clauses, c.String())
	}
	if len(clauses) == 0 {
		clauses = append(clauses, "no install name, compatibility version or library moved")
	}
	return strings.Join(clauses, "; ") + ", measured between " +
		a.Port + "@" + versionOf(in.Baseline) + " (" + sourcePhrase(in.Source) + ")" +
		" and @" + versionOf(in.Candidate) + " (" + afterPhrase(in.FromSource) + ")" +
		platformPhrase(in)
}

// versionOf is the version a manifest reports, which is the whole
// archive-naming string — version, revision and variants — because that
// and not the bare version identifies the build being described.
func versionOf(m *artifact.Manifest) string {
	if m == nil || m.Version == "" {
		return "an unrecorded version"
	}
	return m.Version
}

// sourcePhrase names where the before side came from, in words rather
// than in the wire constant, because the criterion is read by a person.
//
// An unnamed source is said and never guessed at, which is rule 7 at
// the sentence: the zero Source is a caller that did not fill the field
// in, and reading it as an archive would put a provenance a reviewer
// can check under a measurement nobody stated one for.
func sourcePhrase(source Source) string {
	switch source {
	case Archive:
		return "binary archive"
	case Banked:
		return "banked manifest"
	case NoBaseline:
		// Unreachable from Delta, which refuses before it words a
		// criterion, and named here so a Source added later fails the
		// exhaustiveness check rather than falling into the default.
		return "no baseline"
	}
	return "an unnamed source"
}

// afterPhrase names where the after side came from, and never says
// nothing.
//
// The before side always states its provenance, and the after side used
// to state its only when a request flag said so. That flag is
// Run.FromSource, which the CLI sets for a refresh and for --recheck —
// so a plain `dockhand bump`, the case this whole check was written
// for, produced a sentence whose second half was silent even though the
// guest genuinely compiled: the new version names an archive that does
// not exist. A reader met "measured between libwidget@2.4.1 (binary
// archive) and @3.0" and had to assume the rest.
//
// Nothing in the manifest records how the installation was produced, so
// the honest third answer is that it was not recorded. Saying it is the
// point: an assumed provenance and a stated absence are different
// things to a reviewer checking a claim, and only one of them can be
// noticed.
func afterPhrase(fromSource bool) string {
	if fromSource {
		return "built from source"
	}
	return "source not recorded"
}

// platformPhrase names the environment in its own word for itself,
// copied down rather than resolved, because an environment naming a
// release this repo's table cannot is still telling the truth.
func platformPhrase(in Input) string {
	plat := ""
	if in.Candidate != nil {
		plat = in.Candidate.Platform
	}
	if plat == "" && in.Baseline != nil {
		plat = in.Baseline.Platform
	}
	if plat == "" {
		return ""
	}
	return " on " + plat
}
