package portfile

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
)

// The links that answer over HTTPS are switched where the Portfile writes
// them as literals, a site's tags and its place in its list kept, an
// -append's among them; one written through a variable or in quotes, or in
// another option, is left as it is.
func TestSwitchHTTPS(t *testing.T) {
	t.Parallel()
	src := `PortSystem 1.0
name                txt
homepage            http://txt.example/
description         see http://txt.example/
master_sites        http://mirror.example/ \
                    "http://txt.example/pub/:docs"
master_sites-append http://txt.example/pub/
livecheck.url       ${homepage}releases
`
	homepage := macports.PlainURL{Option: "homepage", URL: "http://txt.example/"}
	site := macports.PlainURL{Option: "master_sites", URL: "http://txt.example/pub/"}
	releases := macports.PlainURL{Option: "livecheck.url", URL: "http://txt.example/releases"}
	// A livecheck.url of the site's URL answering switches no site.
	directory := macports.PlainURL{Option: "livecheck.url", URL: "http://mirror.example/"}
	out, switched := SwitchHTTPS([]byte(src), []macports.PlainURL{homepage, site, releases, directory})
	require.Equal(t, []macports.PlainURL{homepage, site}, switched)
	require.Equal(t, `PortSystem 1.0
name                txt
homepage            https://txt.example/
description         see http://txt.example/
master_sites        http://mirror.example/ \
                    "http://txt.example/pub/:docs"
master_sites-append https://txt.example/pub/
livecheck.url       ${homepage}releases
`, string(out))

	out, switched = SwitchHTTPS([]byte(src), nil)
	require.Equal(t, src, string(out))
	require.Empty(t, switched)
}
