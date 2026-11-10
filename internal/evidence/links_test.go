package evidence

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
)

// A passed build's links from a port it doesn't declare in depends_lib are
// a finding a port, counting its libraries; a declared port, itself, its
// siblings, and a library no port provides are none; a build whose links
// weren't read says so, unless another environment's were. Each build is
// judged by its own environment's declarations (C8-2).
func TestUndeclaredLinks(t *testing.T) {
	link := func(binary, library, port string) model.Link {
		return model.Link{Binary: "/opt/local/" + binary, Library: "/opt/local/lib/" + library, Port: port}
	}
	arm, intel := model.Environment{Provider: "tart", Platform: model.Platform{Architecture: "arm64"}}, model.Environment{Provider: "tart", Platform: model.Platform{Architecture: "x86_64"}}
	declared := []string{"zlib", "harbor", "harbor-common"}
	builds := []LinkedBuild{
		{Result: model.TargetResult{Target: "harbor", Outcome: model.OutcomePassed, Links: []model.Link{
			link("bin/harbor", "libonig.5.dylib", "oniguruma"), link("bin/harbor-x", "libonig.5.dylib", "oniguruma"), link("bin/harbor", "libonig-extra.1.dylib", "Oniguruma"),
			link("bin/harbor", "libz.1.dylib", "zlib"), link("bin/harbor", "libharbor.1.dylib", "harbor"), link("bin/harbor", "libshared.dylib", "harbor-common"),
			link("bin/harbor", "libbundled.dylib", ""),
		}}, Environment: arm, Declared: declared},
		// x86_64 declares oniguruma, which hides nothing on arm64.
		{Result: model.TargetResult{Target: "harbor", Outcome: model.OutcomePassed, Links: []model.Link{link("bin/harbor", "libonig.5.dylib", "oniguruma")}},
			Environment: intel, Declared: append(slices.Clone(declared), "oniguruma")},
		{Result: model.TargetResult{Target: "harbor", Outcome: model.OutcomePassed}, Environment: intel, Declared: declared},
		{Result: model.TargetResult{Target: "harbor-cli", Outcome: model.OutcomePassed}, Environment: arm},
		{Result: model.TargetResult{Target: "broken", Outcome: model.OutcomeFailed}, Environment: arm},
	}
	found := UndeclaredLinks(builds)
	require.Equal(t, []LinkFinding{
		{Target: "harbor", Port: "oniguruma", Library: "/opt/local/lib/libonig.5.dylib", Binary: "/opt/local/bin/harbor", Libraries: 2, Environments: []model.Environment{arm}},
		{Target: "harbor-cli", Unread: true},
	}, found)
	require.Equal(t, "harbor links oniguruma", found[0].Subject())
	require.Equal(t, []LinkFinding{{Target: "harbor", Clean: true}}, UndeclaredLinks([]LinkedBuild{{Result: model.TargetResult{Target: "harbor", Outcome: model.OutcomePassed, Links: []model.Link{}}}}), "nothing links, read")
}

// A target whose read builds link only ports it declares is said, the
// ports counted once across builds, so a clean scan is seen to have run
// (field testing's crystal 1.22.1); a failed build is none.
func TestACleanLinkScanIsSaid(t *testing.T) {
	link := func(library, port string) model.Link {
		return model.Link{Binary: "/opt/local/bin/crystal", Library: "/opt/local/lib/" + library, Port: port}
	}
	declared := []string{"crystal", "boehmgc", "libffi", "libiconv", "llvm-23", "pcre2"}
	links := []model.Link{link("libLLVM.23.1.dylib", "llvm-23"), link("libpcre2-8.0.dylib", "pcre2"), link("libgc.1.dylib", "boehmgc"), link("libiconv.2.dylib", "libiconv"), link("libffi.8.dylib", "libffi")}
	monterey, tahoe := model.Environment{Provider: "tart", Platform: model.Platform{Version: "21"}}, model.Environment{Provider: "tart", Platform: model.Platform{Version: "25"}}
	found := UndeclaredLinks([]LinkedBuild{
		{Result: model.TargetResult{Target: "crystal", Outcome: model.OutcomePassed, Tests: model.TestsFailed, Links: links}, Environment: monterey, Declared: declared},
		{Result: model.TargetResult{Target: "crystal", Outcome: model.OutcomePassed, Tests: model.TestsFailed, Links: links[:2]}, Environment: tahoe, Declared: declared},
		{Result: model.TargetResult{Target: "shards", Outcome: model.OutcomeFailed}, Environment: tahoe},
	})
	require.Equal(t, []LinkFinding{{Target: "crystal", Clean: true, Ports: []string{"boehmgc", "libffi", "libiconv", "llvm-23", "pcre2"}}}, found)
}

