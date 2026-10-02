package macports

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/model"
)

// ResolveStub resolves a bump's selection once, for binding and editing
// alike: a stub selection is redirected to the newest subport that carries
// its release and the stub's name is returned beside it; any other
// selection comes back unchanged with an empty name. Where the evaluator
// couldn't tell whether the selected port builds anything, it says so,
// rather than take a stub for an ordinary port.
func ResolveStub(snapshot Snapshot, selected model.Target) (model.Target, string, error) {
	if selected.Subport != "" {
		return selected, "", nil
	}
	newest, _, err := stubMembers(snapshot, selected.Name)
	if err != nil || newest == "" {
		return selected, "", err
	}
	return model.Target{Name: newest, Portfile: selected.Portfile, Subport: newest, Variants: selected.Variants}, selected.Name, nil
}

// stubMembers reports whether the named port is a stub whose subports carry
// its release: it builds nothing itself while sibling subports at the same
// version do, the shape of a python `py-foo` port over its `py3x-foo`
// subports. It returns the newest such subport, by natural order of the
// names, and every member. An ordinary port returns "" and nil.
func stubMembers(snapshot Snapshot, name string) (newest string, members []string, err error) {
	stub, ok := snapshot.Ports[name]
	if !ok || stub.Version == "" {
		return "", nil, nil
	}
	if only, err := stub.MetadataOnly(); err != nil || !only {
		if err != nil {
			err = fmt.Errorf("macports: can't tell whether %s builds anything: %w", name, err)
		}
		return "", nil, err
	}
	for sibling, info := range snapshot.Ports {
		if only, _ := info.MetadataOnly(); sibling == name || info.Version != stub.Version || only {
			continue
		}
		members = append(members, sibling)
	}
	if len(members) == 0 {
		return "", nil, nil
	}
	slices.SortFunc(members, naturalCompare)
	return members[len(members)-1], members, nil
}

// naturalCompare orders names with embedded numbers by their numeric value,
// so py314-foo sorts after py39-foo.
func naturalCompare(a, b string) int {
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
