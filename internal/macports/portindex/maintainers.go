package portindex

import (
	"sort"
	"strings"

	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// The keywords MacPorts allows among a port's maintainers. Neither
// names a person: openmaintainer invites unsolicited changes, and
// nomaintainer says there is nobody to ask.
const (
	openMaintainer = "openmaintainer"
	noMaintainer   = "nomaintainer"
)

// Key prefixes for a normalized maintainer. The two spellings are kept
// apart on purpose. devans and @dbevans are one person maintaining the
// same 2325 ports, and gh:herby, gh:herby-gillot and gh:herbygillot are
// one more; merging them would take a guess about identity that the
// index gives no ground for, and the tool's job here is to say who a
// port names, not who that is.
const (
	MaintainerGitHub = "gh:"   // gh:<lowercased handle>
	MaintainerMail   = "mail:" // mail:<local>@<domain>
)

// Maintainers normalizes one entry's maintainers field into keys, and
// reports whether it declares nomaintainer.
//
// The field is a Tcl list whose elements are themselves lists, which is
// why it is split twice: djview-qt5 carries `{nicos @NicosPavlov}
// openmaintainer`, one person under two spellings plus a keyword.
// Splitting on whitespace instead would produce three maintainers named
// `{nicos`, `@NicosPavlov}` and `openmaintainer`, none of them real.
//
// Each sub-token is one of four shapes, counted over the maintainer's
// tree: an @handle (507 distinct), an obfuscated address written
// domain-first as `domain:local` (599), a bare word (79), or a keyword
// (2). A bare word is a MacPorts address by the project's own
// convention, so `devans` normalizes the same way `macports.org:devans`
// would.
//
// nomaintainer comes back as its own answer rather than as a key. It is
// on 23086 of 41630 ports, better than a third of the tree, and a
// cohort that annotates a member "nomaintainer" is saying nobody is
// there to ask — treating the keyword as a person would invert that.
func Maintainers(field string) (keys []string, nomaintainer bool) {
	elems, errs := syntax.ListValues(field)
	if len(errs) > 0 {
		return nil, false
	}
	seen := make(map[string]bool)
	for _, elem := range elems {
		subs, subErrs := syntax.ListValues(elem)
		if len(subErrs) > 0 {
			continue
		}
		for _, sub := range subs {
			switch strings.ToLower(sub) {
			case noMaintainer:
				nomaintainer = true
				continue
			case openMaintainer, "":
				continue
			}
			key := maintainerKey(sub)
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys, nomaintainer
}

// maintainerKey normalizes one sub-token. Keys are lowercased: GitHub
// handles are case-insensitive, mail domains are, and among the 1187
// distinct tokens in the tree measured there is not one pair differing
// only by case, so folding loses nothing.
func maintainerKey(sub string) string {
	if rest, ok := strings.CutPrefix(sub, "@"); ok {
		if rest == "" {
			return ""
		}
		return MaintainerGitHub + strings.ToLower(rest)
	}
	// The obfuscated form is written domain first, so the first colon
	// is the split. One token in the tree carries two colons
	// (`gmail.com:huanguan1978:crown.hg`); the remainder stays in the
	// local part rather than being reinterpreted, because what the port
	// meant by it is the port's business.
	if domain, local, ok := strings.Cut(sub, ":"); ok {
		if domain == "" || local == "" {
			return ""
		}
		return MaintainerMail + strings.ToLower(local+"@"+domain)
	}
	return MaintainerMail + strings.ToLower(sub+"@macports.org")
}

// Nomaintainer reports whether the entry declares the nomaintainer
// keyword.
func (e Entry) Nomaintainer() bool {
	_, none := Maintainers(e.Fields["maintainers"])
	return none
}

// Maintainers returns the entry's normalized maintainer keys.
func (e Entry) Maintainers() []string {
	keys, _ := Maintainers(e.Fields["maintainers"])
	return keys
}

// ByMaintainer builds the maintainer index in one pass: normalized key
// to the ports naming it, sorted. Ports with no maintainer appear under
// no key at all, which is what nomaintainer means.
func (ix *Index) ByMaintainer() (map[string][]string, error) {
	byKey := make(map[string][]string)
	err := ix.Each(func(e Entry) bool {
		for _, key := range e.Maintainers() {
			byKey[key] = append(byKey[key], e.Name)
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	for key := range byKey {
		sort.Strings(byKey[key])
	}
	return byKey, nil
}
