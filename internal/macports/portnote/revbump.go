package portnote

import (
	"regexp"
	"strings"
)

// The revbump-instruction family: a comment in a Portfile telling
// whoever updates this port to bump something else.
//
// It is the one annotation a reader can act on with nothing but the
// file. Everything else a cohort decision rests on is measured — an
// install name that moved, a compatibility version that went backwards
// — and measuring needs an environment that has built the port. A
// maintainer's own written instruction needs only the bytes, and it is
// evidence of a kind no measurement produces: it can name a break otool
// cannot see, and it can be wrong, which is why what this package does
// with it is transcribe it.
//
// A comment is never a roster to act on, and telling the two forms
// apart is the reason this reader exists. The named form carries port
// names a caller can look up; the unnamed form ("all dependents will
// need to be rev-bumped") names nobody on purpose, and reading it as
// "every dependent" would auto-include hundreds of ports off one
// sentence. Both are reported, and they are reported as different
// things.

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
// both a refusal and an instruction is read as a refusal, which is the
// safe half of the trade: a missed instruction leaves the measurement
// to speak for itself, and a quoted refusal would hold publication for
// a comment that asked for nothing.
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
// must be shown and a tool must not act on.
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
// mechanism and also the whole of the guessing this reader does, so it
// is closed and each member is here for a reason:
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
// token justified by the vocabulary or by the caller's own roster is
// taken as a port.
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
// is what listStop and the caller's roster are for.
var portName = regexp.MustCompile(`^(?:[A-Za-z0-9_.+-]+/)?([A-Za-z][A-Za-z0-9._+-]*)$`)

// Instruction is one comment of the revbump-instruction family, as the
// Portfile writes it.
//
// It is an observation and not a proposal: it says what the comment is
// and what it named, and it says nothing about the port it was read
// from — a comment that names only the port whose file it sits in is
// the reverse direction, and a comment that names nobody and asks for
// no class is about the port's own revision, and both of those are
// judgments a caller makes with context this reader does not have.
type Instruction struct {
	// Quote is the whole comment block, verbatim.
	//
	// The whole block and never the matching line: the condition a human
	// has to weigh sits below the verb in dav1d ("... whenever" /
	// "dav1d's version is updated.") and above it in protobuf3-cpp ("For
	// a minor or major version number change, also" / "Revbump et,
	// protobuf-c, mosh and py-onnx"), so a quote cut to the matching
	// line drops the condition in one of the two shapes. A verbatim
	// quote that is not verbatim is worse than no quote at all.
	Quote string
	// Ports are the port names the comment names, in the order it names
	// them and without repeats. Empty for the unnamed form, and empty
	// for a bump verb with no object at all.
	Ports []string
	// Collective reports the unnamed form: the comment asks for the
	// dependents as a class. It is stated beside an empty Ports rather
	// than folded into it because the two mean opposite things — a class
	// with no members named is a criterion a person must be shown, and
	// no class and no names is a sentence about this port's own revision.
	Collective bool
}

// MentionsRevbump reports whether a Portfile carries a comment of the
// revbump-instruction family at all.
//
// It is the cheap half, for a caller deciding whether the expensive
// half is worth paying for. The roster Instructions reads names against
// is the tree's reverse index, and building that index is one
// sequential pass over the whole PortIndex — 25.6 MB and 41630 entries
// on a real tree. A few dozen Portfiles carry one of these comments, so
// a caller that filled the roster unconditionally would spend that pass
// on every port in order to narrow a roster that will never be
// consulted.
//
// It is the same two patterns Instructions uses over the same blocks,
// so a comment this says no about is one Instructions would have
// skipped. False here means the roster changes nothing; it never means
// the roster is unavailable.
func MentionsRevbump(src []byte) bool {
	for _, b := range Blocks(src) {
		if bumpVerb.MatchString(b.Prose) && !notBumping.MatchString(b.Prose) {
			return true
		}
	}
	return false
}

// Instructions reads every comment of the revbump-instruction family a
// Portfile carries, one per comment BLOCK.
//
// known is the roster the caller already knows to be ports — the tree's
// reverse index for the port being read, where a caller has one. It
// only ever widens what is recognised, never narrows it: a token the
// index already calls a port is a port however much it looks like
// prose, which is the one place this reader can be certain rather than
// careful. A caller with no index passes nothing and gets the word
// list's answer, which is the answer for every ordinary port anyway.
func Instructions(src []byte, known []string) []Instruction {
	index := make(map[string]bool, len(known))
	for _, k := range known {
		index[strings.ToLower(k)] = true
	}
	var out []Instruction
	for _, b := range Blocks(src) {
		if !bumpVerb.MatchString(b.Prose) || notBumping.MatchString(b.Prose) {
			continue
		}
		out = append(out, Instruction{
			Quote:      b.Text,
			Ports:      namedPorts(b, index),
			Collective: collective.MatchString(b.Prose),
		})
	}
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
func namedPorts(b Block, known map[string]bool) []string {
	seen := map[string]bool{}
	var out []string
	add := func(name string) {
		if name == "" || seen[strings.ToLower(name)] {
			return
		}
		seen[strings.ToLower(name)] = true
		out = append(out, name)
	}
	for _, m := range bumpVerb.FindAllStringIndex(b.Prose, -1) {
		for _, name := range rosterAfter(b.Prose[m[1]:], known) {
			add(name)
		}
	}
	for _, line := range b.Lines {
		if m := bulletLine.FindStringSubmatch(line); m != nil {
			if name, ok := readName(m[1], known); ok {
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
// reader is built on. A token the vocabulary does not know and the
// caller's roster does not name might be a port and might be the next
// word of an English sentence, and there is no way to tell from here;
// taking it would put a word like "whenever" forward as a port, and
// skipping past it would let the sentence's object be read as a roster
// item three words later. A truncated roster beside a verbatim quote is
// the answer a human can finish.
func rosterAfter(rest string, known map[string]bool) []string {
	var out []string
	for _, word := range strings.Fields(rest) {
		// A clause boundary ends the roster whatever the word is: the
		// names belong to the sentence the verb is in.
		end := strings.ContainsAny(word, ".;:!?")
		name, ok := readName(word, known)
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
// maxima — because a caller names a port and the tree's own index is
// what says where it lives.
//
// The caller's roster wins over the word list, which is the whole
// reason it is a parameter.
func readName(word string, known map[string]bool) (string, bool) {
	cleaned := strings.Trim(word, ",.;:!?'\"`()[]")
	if cleaned == "" {
		return "", false
	}
	m := portName.FindStringSubmatch(cleaned)
	if m == nil {
		return "", false
	}
	name := m[1]
	if known[strings.ToLower(name)] {
		return name, true
	}
	lower := strings.ToLower(name)
	if listStop[lower] || listSkip[lower] {
		return "", false
	}
	return name, true
}
