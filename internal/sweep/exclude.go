package sweep

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/herbygillot/dockhand/internal/intent"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/macports/tree"
)

// Reason is why a target was excluded from a sweep.
type Reason int

const (
	// ReplacedBy is the index's replaced_by field: the only
	// machine-readable obsolescence marker a real PortIndex carries.
	// There is no obsolete category and no obsolete field.
	ReplacedBy Reason = iota
	// ObsoleteGroup is the obsolete PortGroup with no replaced_by,
	// recognized by the description the group itself writes.
	ObsoleteGroup
	// DoNotUpgrade is a maintainer's comment pinning the version,
	// sitting against the line that declares it.
	DoNotUpgrade
	// RevbumpHub is a Portfile whose comments instruct that its
	// dependents be revbumped when it moves — a bump that obliges a
	// cascade no sweep can perform.
	RevbumpHub
)

func (r Reason) String() string {
	switch r {
	case ReplacedBy:
		return "replaced"
	case ObsoleteGroup:
		return "obsolete"
	case DoNotUpgrade:
		return "do-not-upgrade"
	case RevbumpHub:
		return "revbump-hub"
	}
	return "unknown reason"
}

// Human reports whether the exclusion routes to a human lane rather
// than to the bin.
//
// A replaced or obsolete port is finished and nothing is owed about it.
// A pinned version and a revbump hub are the opposite: somebody decided
// something a sweep cannot re-decide, and the exclusion exists so that
// decision reaches a person with the evidence attached. That is also
// why a false positive is cheap here and a false negative is not — the
// first costs a review, the second bumps a port its maintainer pinned.
func (r Reason) Human() bool {
	switch r {
	case ReplacedBy, ObsoleteGroup:
		return false
	case DoNotUpgrade, RevbumpHub:
		return true
	}
	return false
}

// Excluded is one target a sweep should not touch, and the evidence.
type Excluded struct {
	Target tree.Target
	Reason Reason
	// Detail is one line saying what the signal was.
	Detail string
	// Quote is the Portfile's own words, verbatim, when the signal was
	// a comment — the whole block, indentation and '#' included,
	// because the condition a human has to weigh sits above the verb
	// as often as below it.
	Quote string
}

// Subject is the material one target's exclusion is decided from: the
// Portfile's bytes and the target's own indexed fields.
//
// Both are gathered by the caller. Exclusions never opens anything,
// which is what lets every rule below be tested against a quoted
// Portfile with no tree, no PortIndex and no MacPorts on the machine.
type Subject struct {
	Target tree.Target
	// Src is the Portfile's bytes. Nil is allowed and simply means the
	// comment rules find nothing.
	Src []byte
	// Indexed is the target's own PortIndex entry fields — the
	// subport's when the Target names one, the portdir's top-level
	// entry otherwise. Nil means the index had nothing to say.
	Indexed map[string]string
}

// Exclusions splits subjects into the ones a sweep should work on and
// the ones it should not, with a reason for every exclusion.
//
// Four signals, tested in this order, first match wins:
//
// replaced_by, straight from the index. 2158 entries carry it on the
// maintainer's tree, about 2004 of them the perl5 group's unversioned
// stubs — `outdated category:perl` without this asks upstream about
// two thousand ports that were retired years ago, which is the
// politeness ruling's own worked example and costs nothing to prevent.
// The value is quoted, never resolved: science/wfview's replaced_by is
// a URL, not a port name.
//
// The obsolete PortGroup's description fingerprint, for the 27 entries
// that use the group and set no replaced_by. known_fail is NOT a
// substitute — 1048 entries carry it while being neither obsolete nor
// replaced, and excluding on it would drop a thousand live ports.
//
// A do-not-upgrade comment abutting the version.
//
// A revbump-instruction comment, through intent's own reader, so the
// rule that decides it is the one the planner already uses.
//
// The first two key off the target's own index entry and never off a
// text scan for `PortGroup obsolete`. 2041 portdirs hold both an
// obsolete entry and a live one — devel/libftdi's obsolete top-level
// port sits above libftdi0 and libftdi1 in the same file — so a scan of
// the file would exclude all three; and devel/cmake-devel writes one of
// its obsolete subports with the PortGroup line commented out and the
// replaced_by still set, which the index sees and a scan does not.
func Exclusions(subjects []Subject) ([]tree.Target, []Excluded) {
	keep := make([]tree.Target, 0, len(subjects))
	var out []Excluded
	for _, s := range subjects {
		if ex, ok := exclude(s); ok {
			out = append(out, ex)
			continue
		}
		keep = append(keep, s.Target)
	}
	return keep, out
}