// A target's own port and its Portfile's other subports aren't another
// port's libraries: ace's notice counted ace's own, and tao's its
// subports' (the ace and tao updates, 2026-10-28).
func TestALinkNoticeLeavesOutTheTargetsOwnPorts(t *testing.T) {
	link := func(library, port string) model.Link {
		return model.Link{Binary: "/opt/local/bin/tao_idl", Library: "/opt/local/lib/" + library, Port: port}
	}
	own := []string{"ace", "tao"}
	found := UndeclaredLinks([]LinkedBuild{{Result: model.TargetResult{Target: "ace", Outcome: model.OutcomePassed, Links: []model.Link{link("libACE.dylib", "ace")}}, Declared: own, Own: own}})
	require.Equal(t, []LinkFinding{{Target: "ace", Clean: true}}, found, "it links no other port's")
	found = UndeclaredLinks([]LinkedBuild{{Result: model.TargetResult{Target: "tao", Outcome: model.OutcomePassed,
		Links: []model.Link{link("libACE.dylib", "ace"), link("libssl.3.dylib", "openssl3"), link("libz.1.dylib", "zlib")}}, Declared: append(own, "openssl3", "zlib"), Own: own}})
	require.Equal(t, []LinkFinding{{Target: "tao", Clean: true, Ports: []string{"openssl3", "zlib"}}}, found, "both declared ports named")
}

// An unread target carries the reason its first unread build gave.
func TestAnUnreadTargetCarriesItsReason(t *testing.T) {
	t.Parallel()
	found := UndeclaredLinks([]LinkedBuild{
		{Result: model.TargetResult{Target: "bazel", Outcome: model.OutcomePassed}, Unread: "its result is check-610's, which didn't read them"},
		{Result: model.TargetResult{Target: "bazel", Outcome: model.OutcomePassed}, Unread: "the guest couldn't read them, as its log says"},
	})
	require.Equal(t, []LinkFinding{{Target: "bazel", Unread: true, Reason: "its result is check-610's, which didn't read them"}}, found)
}

// A port the Portfile's own text declares in depends_lib that installs a
// library none of the target's builds links, where each build installed a
// Mach-O file, is said, once, with its line (unlinked-dependency); one a
// PortGroup adds, a port of programs alone, a build installing no Mach-O,
// one linked on another release, and a target with an unread build are
// none (the abook session's libiconv).
func TestADeclaredLibraryNothingLinksIsSaid(t *testing.T) {
	arm, old := model.Environment{Provider: "tart", Platform: model.Platform{Version: "25", Architecture: "arm64"}}, model.Environment{Provider: "tart", Platform: model.Platform{Version: "21", Architecture: "arm64"}}
	iconv := model.DeclaredPort{Port: "libiconv", Libraries: []string{"/opt/local/lib/libiconv.2.dylib"}}
	ncurses := model.DeclaredPort{Port: "ncurses", Libraries: []string{"/opt/local/lib/libncursesw.6.dylib"}}
	git := model.DeclaredPort{Port: "git", Libraries: []string{}}
	python := model.DeclaredPort{Port: "python313", Libraries: []string{"/opt/local/lib/libpython3.13.dylib"}}
	written := []WrittenLib{{Port: "libiconv", Option: "depends_lib-append", Line: 23}, {Port: "ncurses", Option: "depends_lib-append", Line: 23}, {Port: "git", Option: "depends_lib-append", Line: 24}}
	ncursesLink := model.Link{Binary: "/opt/local/bin/abook", Library: "/opt/local/lib/libncursesw.6.dylib", Port: "ncurses"}
	build := func(environment model.Environment, machos int, links ...model.Link) LinkedBuild {
		return LinkedBuild{Result: model.TargetResult{Target: "abook", Outcome: model.OutcomePassed, Links: append([]model.Link{}, links...),
			Declared: &model.Declared{Ports: []model.DeclaredPort{iconv, ncurses, git, python}, MachOs: machos}}, Environment: environment, Declared: []string{"abook", "libiconv", "ncurses", "git", "python313"}, Written: written}
	}
	unlinked := func(builds ...LinkedBuild) []LinkFinding {
		var found []LinkFinding
		for _, finding := range UndeclaredLinks(builds) {
			if finding.Unlinked {
				found = append(found, finding)
			}
		}
		return found
	}
	require.Equal(t, []LinkFinding{{Target: "abook", Port: "libiconv", Unlinked: true, Option: "depends_lib-append", Line: 23}}, unlinked(build(arm, 1, ncursesLink), build(old, 1, ncursesLink)),
		"git installs no library, python313 is the PortGroup's, ncurses is linked")
	require.Empty(t, unlinked(build(arm, 0)), "a build installing no Mach-O file says nothing")
	linkedOld := model.Link{Binary: "/opt/local/bin/abook", Library: "/opt/local/lib/libiconv.2.dylib", Port: "libiconv"}
	require.Empty(t, unlinked(build(arm, 1, ncursesLink), build(old, 1, ncursesLink, linkedOld)), "linked on one release is linked")
	unread := build(old, 1)
	unread.Result.Declared = nil
	require.Empty(t, unlinked(build(arm, 1, ncursesLink), unread), "a build whose declared ports weren't read leaves it unsaid")
	all := UndeclaredLinks([]LinkedBuild{build(arm, 1, ncursesLink)})
	require.Equal(t, []LinkFinding{{Target: "abook", Clean: true, Ports: []string{"ncurses"}}, {Target: "abook", Port: "libiconv", Unlinked: true, Option: "depends_lib-append", Line: 23}}, all, "beside the clean scan, after it")
}

