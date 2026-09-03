package intent

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/record"
)

// The instruction-comment finding: a comment in the Portfile telling
// whoever updates this port to bump something else.
//
// It is the one finding a plan can make on its own. Everything else the
// cohort rests on is measured — an install name that moved, a
// compatibility version that went backwards — and measuring needs an
// environment that has built the port. A maintainer's own written
// instruction needs nothing but the file, and it is evidence of a kind
// no measurement produces: it can name a break otool cannot see, and it
// can be wrong, which is why what dockhand does with it is quote it.
//
// Nothing here proposes anything. The finding carries the comment
// VERBATIM and the ports it named, and a human weighs it — against the
// measurement, which may disagree with it, and against the tree, which
// may not carry the ports it names any more. A comment is never a
// roster the tool acts on: the unnamed form ("all dependents will need
// to be rev-bumped") names nobody on purpose, because reading it as
// "every dependent" would auto-include hundreds of ports off one
// sentence.
//
// The shapes below are transcribed from the real tree rather than
// invented, and the invented sentence is worth naming because it was
// the plan's own: "increase the revision of the following ports when
// updating" matches ZERO Portfiles. What is there is a family — three
// forms of it, plus two that look like members and are not.

// FindingInstruction is the kind an instruction-comment finding carries
// on the wire. It is a constant because two packages read it: this one
// writes it, and the settlement maps it back into the quotes a cohort
// decision weighs.
const FindingInstruction = "instruction-comment"

// bumpVerb is the family, not a sentence. It wants a bump verb with a
// revision as its object, in any of the spellings the tree uses:
// "increase the revision", "increase the revision number", "bump the
// revisions", "revbump", "rev-bump", "rev bump", "revbumping".
//
// Anchored on the verb rather than on a whole phrase because the
// condition wanders: dav1d wraps its sentence across two comment lines
// and protobuf3-cpp puts the condition on the line ABOVE the verb, so
// any pattern long enough to be a sentence matches neither.
var bumpVerb = regexp.MustCompile(
	`(?i)\b(?:rev[- ]?bump(?:ed|ing|s)?|(?:increase|increasing|bump(?:ing|s)?)\s+(?:the\s+)?revisions?(?:\s+numbers?)?)\b`)

// notBumping is the negation guard, and it is why the family alone is
// not enough. Every cue here is measured on a comment that matches
// bumpVerb and means the opposite:
//
//	openssl3, openssl11, openssl10: "is too obscure to justify
//	revbumping the dependents."
//	py-sip4: "-> SO: no rev-bumps are be needed."
//	perl5:   "Rather not revbump many p5 ports, so just fix it for
//	         new versions"
//
// Tested against the whole comment block rather than one clause of it,
// because a block is a handful of lines and the direction of a short
// paragraph is the direction of its sentences. A block that carries
// both a refusal and an instruction declines, which is the safe half of
// the trade: a missed instruction leaves the measurement to speak for
// itself, and a quoted refusal would hold publication for a comment
// that asked for nothing.
var notBumping = regexp.MustCompile(
	`(?i)(?:\bno\b|\bnot\b|\bnever\b|\bnothing\b|\bunnecessary\b|to justify)`)

// collective is the unnamed form: a comment that asks for the
// dependents as a class rather than naming any of them.
//
//	icu, icu-devel: "increase the revision number of the dependents
//	                 whenever the library version number changes."
//	cmark, cmark-gfm: "Any version update requires revbumping all
//	                 ports that link with the library"
//	abseil, spdlog: "Ports that depend on this port must be revbump"
//	geos:            "all dependents will need to be rev-bumped."
//
// It contributes a criterion and no candidates. That is the whole point
// of telling it apart from the named form: it is the shape a reader
// must be shown and the tool must not act on.
var collective = regexp.MustCompile(
	`(?i)(?:\bdependents?\b|\bports that depend\b|\bports that link\b|\breverse dependencies\b)`)