func exclude(s Subject) (Excluded, bool) {
	if v := strings.TrimSpace(s.Indexed["replaced_by"]); v != "" {
		return Excluded{Target: s.Target, Reason: ReplacedBy,
			Detail: fmt.Sprintf("the index says this port is replaced by %q", v)}, true
	}
	if d := strings.TrimSpace(s.Indexed["description"]); obsoleteDescription(d) {
		return Excluded{Target: s.Target, Reason: ObsoleteGroup,
			Detail: fmt.Sprintf("the obsolete PortGroup describes this port as %q", d)}, true
	}
	if len(s.Src) == 0 {
		return Excluded{}, false
	}
	if b, ok := doNotUpgrade(s.Src); ok {
		return Excluded{Target: s.Target, Reason: DoNotUpgrade,
			Detail: "a comment against the version line asks that this port not be moved",
			Quote:  b}, true
	}
	// No Quote for a hub. intent owns the two patterns that decide this
	// and exports only the yes/no, and a quote reproduced from a second
	// copy of those patterns would be a verbatim contract with two
	// places to drift. The reason plus the portdir is enough to find
	// the comment; the planner quotes it properly when it plans the
	// port.
	if intent.MentionsRevbump(s.Src) {
		return Excluded{Target: s.Target, Reason: RevbumpHub,
			Detail: "a comment instructs that dependents be revbumped when this port moves"}, true
	}
	return Excluded{}, false
}

// obsoleteDescription recognizes the description the obsolete PortGroup
// writes for itself — the second signal, for the ports that use the
// group and name no replacement. Both spellings come from the group's
// own set_descriptions.
func obsoleteDescription(d string) bool {
	return d == "Obsolete port" || strings.HasPrefix(d, "Obsolete port, replaced by ")
}

// doNotUpgradePhrase is the family of sentences a maintainer writes to
// say a version is held where it is. Measured over the maintainer's
// tree: 57 comment blocks match it.
var doNotUpgradePhrase = regexp.MustCompile(
	`(?i)\b(?:do not|don't|dont|never)\s+(?:update|upgrade|bump|advance)\b` +
		`|\b(?:stay|stays|staying|remain|remains|keep|keeping|kept|stick|sticking)\s+(?:at|on|with|to|this at)\b` +
		`|\bpin(?:ned)?\s+(?:to|at)\b` +
		`|\bpegged\b` +
		`|\bwill not be updated\b`)

// versionDeclaring recognizes a line that states the port's version:
// the field itself, the forge setup commands that carry one as an
// argument, and epoch, which is half of a MacPorts version identity and
// only ever written beside the other half.
var versionDeclaring = regexp.MustCompile(`^\s*(?:` +
	`version|epoch` +
	`|github\.setup|gitlab\.setup|bitbucket\.setup|codeberg\.setup|sourcehut\.setup` +
	`|go\.setup|golang\.setup|crates\.setup|cargo\.setup|ruby\.setup|perl5\.setup` +
	`|python\.setup|php\.setup|haskell\.setup|xcode\.setup|cran\.setup|R\.setup` +
	`|fossil\.setup|svn\.setup` +
	`)\s`)

