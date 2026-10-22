package distfiles

import (
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// ManifestCandidates limits source inspection to files MacPorts will extract.
// Membership is not proof of manifest ownership; the dependency reader must
// confirm the manifest at the evaluated source directory in the archive.
func ManifestCandidates(info macports.PortInfo, names []string) ([]string, error) {
	for _, key := range []string{"extract.only", "extract.rename", "worksrcdir"} {
		if info.OptionErrors[key] != "" {
			return nil, portfile.ErrUnsupported.Because("cannot evaluate %s for dependency source ownership", key)
		}
	}
	extracted, errs := syntax.ListValues(info.Options["extract.only"])
	if len(errs) > 0 {
		return nil, portfile.ErrUnsupported.Because("invalid extraction plan")
	}
	// Readers without native extraction facts cannot disambiguate multiple files.
	if _, ok := info.Options["extract.only"]; !ok {
		if len(names) != 1 {
			return nil, portfile.ErrUnsupported.Because("dependency source needs a native extraction plan")
		}
		extracted = names
	}
	var result []string
	for _, file := range extracted {
		name, _, _ := strings.Cut(file, ":")
		if !slices.Contains(names, name) {
			return nil, portfile.ErrUnsupported.Because("extracted file %s has no source archive", name)
		}
		if !slices.Contains(result, name) {
			result = append(result, name)
		}
	}
	if len(result) == 0 {
		return nil, portfile.ErrUnsupported.Because("no extracted dependency source")
	}
	rename, err := info.Bool("extract.rename")
	if err != nil {
		return nil, err
	}
	if len(result) > 1 && rename {
		return nil, portfile.ErrUnsupported.Because("multiple extracted sources with extract.rename need manual manifest ownership")
	}
	return result, nil
}