// bulletLine is a roster item under a header line, which is how the two
// longest instructions in the tree are written:
//
//	openssl3: "Please revbump these ports when updating the openssl3
//	          version/revision" then "  - freeradius (#43461)",
//	          "  - openssh (#54990)", "  - p5-net-ssleay (#67321, for
//	          minor version bumps)", "  - openssl (to rebuild the shim
//	          links)."
//	spdlog:   "Ports that depend on this port must be revbump after
//	          update:" then "- tiledb"
//
// The parenthetical after the name is the maintainer's caveat and is
// kept in the quote and out of the name.
var bulletLine = regexp.MustCompile(`^[-*•]\s+(\S+)`)

// listSkip are the words that may sit inside a roster without ending
// it. Every one is measured: "of" and "the" from "the revision of the
// dependents", "and" from every list in the family, "possibly" from
// sbcl's "math/maxima, math/fricas and possibly math/maxima-devel", and
// "or" and "also" beside them because a list that admits "and" and
// refuses its two synonyms would truncate on the first Portfile that
// used one.
var listSkip = map[string]bool{
	"of": true, "the": true, "and": true, "or": true, "also": true, "possibly": true,
}

// listStop are the words that end a roster. This set is the honest
// mechanism and also the whole of the guessing this rule does, so it is
// closed and each member is here for a reason:
//
//   - when, whenever, any, after, if, because, before, unless, while,
//     since: the condition clause every named form ends with — "whenever
//     dav1d's version is updated", "any time the db48 version changes",
//     "when this port changes", "after update".
//   - these, those, all, every, its, their, following, this, that, each:
//     a determiner where a name would go, which is the unnamed form or a
//     pointer at a list below.
//   - dependents, dependent, ports, port: the class rather than a
//     member.
//   - to, on, in, for, so, with, from, whose, must, will, need, needs,
//     should, please: prose that cannot begin a port name in any measured
//     example, listed so an unfamiliar sentence truncates the roster
//     rather than contributing an English word to it.
//
// A word this set does not know ends the roster as well, and that is
// the rule that makes the extraction refuse rather than guess: only a
// token justified by the vocabulary or by the index is taken as a port.
var listStop = map[string]bool{
	"when": true, "whenever": true, "any": true, "after": true, "if": true,
	"because": true, "before": true, "unless": true, "while": true, "since": true,
	"these": true, "those": true, "all": true, "every": true, "its": true,
	"their": true, "following": true, "this": true, "that": true, "each": true,
	"dependents": true, "dependent": true, "ports": true, "port": true,
	"to": true, "on": true, "in": true, "for": true, "so": true, "with": true,
	"from": true, "whose": true, "must": true, "will": true, "need": true,
	"needs": true, "should": true, "please": true,
}

// portName is the shape a MacPorts port name has, with the category
// prefix sbcl writes ("math/maxima") allowed in front of it. Matching
// the shape is never enough on its own — "whenever" fits it too — which
// is what listStop and the dependent roster are for.
var portName = regexp.MustCompile(`^(?:[A-Za-z0-9_.+-]+/)?([A-Za-z][A-Za-z0-9._+-]*)$`)

// MentionsRevbump reports whether a Portfile carries a comment of the
// revbump-instruction family at all.
//
// It is the cheap half of the rule, exported so a caller can decide
// whether the expensive half is worth paying for. The dependent roster
// instructionFindings takes is read from the tree's reverse index, and
// building that index is one sequential pass over the whole PortIndex —
// 25.6 MB and 41630 entries on a real tree. A few dozen Portfiles carry
// one of these comments, so a caller that filled the roster
// unconditionally would spend that pass on every plan of every port in
// order to narrow a roster that will never be consulted.
//
// It is the same two patterns the finding uses over the same blocks, so
// a comment this says no about is one instructionFindings would have
// skipped. False here means the roster changes nothing; it never means
// the roster is unavailable.
func MentionsRevbump(src []byte) bool {
	for _, b := range commentBlocks(src) {
		if bumpVerb.MatchString(b.prose) && !notBumping.MatchString(b.prose) {
			return true
		}
	}
	return false
}

