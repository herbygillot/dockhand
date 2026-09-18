package macports

import (
	"slices"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/record"
)

// ResolveStub resolves a bump's selection once, for binding and editing
// alike: a stub selection is redirected to the newest subport that carries
// its release and the stub's name is returned beside it; any other
// selection comes back unchanged with an empty name.
func ResolveStub(snapshot Snapshot, selected record.Target) (record.Target, string) {
	if selected.Subport != "" {
		return selected, ""
	}
	newest, _ := StubMembers(snapshot, selected.Name)
	if newest == "" {
		return selected, ""
	}
	return record.Target{Name: newest, Portfile: selected.Portfile, Subport: newest, Variants: selected.Variants}, selected.Name
}

// StubMembers reports whether the named port is a stub whose subports carry
// its release: it builds nothing itself while sibling subports at the same
// version do, the shape of a python `py-foo` port over its `py3x-foo`
// subports. It returns the newest such subport, by natural order of the
// names, and every member. An ordinary port returns "" and nil.
func StubMembers(snapshot Snapshot, name string) (newest string, members []string) {
	stub, ok := snapshot.Ports[name]
	if !ok || stub.Options["dockhand.metadata_only"] != "1" || stub.Version == "" {
		return "", nil
	}
	for sibling, info := range snapshot.Ports {
		if sibling == name || info.Version != stub.Version || info.Options["dockhand.metadata_only"] == "1" {
			continue
		}
		members = append(members, sibling)
	}
	if len(members) == 0 {
		return "", nil
	}
	slices.SortFunc(members, NaturalCompare)
	return members[len(members)-1], members
}

// NaturalCompare orders names with embedded numbers by their numeric value,
// so py314-foo sorts after py39-foo.
func NaturalCompare(a, b string) int {
	for a != "" && b != "" {
		ad, bd := leadingDigits(a), leadingDigits(b)
		if ad > 0 && bd > 0 {
			x, _ := strconv.ParseUint(a[:ad], 10, 64)
			y, _ := strconv.ParseUint(b[:bd], 10, 64)
			if x != y {
				if x < y {
					return -1
				}
				return 1
			}
			a, b = a[ad:], b[bd:]
			continue
		}
		if a[0] != b[0] {
			if a[0] < b[0] {
				return -1
			}
			return 1
		}
		a, b = a[1:], b[1:]
	}
	return strings.Compare(a, b)
}

func leadingDigits(s string) int {
	n := 0
	for n < len(s) && s[n] >= '0' && s[n] <= '9' {
		n++
	}
	return n
}
