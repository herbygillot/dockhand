package macports_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
)

// The ports tree's layout is macports' to say: which top-level directories
// are categories, which port directory a path lies in, and where Portfiles
// and PortGroups are. (The private-helper review of 2026-09-28, finding 7,
// and the code-organization review's finding 29.)
func TestThePortsTreesLayout(t *testing.T) {
	for name, category := range map[string]bool{"devel": true, "R": true, "_resources": false, ".github": false, "": false} {
		require.Equal(t, category, macports.IsCategory(name), name)
	}
	for path, directory := range map[string]string{
		"devel/jq/Portfile":                       "devel/jq",
		"devel/jq/files/patch-fix.diff":           "devel/jq",
		"devel/.jq/Portfile":                      "devel/.jq",
		"_resources/port1.0/group/golang-1.0.tcl": "",
		".github/workflows/main.yml":              "",
		"devel/README":                            "",
		"README.md":                               "",
		"devel/jq":                                "",
		"devel//Portfile":                         "",
	} {
		got, ok := macports.PortDirectoryOf(path)
		require.Equal(t, directory, got, path)
		require.Equal(t, directory != "", ok, path)
	}
	for path, portfile := range map[string]bool{
		"devel/jq/Portfile":       true,
		"devel/jq/files/Portfile": false,
		"devel/jq/Portfile/":      false,
		"_resources/jq/Portfile":  false,
		"devel/../Portfile":       false,
		"devel/j\\q/Portfile":     false,
		"devel/jq/portfile":       false,
	} {
		require.Equal(t, portfile, macports.ValidPortfilePath(path), path)
	}
	require.Equal(t, "_resources/port1.0/group/golang-1.0.tcl", macports.PortGroup{Name: "golang", Version: "1.0"}.Path())
	for path, group := range map[string]macports.PortGroup{
		"_resources/port1.0/group/golang-1.0.tcl":                      {Name: "golang", Version: "1.0"},
		"_resources/port1.0/group/compiler_blacklist_versions-1.0.tcl": {Name: "compiler_blacklist_versions", Version: "1.0"},
		"_resources/port1.0/group/app-bundle-2.tcl":                    {Name: "app-bundle", Version: "2"},
		"_resources/port1.0/group/golang.tcl":                          {},
		"_resources/port1.0/group/golang-.tcl":                         {},
		"_resources/port1.0/group/-1.0.tcl":                            {},
		"_resources/port1.0/group/golang-v1.tcl":                       {},
		"_resources/port1.0/group/sub/golang-1.0.tcl":                  {},
		"_resources/port1.0/group/golang-1.0.tcl~":                     {},
		"_resources/port1.0/compilers/clang-1.0.tcl":                   {},
		"devel/jq/Portfile":                                            {},
	} {
		got, ok := macports.PortGroupAt(path)
		require.Equal(t, group, got, path)
		require.Equal(t, group != macports.PortGroup{}, ok, path)
		if ok {
			require.Equal(t, path, got.Path(), "a PortGroup's path reads back")
		}
	}
}

// A new port's category is a directory a category can be: not one the
// tree keeps for itself, and one directory in a port name's characters
// (the helper-ownership review's table).
func TestACategoryANewPortCanGoIn(t *testing.T) {
	for _, name := range []string{"devel", "python", "x11", "sysutils"} {
		require.True(t, macports.ValidCategory(name), name)
	}
	for _, name := range []string{"", "_resources", ".github", "a/b", "net work", "..", "."} {
		require.False(t, macports.ValidCategory(name), name)
	}
}