// scopeChanging recognizes a line that means the scan has left the
// version's neighbourhood: a variable assignment, or the opening of a
// conditional, a variant, a subport or any other braced body. It is the
// whole of what separates the true pins from the false ones, because
// every measured false positive abuts a `set` or a block opener rather
// than a version.
var scopeChanging = regexp.MustCompile(`^\s*(?:set|if|else|elseif|platform|variant|subport|foreach|while|switch|proc|for)\b|\{\s*$`)

// adjacencyWindow is how many code lines the scan will cross to reach a
// version. Three covers the header shapes the tree writes — a comment
// above `name` above `version`, or below `version` above `revision` —
// without wandering into the next stanza.
const adjacencyWindow = 3

// doNotUpgrade reports whether the Portfile carries a comment of the
// do-not-upgrade family sitting against the line that declares its
// version, and returns that comment verbatim.
//
// Two conjoined tests, both mechanical, because phrase-matching alone
// is not enough. graphics/glfw's "it will not be updated" is inside a
// long_description continuation and is not a comment at all, so the
// first test drops it. The rest of the measured false positives are
// real comments about something else that happens to be held still —
// llvm-14's Python version, cmus's ffmpeg major, pypy's bootstrap
// tarball, iana-enterprise-numbers' pinned commit — and every one of
// them abuts a `set` or a block opener. The second test drops those.
//
// The scan looks both ways. multimedia/mkvtoolnix-legacy and
// net/qBittorrent-qt5 write the note UNDER the version they are holding,
// and a forward-only rule would read the maintainer's instruction and
// bump the port anyway.
//
// Measured over the maintainer's tree: of the 57 comment blocks the
// phrase family matches, 37 are version-adjacent and 20 are not, and
// every false positive the survey named falls on the ignored side. One
// residual stays on the excluded side — sysutils/cpmtools sits above
// its dist_subdir and below its version saying that UPSTREAM does not
// update the version number — and it is left there deliberately.
//
// This is a textual approximation of an anchor that exists properly
// elsewhere — portstyle.Locate corroborates the version's span against
// the evaluated value — and it is textual on purpose: exclusion happens
// at selection time, before any evaluator has met the port, which is
// the whole point of paying for it. The residual error is one-sided by
// design: this routes to a human lane, so a false positive costs a
// review and a false negative bumps a port somebody pinned.
func doNotUpgrade(src []byte) (string, bool) {
	lines := strings.Split(string(src), "\n")
	for _, b := range commentBlocks(lines) {
		if !doNotUpgradePhrase.MatchString(b.prose) {
			continue
		}
		if nearVersion(lines, b.end+1, 1) || nearVersion(lines, b.start-1, -1) {
			return b.text, true
		}
	}
	return "", false
}

// nearVersion walks away from a comment block in one direction, over
// blank lines and other comments, and reports whether it reaches a
// version declaration before it leaves the stanza or exhausts the
// window.
func nearVersion(lines []string, from, step int) bool {
	crossed := 0
	for i := from; i >= 0 && i < len(lines) && crossed < adjacencyWindow; i += step {
		t := strings.TrimSpace(lines[i])
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		if versionDeclaring.MatchString(lines[i]) {
			return true
		}
		if scopeChanging.MatchString(lines[i]) {
			return false
		}
		crossed++
	}
	return false
}

// block is one run of adjacent comment lines: verbatim, as prose for
// the patterns to read, and where it sits.
//
// This is a second comment splitter — intent has one for the
// revbump-instruction rule and does not export it — and the duplication
// is the cost of Exclusions being pure and self-contained. The contract
// they share is that text is verbatim; if intent ever exports its
// reader, this should go.
type block struct {
	text  string
	prose string
	start int
	end   int
}