// An undeclared link whose library a declared port's pkg-config names, in
// its own module's Libs or a module its Requires: reaches, says the port
// and the modules followed (gnome-voice's note 9).
func TestAnUndeclaredLinkSaysThePkgConfigThatBroughtItIn(t *testing.T) {
	cairo := model.Link{Binary: "/opt/local/bin/gnome-voice", Library: "/opt/local/lib/libcairo.2.dylib", Port: "cairo"}
	gtk := model.DeclaredPort{Port: "gtk4", Libraries: []string{"/opt/local/lib/libgtk-4.1.dylib"}, Modules: []model.PkgConfigModule{{Name: "gtk4", Libs: []string{"gtk-4"}, Requires: []string{"pango", "cairo"}}}}
	cairoPort := model.DeclaredPort{Port: "glib2", Modules: []model.PkgConfigModule{{Name: "cairo", Libs: []string{"cairo"}}}}
	build := LinkedBuild{Result: model.TargetResult{Target: "gnome-voice", Outcome: model.OutcomePassed, Links: []model.Link{cairo},
		Declared: &model.Declared{Ports: []model.DeclaredPort{gtk, cairoPort}, MachOs: 1}}, Declared: []string{"gnome-voice", "gtk4", "glib2"}}
	found := UndeclaredLinks([]LinkedBuild{build})
	require.Len(t, found, 1)
	require.Equal(t, &LinkThrough{Port: "gtk4", Modules: []string{"gtk4", "cairo"}}, found[0].Through)
	build.Result.Declared.Ports = []model.DeclaredPort{{Port: "gtk4", Modules: []model.PkgConfigModule{{Name: "gtk4", Libs: []string{"gtk-4"}, Requires: []string{"pango"}}}}}
	require.Nil(t, UndeclaredLinks([]LinkedBuild{build})[0].Through, "a Requires: no declared port installs stops there")
}

// A finding's "through" is read from the builds that made it, not
// another environment's: an arm64 finding took an x86_64 build's
// pkg-config path to cairo (Codex's sixth review, #77, its probe).
func TestAFindingsThroughIsItsOwnBuilds(t *testing.T) {
	t.Parallel()
	arm := model.Environment{Provider: "command", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}}
	intel := arm
	intel.Platform.Architecture = "x86_64"
	link := model.Link{Binary: "/opt/local/bin/tool", Library: "/opt/local/lib/libcairo.2.dylib", Port: "cairo"}
	builds := []LinkedBuild{
		{Environment: arm, Declared: []string{"tool", "gtk4"}, Result: model.TargetResult{Target: "tool", Outcome: model.OutcomePassed, Links: []model.Link{link}, Declared: &model.Declared{MachOs: 1, Ports: []model.DeclaredPort{{Port: "gtk4", Modules: []model.PkgConfigModule{{Name: "gtk4", Libs: []string{"gtk-4"}}}}}}}},
		{Environment: intel, Declared: []string{"tool", "gtk4", "cairo"}, Result: model.TargetResult{Target: "tool", Outcome: model.OutcomePassed, Links: []model.Link{link}, Declared: &model.Declared{MachOs: 1, Ports: []model.DeclaredPort{{Port: "gtk4", Modules: []model.PkgConfigModule{{Name: "gtk4", Libs: []string{"cairo"}}}}}}}},
	}
	found := UndeclaredLinks(builds)
	require.Len(t, found, 1)
	require.Equal(t, []model.Environment{arm}, found[0].Environments)
	require.Nil(t, found[0].Through, "the arm64 pkg-config modules contain no cairo linkage")
}
