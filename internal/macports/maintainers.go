package macports

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/text"
)

// The keywords a maintainers entry can be in place of a person, which name
// no one: openmaintainer lets anyone change the port without asking, and
// nomaintainer says no one maintains it.
const (
	OpenMaintainer = "openmaintainer"
	NoMaintainer   = "nomaintainer"
)

// A Maintainer is one entry of a port's maintainers: the spellings that
// reach one person, as {@ada example.org:ada} reaches Ada by her GitHub
// handle and by her address, or a keyword alone.
type Maintainer []string

// ReadMaintainers reads a maintainers value, as a Portfile sets it and the
// port index carries it, the way MacPorts does: a Tcl list of entries,
// each a Tcl list of spellings. An empty entry is dropped, as MacPorts
// drops it.
func ReadMaintainers(value string) ([]Maintainer, error) {
	entries, failures := syntax.ListValues(value)
	if len(failures) > 0 {
		return nil, fmt.Errorf("maintainers %q: %s", value, failures[0].Type)
	}
	var maintainers []Maintainer
	for _, entry := range entries {
		spellings, failures := syntax.ListValues(entry)
		if len(failures) > 0 {
			return nil, fmt.Errorf("maintainers %q: %s", value, failures[0].Type)
		}
		if len(spellings) > 0 {
			maintainers = append(maintainers, spellings)
		}
	}
	return maintainers, nil
}

// MaintainerIdentity is who a maintainer's spelling names, spelled one way
// for each, in lowercase, so that two spellings of one maintainer are
// equal. As MacPorts reads spellings, a GitHub handle is @ada; an address
// is ada@example.org, which a Portfile obscures as example.org:ada, split
// at its first colon; a MacPorts handle, ada, is its address,
// ada@macports.org; and a keyword is itself. ada@github, as Repology
// spells a GitHub handle, is @ada.
func MaintainerIdentity(spelling string) string {
	spelling = strings.ToLower(spelling)
	if handle, ok := strings.CutSuffix(spelling, "@github"); ok {
		return "@" + handle
	}
	if strings.Contains(spelling, "@") {
		return spelling
	}
	if domain, user, ok := strings.Cut(spelling, ":"); ok {
		return user + "@" + domain
	}
	if MaintainerKeyword(spelling) {
		return spelling
	}
	return spelling + "@macports.org"
}

// MaintainerKeyword reports whether a spelling is openmaintainer or
// nomaintainer, which name no one.
func MaintainerKeyword(spelling string) bool {
	return spelling == OpenMaintainer || spelling == NoMaintainer
}

// CheckMaintainers checks a maintainers line as MacPorts writes one:
// entries apart by spaces, each a spelling or a braced group of them, as
// {@ada example.org:ada} openmaintainer, at least one of them. A spelling
// holds nothing Tcl reads specially, so the line means the same written
// as a Portfile's words as it does read as the port index's list.
func CheckMaintainers(line string) error {
	for _, r := range line {
		if strings.ContainsRune(`"$;[\]`, r) || unicode.IsControl(r) && r != '\t' {
			return fmt.Errorf("%q has %q, which Tcl reads specially", line, r)
		}
	}
	src := []byte(line)
	entries, failures := syntax.SplitList(src, text.Span{Start: 0, End: len(src)})
	if len(failures) > 0 {
		if failures[0].Type == syntax.ListUntermBrace {
			return fmt.Errorf("%q leaves a group open", line)
		}
		return fmt.Errorf("%q has a stray brace", line)
	}
	empty := true
	for _, entry := range entries {
		word := entry.Text(src)
		group, grouped := strings.CutPrefix(word, "{")
		if !grouped {
			if strings.ContainsAny(word, "{}") {
				return fmt.Errorf("%q has a stray brace", line)
			}
			empty = false
			continue
		}
		group = strings.TrimSuffix(group, "}")
		if strings.ContainsAny(group, "{}") {
			return fmt.Errorf("%q opens a group inside another", line)
		}
		empty = empty && strings.TrimSpace(group) == ""
	}
	if empty {
		return fmt.Errorf("%q has no entries", line)
	}
	return nil
}