// instructionFindings reads the Portfile for comments of the
// revbump-instruction family and states each one as a finding.
//
// One finding per comment BLOCK — a maximal run of adjacent comment
// lines — and the whole block is the quote. That is not tidiness: the
// condition a human has to weigh sits below the verb in dav1d ("...
// whenever" / "dav1d's version is updated.") and above it in
// protobuf3-cpp ("For a minor or major version number change, also" /
// "Revbump et, protobuf-c, mosh and py-onnx"), so a quote cut to the
// matching line drops the condition in one of the two shapes. A
// verbatim quote that is not verbatim is worse than no finding.
//
// port is the context being changed and deps the ports the index says
// depend on it. Both narrow the reading rather than widening it: a
// comment whose only named port is the port itself is the REVERSE
// direction — it says what triggers a bump of this port, not what to
// bump with it — and a token the index already calls a dependent is a
// port whatever the word list thinks of it.
func instructionFindings(src []byte, portdir, port string, deps []string) []record.Finding {
	dependent := make(map[string]bool, len(deps))
	for _, d := range deps {
		dependent[strings.ToLower(d)] = true
	}
	source := portfileSource(portdir)
	var out []record.Finding
	for _, b := range commentBlocks(src) {
		if !bumpVerb.MatchString(b.prose) || notBumping.MatchString(b.prose) {
			continue
		}
		named := namedPorts(b, dependent)
		kept := make([]string, 0, len(named))
		for _, n := range named {
			if !strings.EqualFold(n, port) {
				kept = append(kept, n)
			}
		}
		if len(named) > 0 && len(kept) == 0 {
			// The comment names this port and nothing else, so it is the
			// dependent's own note about what triggers a bump of IT —
			// mpv's "Please revbump mpv whenever linked ffmpeg is
			// updated!" is the tree's own example. Read as an instruction
			// it would say to revbump the port being changed, which is
			// nobody.
			continue
		}
		if len(kept) == 0 && !collective.MatchString(b.prose) {
			// A bump verb with no object: privoxy's "Please increase the
			// revision whenever curl-ca-bundle contents change" is about
			// its own revision and names no dependent at all. There is
			// nothing here for a cohort to weigh.
			continue
		}
		f := record.Finding{
			Kind:  FindingInstruction,
			Ports: []string{port},
			// Verbatim, and Criterion deliberately left empty: the
			// criterion of this finding IS its quote, and writing the same
			// sentence into two keys of every note would be two places for
			// it to drift.
			Source:      source,
			Quote:       b.text,
			Disposition: record.Proposed,
		}
		for _, n := range kept {
			f.Candidates = append(f.Candidates, record.Candidate{
				Port: n, Reason: "named by the instruction comment in " + source})
		}
		out = append(out, f)
	}
	return out
}

// block is one run of adjacent comment lines: the bytes as the Portfile
// writes them, and the same lines as prose for the patterns to read.
type block struct {
	// text is verbatim — the '#' and the indentation as they stand,
	// lines joined by the newline that separated them.
	text string
	// prose is the same lines with the comment marker and any "NOTE:"
	// stripped, joined by one space, so a sentence that wrapped reads as
	// one sentence.
	prose string
	// lines are the prose of each line on its own, for the roster forms
	// that are written one item per line.
	lines []string
}

// commentBlocks splits a Portfile into its runs of adjacent comment
// lines.
//
// Every comment line counts, wherever it sits. The rule that cares
// where a comment is — the rider's first proof — is about EDITING
// bytes; this only reads them, and privoxy's own instruction lives
// inside a subport block, so a reader that skipped the braced bodies
// would miss the shapes the tree actually writes.
func commentBlocks(src []byte) []block {
	var out []block
	var cur block
	flush := func() {
		if cur.text != "" {
			out = append(out, cur)
		}
		cur = block{}
	}
	for _, raw := range strings.Split(string(src), "\n") {
		trimmed := strings.TrimSpace(raw)
		if !strings.HasPrefix(trimmed, "#") {
			flush()
			continue
		}
		line := strings.TrimSpace(strings.TrimPrefix(trimmed, "#"))
		line = strings.TrimSpace(strings.TrimPrefix(line, "NOTE:"))
		if cur.text == "" {
			cur.text = raw
		} else {
			cur.text += "\n" + raw
		}
		cur.prose = strings.TrimSpace(cur.prose + " " + line)
		cur.lines = append(cur.lines, line)
	}
	flush()
	return out
}

