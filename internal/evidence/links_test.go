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
	require.Empty(t, UndeclaredLinks([]LinkedBuild{{Result: model.TargetResult{Target: "harbor", Outcome: model.OutcomePassed, Links: []model.Link{}}}}), "nothing links")
}
