package outdated

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/upstream"
)

// A subport whose source convention discovery doesn't take is checked
// with a sibling of the same Portfile and release that was, and says so:
// py310-cbor2 beside py-cbor2, whose livecheck the python PortGroup turns
// off (batch 30). One of another release, another Portfile, or another
// problem stays as it is.
func TestASubportIsCheckedWithItsSibling(t *testing.T) {
	unsupported := func(selector, portfile, version string) Port {
		return Port{Selector: selector, Result: upstream.Result{CurrentVersion: version, Assessment: upstream.Unknown, Detail: "upstream: automatic selection does not support this source convention"},
			portfile: portfile, unsupported: true}
	}
	checked := Port{Selector: "py-cbor2", Result: upstream.Result{CurrentVersion: "5.7.1", CandidateVersion: "6.1.5", Assessment: upstream.UpdateAvailable, Detail: "Selected 6.1.5 from livecheck"},
		portfile: "python/py-cbor2/Portfile"}
	broken := unsupported("py311-cbor2", "python/py-cbor2/Portfile", "5.7.1")
	broken.unsupported = false
	ports := WithSiblings([]Port{
		unsupported("py310-cbor2", "python/py-cbor2/Portfile", "5.7.1"),
		checked,
		broken,
		unsupported("kubectl_select", "sysutils/kubectl/Portfile", "0.0.0"),
		unsupported("py310-other", "python/py-other/Portfile", "5.7.1"),
	})
	require.Equal(t, "py-cbor2", ports[0].With)
	require.Equal(t, upstream.UpdateAvailable, ports[0].Assessment)
	require.Equal(t, "6.1.5", ports[0].CandidateVersion)
	require.Equal(t, "Selected 6.1.5 from livecheck", ports[0].Detail, "a checked port's detail says how, and is no problem")
	require.Equal(t, "py310-cbor2", ports[0].Selector)
	for _, port := range ports[2:] {
		require.Empty(t, port.With, port.Selector)
		require.Equal(t, upstream.Unknown, port.Assessment, port.Selector)
	}
}
