package portindex

import (
	"cmp"
	"maps"
	"slices"

	"github.com/herbygillot/dockhand/internal/macports"
)

// MaintainerSpelling is one way the index's ports write a maintainer: the
// entry of their maintainers lines, as written, and how many Portfiles
// write it so.
type MaintainerSpelling struct {
	Maintainer macports.Maintainer
	Portfiles  int
}

// Spellings are the ways the index's ports write the maintainer whom a
// spelling names (macports.Maintainer.Names): each entry of a maintainers
// field that names them, as written, with how many Portfiles write it so,
// most first, and those alike by the entry's words. A Portfile's subports
// are its own, and it's counted once. A maintainers field that can't be
// read is passed over: what the rest write is still how they write it.
func (i *Index) Spellings(spelling string) ([]MaintainerSpelling, error) {
	written := map[string]macports.Maintainer{}
	portfiles := map[string]map[string]bool{}
	err := i.Each(func(entry Entry) bool {
		maintainers, err := macports.ReadMaintainers(entry.Fields["maintainers"])
		if err != nil {
			return true
		}
		for _, maintainer := range maintainers {
			if !maintainer.Names(spelling) {
				continue
			}
			words := maintainer.String()
			if portfiles[words] == nil {
				written[words], portfiles[words] = maintainer, map[string]bool{}
			}
			portfiles[words][entry.Portdir] = true
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	var spellings []MaintainerSpelling
	for _, words := range slices.Sorted(maps.Keys(written)) {
		spellings = append(spellings, MaintainerSpelling{Maintainer: written[words], Portfiles: len(portfiles[words])})
	}
	slices.SortStableFunc(spellings, func(a, b MaintainerSpelling) int { return cmp.Compare(b.Portfiles, a.Portfiles) })
	return spellings, nil
}