// namedPorts reads the roster a comment block names, in the order it
// names them and without repeats.
//
// Two forms, and a block may use both: names in the sentence after the
// bump verb, and names on bullet lines under a header that pointed at
// them. openssl3 is the second alone — its header says "these ports"
// and the four names are bullets — and protobuf3-cpp is the first
// alone.
func namedPorts(b block, dependent map[string]bool) []string {
	seen := map[string]bool{}
	var out []string
	add := func(name string) {
		if name == "" || seen[strings.ToLower(name)] {
			return
		}
		seen[strings.ToLower(name)] = true
		out = append(out, name)
	}
	for _, m := range bumpVerb.FindAllStringIndex(b.prose, -1) {
		for _, name := range rosterAfter(b.prose[m[1]:], dependent) {
			add(name)
		}
	}
	for _, line := range b.lines {
		if m := bulletLine.FindStringSubmatch(line); m != nil {
			if name, ok := readName(m[1], dependent); ok {
				add(name)
			}
		}
	}
	return out
}

// rosterAfter reads the port names that follow a bump verb, stopping at
// the first token it cannot justify.
//
// Stopping — rather than skipping and continuing — is the refusal this
// rule is built on. A token the vocabulary does not know and the index
// does not call a dependent might be a port and might be the next word
// of an English sentence, and there is no way to tell from here; taking
// it would put a word like "whenever" into a note as a port, and
// skipping past it would let the sentence's object be read as a roster
// item three words later. A truncated roster beside a verbatim quote is
// the answer a human can finish.
func rosterAfter(rest string, dependent map[string]bool) []string {
	var out []string
	for _, word := range strings.Fields(rest) {
		// A clause boundary ends the roster whatever the word is: the
		// names belong to the sentence the verb is in.
		end := strings.ContainsAny(word, ".;:!?")
		name, ok := readName(word, dependent)
		switch {
		case ok:
			out = append(out, name)
		case listSkip[strings.ToLower(strings.Trim(word, ",.;:!?'\"`()"))]:
		default:
			return out
		}
		if end {
			return out
		}
	}
	return out
}

// readName reads one token as a port name, and says no where it cannot
// be justified.
//
// The token is stripped of the punctuation a list and a quote leave on
// it: grpc writes 'apache-arrow' in single quotes, openssl3's bullets
// carry a trailing parenthetical, and every list carries commas. The
// category prefix sbcl writes is dropped — "math/maxima" is the port
// maxima — because a candidate row names a port and the tree's own
// index is what says where it lives.
//
// The index wins over the word list. A token the dependents roster
// already names is a port however much it looks like prose, which is
// the one place this rule can be certain rather than careful.
func readName(word string, dependent map[string]bool) (string, bool) {
	cleaned := strings.Trim(word, ",.;:!?'\"`()[]")
	if cleaned == "" {
		return "", false
	}
	m := portName.FindStringSubmatch(cleaned)
	if m == nil {
		return "", false
	}
	name := m[1]
	if dependent[strings.ToLower(name)] {
		return name, true
	}
	lower := strings.ToLower(name)
	if listStop[lower] || listSkip[lower] {
		return "", false
	}
	return name, true
}

// portfileSource is where a finding says it read a comment:
// "<category>/<port>/Portfile", the way a reader would cite it and the
// way `git log` names the file.
//
// The last two elements of the portdir and not the whole path, because
// the whole path is this machine's and a note outlives it. A portdir
// with no category above it keeps whatever it has rather than inventing
// one.
func portfileSource(portdir string) string {
	if portdir == "" {
		return ""
	}
	clean := filepath.ToSlash(filepath.Clean(portdir))
	parts := strings.Split(clean, "/")
	if n := len(parts); n >= 2 {
		return parts[n-2] + "/" + parts[n-1] + "/" + macports.PortfileName
	}
	return clean + "/" + macports.PortfileName
}
