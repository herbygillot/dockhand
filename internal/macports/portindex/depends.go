package portindex

import (
	"sort"
	"strings"

	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// The dependency fields a revbump cascade follows. They are the three
// that describe what a port needs in order to be built and to run, and
// so the three whose targets a changed ABI can reach.
//
// depends_build is here to be LISTED, never to be proposed: a
// build-only dependent links against nothing, so a revbump of it
// rebuilds a binary that did not change. It is carried because a
// dependent that turns out to be build-only is a fact the proposal has
// to state, not one it should silently drop.
//
// depends_test, depends_extract, depends_fetch and depends_patch are
// deliberately absent: they name tools a build runs, not libraries it
// links.
const (
	DependsLib   = "depends_lib"
	DependsBuild = "depends_build"
	DependsRun   = "depends_run"
)

// dependencyKeys is the order Dependent.Keys reports edges in, so a row
// reads the same on every run.
var dependencyKeys = []string{DependsLib, DependsBuild, DependsRun}

// Dependent is one port that declares a dependency on another, carrying
// what a cohort decision needs to know about it without opening a tree
// of its own.
//
// Requires is the row's own dependency targets. It is here so that
// ordering a cohort — build the library before the things that link it
// — is arithmetic over values a caller was handed, rather than a second
// walk of the index from inside a judgment that is not allowed one.
type Dependent struct {
	Name    string // the dependent's name, as indexed
	Portdir string // its portdir, slash-relative to the tree root

	// Keys are the depends_* fields carrying this edge, in
	// dependencyKeys order. A port may declare the same target under
	// more than one — 5702 pairs do — so a row that is build-only is
	// one whose Keys hold depends_build alone.
	Keys []string

	ReplacedBy   string   // the dependent's own replaced_by, "" when none
	KnownFail    bool     // the dependent's own known_fail
	Nomaintainer bool     // the dependent declares the nomaintainer keyword
	Requires     []string // its own dependency targets, lowercased and sorted

	// Conflicts is the dependent's own conflicts field, lowercased. Two
	// members that name each other cannot be installed in one guest, so
	// a cohort that stages both spends a build proving MacPorts will
	// refuse the second — measured at 2303 index rows, this is ordinary
	// rather than exotic.
	Conflicts []string
}

// BuildOnly reports whether the edge is carried by depends_build alone,
// which is the case that gets listed and never proposed.
func (d Dependent) BuildOnly() bool {
	return len(d.Keys) == 1 && d.Keys[0] == DependsBuild
}

// DependencyName extracts the depended-on port name from one dependency
// token.
//
// MacPorts writes four forms, and the index stores them fully expanded:
// port:NAME, and lib:TEST:NAME, bin:TEST:NAME and path:TEST:NAME, whose
// middle field is what the test looks for rather than what provides it.
// netdata's own depends_lib carries all four in one value. Taking
// everything after the LAST colon is right for all of them and needs no
// per-prefix branch — including for the one token in the maintainer's
// tree that breaks the shape, `port:bin/cmake:cmake`, which resolves to
// cmake rather than to the nothing that reading field two would give.
//
// A token with no colon is the legacy bare form and names the port
// outright. None appear in the tree measured (218859 port:, 13820
// path:, 5458 bin:, 80 lib:, 0 bare), but reading one as a name costs
// nothing and dropping one would lose a dependent silently.
func DependencyName(token string) string {
	if i := strings.LastIndexByte(token, ':'); i >= 0 {
		return token[i+1:]
	}
	return token
}

// dependencyEdges maps each port this entry depends on, lowercased, to
// the depends_* keys declaring it.
//
// Each value is split as the Tcl list it is rather than on whitespace.
// 32 values in the maintainer's tree carry a brace-quoted element —
// mercurial's depends_build holds `{bin:${prefix}/bin/gmake:gmake}`,
// BiggerSQL's depends_lib `{path:${prefix}/lib/postgresql84:postgresql84}`
// — where whitespace splitting leaves the closing brace attached and
// yields a port named postgresql84}.
//
// Keys are lowercased because the index's own names are matched that
// way (Lookup does it, following the accelerator's pre-lowercased
// keys), and because dependents do not always spell a port the way it
// is indexed: cubeb and VLC2 declare port:speexDSP where the port is
// speexdsp, virglrenderer port:moltenvk where it is MoltenVK. Folding
// recovers 40 edges that exact matching drops, and it is lossless —
// among 41630 indexed names there is not one case collision.
func (e Entry) dependencyEdges() (map[string][]string, []string) {
	var edges map[string][]string
	var unread []string
	for _, key := range dependencyKeys {
		raw := e.Fields[key]
		if raw == "" {
			continue
		}
		tokens, errs := syntax.ListValues(raw)
		if len(errs) > 0 {
			// An unsplittable value is one field of one entry. Losing
			// its edges is a short cohort; losing the entry's other
			// fields with it would be a missing port. It is COUNTED
			// rather than merely stepped over: a cohort that is short
			// because a field would not parse is a dependent left broken,
			// and the whole argument for this index refusing rather than
			// truncating is undone if it truncates one field in silence.
			unread = append(unread, key)
			continue
		}
		for _, tok := range tokens {
			name := strings.ToLower(DependencyName(tok))
			if name == "" {
				continue
			}
			if edges == nil {
				edges = make(map[string][]string, len(tokens))
			}
			if ks := edges[name]; len(ks) == 0 || ks[len(ks)-1] != key {
				edges[name] = append(ks, key)
			}
		}
	}
	return edges, unread
}

// Unread is one dependency field that would not split as a Tcl list, so
// whatever it declared is missing from the reverse index.
//
// It is reported rather than counted away because of what its absence
// looks like: a cohort with a member missing and nothing said about it,
// which is exactly the outcome this index refuses a partial walk to
// avoid. Zero of the 116 entries in the captured slice carry one, and
// the incidence on a full 41630-entry tree is unmeasured — which is
// itself a reason to surface it rather than assume it stays zero.
type Unread struct {
	// Port is the entry whose field could not be read, as indexed, and
	// Portdir is where it lives.
	Port    string
	Portdir string
	// Field is the depends_* key that would not split.
	Field string
}

// Reverse is the reverse dependency index and what could not be read
// while building it.
type Reverse struct {
	// ByPort maps a port name, lowercased, to every port declaring it.
	ByPort map[string][]Dependent
	// Unread are the fields that would not split, sorted by port and
	// then field. A caller that proposes a cohort states them: the list
	// it is about to put forward may be short by exactly these.
	Unread []Unread
}

// Dependents builds the reverse dependency index in one pass: for each
// port name, lowercased, every port that declares it under depends_lib,
// depends_build or depends_run.
//
// A dependent is reported at its own Portdir, which is the parent
// directory for every subport by construction — and the parent is the
// staging unit, because a subport has no directory of its own. That
// mapping is not derived from the name and must not be: 21570 of 41630
// indexed names (51.8%) differ from their portdir's basename, and the
// same 21570 match no portdir basename anywhere in the tree. Callers
// that stage a cohort dedupe on Portdir, so gdal's 82 dependents become
// 39 portdirs to edit.
//
// Rows come back sorted by name, so two runs over one tree propose the
// same cohort in the same order.
func (ix *Index) Dependents() (Reverse, error) {
	rev := make(map[string][]Dependent)
	var unread []Unread
	err := ix.Each(func(e Entry) bool {
		edges, bad := e.dependencyEdges()
		for _, key := range bad {
			unread = append(unread, Unread{Port: e.Name, Portdir: e.Portdir, Field: key})
		}
		if len(edges) == 0 {
			return true
		}
		row := Dependent{
			Name:         e.Name,
			Portdir:      e.Portdir,
			ReplacedBy:   e.Fields["replaced_by"],
			KnownFail:    tclTrue(e.Fields["known_fail"]),
			Requires:     make([]string, 0, len(edges)),
			Nomaintainer: e.Nomaintainer(),
			Conflicts:    lowerFields(e.Fields["conflicts"]),
		}
		for name := range edges {
			row.Requires = append(row.Requires, name)
		}
		sort.Strings(row.Requires)
		for name, keys := range edges {
			member := row
			member.Keys = keys
			rev[name] = append(rev[name], member)
		}
		return true
	})
	if err != nil {
		return Reverse{}, err
	}
	for name := range rev {
		rows := rev[name]
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].Name != rows[j].Name {
				return rows[i].Name < rows[j].Name
			}
			return rows[i].Portdir < rows[j].Portdir
		})
	}
	sort.Slice(unread, func(i, j int) bool {
		if unread[i].Port != unread[j].Port {
			return unread[i].Port < unread[j].Port
		}
		return unread[i].Field < unread[j].Field
	})
	return Reverse{ByPort: rev, Unread: unread}, nil
}

// tclTrue reads an index field written as a Tcl boolean. known_fail is
// the field this exists for, and it is only ever "yes" in the tree
// measured; the rest of Tcl's truthy set is accepted because the index
// is generated by a Tcl program that could write any of it.
func tclTrue(v string) bool {
	switch strings.ToLower(v) {
	case "yes", "true", "on", "1":
		return true
	default:
		return false
	}
}

// lowerFields reads a whitespace-separated index field as a lowercased
// list. A single value is written bare and several are braced, and Tcl
// leaves the braces on the value this reader is handed, so they are
// trimmed rather than parsed.
func lowerFields(v string) []string {
	v = strings.Trim(strings.TrimSpace(v), "{}")
	if v == "" {
		return nil
	}
	out := strings.Fields(strings.ToLower(v))
	sort.Strings(out)
	return out
}
