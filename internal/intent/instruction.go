package intent

import (
	"path/filepath"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portnote"
	"github.com/herbygillot/dockhand/internal/plan"
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
// The reading is portnote's and the judgment is this file's, and the
// line between them is exactly where it looks. portnote recognises the
// family and transcribes what a comment named; what makes this file the
// finding's author is the context portnote has none of — which port the
// file belongs to, and therefore which of the shapes it recognises are
// about somebody else and which are about the port's own revision.

// FindingInstruction is the kind an instruction-comment finding carries
// on the wire. It is a constant because two packages read it: this one
// writes it, and the settlement maps it back into the quotes a cohort
// decision weighs.
const FindingInstruction = "instruction-comment"

// instructionFindings reads the Portfile for comments of the
// revbump-instruction family and states each one as a finding.
//
// One finding per comment BLOCK, and the whole block is the quote —
// portnote's rule, because the condition a human has to weigh sits
// below the verb as often as above it and a quote cut to the matching
// line drops it in one of the two shapes.
//
// port is the context being changed and deps the ports the index says
// depend on it. Both narrow the reading rather than widening it: a
// comment whose only named port is the port itself is the REVERSE
// direction — it says what triggers a bump of this port, not what to
// bump with it — and a token the index already calls a dependent is a
// port whatever the word list thinks of it.
func instructionFindings(src []byte, portdir, port string, deps []string) []plan.Finding {
	source := portfileSource(portdir)
	var out []plan.Finding
	for _, note := range portnote.Instructions(src, deps) {
		kept := make([]string, 0, len(note.Ports))
		for _, n := range note.Ports {
			if !strings.EqualFold(n, port) {
				kept = append(kept, n)
			}
		}
		if len(note.Ports) > 0 && len(kept) == 0 {
			// The comment names this port and nothing else, so it is the
			// dependent's own note about what triggers a bump of IT —
			// mpv's "Please revbump mpv whenever linked ffmpeg is
			// updated!" is the tree's own example. Read as an instruction
			// it would say to revbump the port being changed, which is
			// nobody.
			continue
		}
		if len(kept) == 0 && !note.Collective {
			// A bump verb with no object: privoxy's "Please increase the
			// revision whenever curl-ca-bundle contents change" is about
			// its own revision and names no dependent at all. There is
			// nothing here for a cohort to weigh.
			continue
		}
		f := plan.Finding{
			Kind:  FindingInstruction,
			Ports: []string{port},
			// Verbatim, and Criterion deliberately left empty: the
			// criterion of this finding IS its quote, and writing the same
			// sentence into two keys of every note would be two places for
			// it to drift.
			Source:      source,
			Quote:       note.Quote,
			Disposition: plan.Proposed,
		}
		for _, n := range kept {
			f.Candidates = append(f.Candidates, plan.Candidate{
				Port: n, Reason: "named by the instruction comment in " + source})
		}
		out = append(out, f)
	}
	return out
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