// commentBlocks splits lines into runs of adjacent comment lines. Every
// comment counts wherever it sits: the note that matters is as likely
// to be inside a variant or a platform block as at the top of the file.
func commentBlocks(lines []string) []block {
	var out []block
	var cur block
	open := false
	flush := func(end int) {
		if open {
			cur.end = end
			out = append(out, cur)
		}
		cur, open = block{}, false
	}
	for i, raw := range lines {
		trimmed := strings.TrimSpace(raw)
		if !strings.HasPrefix(trimmed, "#") {
			flush(i - 1)
			continue
		}
		line := strings.TrimSpace(strings.TrimPrefix(trimmed, "#"))
		line = strings.TrimSpace(strings.TrimPrefix(line, "NOTE:"))
		if !open {
			open, cur.start, cur.text = true, i, raw
		} else {
			cur.text += "\n" + raw
		}
		cur.prose = strings.TrimSpace(cur.prose + " " + line)
	}
	flush(len(lines) - 1)
	return out
}

// Selection is what Select decided about a set of targets.
type Selection struct {
	// Keep are the targets the sweep should work on.
	Keep []tree.Target
	// Excluded are the rest, each with its reason.
	Excluded []Excluded
}

// Select gathers the material Exclusions needs and applies it: one pass
// over the tree's PortIndex, then each surviving target's Portfile.
//
// It is a selector-time filter and belongs to selector-shaped
// invocations. A user who names one port has already made the decision
// this makes for them, and a verb that refused a named port because the
// index calls it replaced would have changed what a single-target
// invocation does. Whether to call this is the caller's, and no verb
// should call it for a sweep of one.
//
// The index pass is the same full read Tree.Dependents pays for and is
// paid once per invocation, never per target. The Portfile reads are
// one per surviving target; a Portfile that cannot be read excludes
// nothing, because a filter that dropped ports on an I/O error would
// silently shrink a sweep for a reason that has nothing to do with the
// ports.
func Select(tr *tree.Tree, targets []tree.Target) (Selection, error) {
	fields, err := indexFields(tr)
	if err != nil {
		return Selection{}, err
	}
	subjects := make([]Subject, 0, len(targets))
	for _, t := range targets {
		s := Subject{Target: t, Indexed: fields(t)}
		if pf, err := t.Portfile(); err == nil {
			if src, err := os.ReadFile(pf); err == nil {
				s.Src = src
			}
		}
		subjects = append(subjects, s)
	}
	keep, excluded := Exclusions(subjects)
	return Selection{Keep: keep, Excluded: excluded}, nil
}

// indexFields builds the target→fields lookup Select hands to
// Exclusions, from one pass over the tree's PortIndex.
//
// A Target with a Subport names its entry outright. A Target resolved
// by location names the portdir's top-level port, which the index does
// not mark, so it is found by the directory's base name — the
// convention the whole tree follows — and only when an entry in that
// portdir actually carries it. A portdir whose top-level entry cannot
// be identified yields no fields and is therefore never excluded on an
// index signal: an exclusion has to rest on evidence, not on the
// absence of it.
//
// A tree with no PortIndex is not an error. Name resolution needs one;
// this does not, and a sweep over portdir paths on an unindexed tree
// should lose the two index rules rather than fail.
func indexFields(tr *tree.Tree) (func(tree.Target) map[string]string, error) {
	root := tr.Root()
	byDir := map[string]map[string]map[string]string{}
	ix, err := portindex.Open(root)
	if err != nil {
		if errors.Is(err, portindex.ErrNoIndex) {
			return func(tree.Target) map[string]string { return nil }, nil
		}
		return nil, err
	}
	if err := ix.Each(func(e portindex.Entry) bool {
		dir := filepath.Join(root, filepath.FromSlash(e.Portdir))
		if byDir[dir] == nil {
			byDir[dir] = map[string]map[string]string{}
		}
		byDir[dir][e.Name] = e.Fields
		return true
	}); err != nil {
		return nil, err
	}
	return func(t tree.Target) map[string]string {
		entries := byDir[t.Portdir]
		if entries == nil {
			return nil
		}
		name := t.Subport
		if name == "" {
			name = filepath.Base(t.Portdir)
		}
		return entries[name]
	}, nil
}
