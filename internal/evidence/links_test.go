package evidence

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
)

// A passed build's links from a port it doesn't declare in depends_lib are
// a finding a port, counting its libraries; a declared port, itself, its
// siblings, and a library no port provides are none; a build whose links
// weren't read says so, unless another environment's were.
func TestUndeclaredLinks(t *testing.T) {
	link := func(binary, library, port string) model.Link {
		return model.Link{Binary: "/opt/local/" + binary, Library: "/opt/local/lib/" + library, Port: port}
	}
	results := []model.TargetResult{
		{Target: "harbor", Outcome: model.OutcomePassed, Links: []model.Link{
			link("bin/harbor", "libonig.5.dylib", "oniguruma"), link("bin/harbor-x", "libonig.5.dylib", "oniguruma"), link("bin/harbor", "libonig-extra.1.dylib", "Oniguruma"),
			link("bin/harbor", "libz.1.dylib", "zlib"), link("bin/harbor", "libharbor.1.dylib", "harbor"), link("bin/harbor", "libshared.dylib", "harbor-common"),
			link("bin/harbor", "libbundled.dylib", ""),
		}},
		{Target: "harbor", Outcome: model.OutcomePassed},
		{Target: "harbor-cli", Outcome: model.OutcomePassed},
		{Target: "broken", Outcome: model.OutcomeFailed},
	}
	declared := map[model.TargetID][]string{"harbor": {"zlib", "harbor", "harbor-common"}}
	require.Equal(t, []LinkFinding{
		{Target: "harbor", Port: "oniguruma", Library: "/opt/local/lib/libonig.5.dylib", Binary: "/opt/local/bin/harbor", Libraries: 2},
		{Target: "harbor-cli", Unread: true},
	}, UndeclaredLinks(results, declared))
	require.Equal(t, "harbor links oniguruma", UndeclaredLinks(results, declared)[0].Subject())
	require.Empty(t, UndeclaredLinks([]model.TargetResult{{Target: "harbor", Outcome: model.OutcomePassed, Links: []model.Link{}}}, nil), "nothing links")
}
