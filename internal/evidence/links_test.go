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
