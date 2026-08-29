package eval

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFetchInfo(t *testing.T) {
	e := newEvaluator(t)
	dir := portdirWith(t, `PortSystem 1.0
name fetchprobe
version 1.2.3
master_sites http://127.0.0.1:1/files
distname fetchprobe-1.2.3
checksums rmd160 0 sha256 0 size 0
`)
	fi, err := e.FetchInfo(context.Background(), dir, "", "", false)
	require.NoError(t, err)
	urls := fi.Files["fetchprobe-1.2.3.tar.gz"]
	require.NotEmpty(t, urls)
	require.Contains(t, urls[0], "http://127.0.0.1:1/files/fetchprobe-1.2.3.tar.gz")
}

func TestFetchInfoOptions(t *testing.T) {
	e := newEvaluator(t)
	dir := portdirWith(t, `PortSystem 1.0
name fetchopts
version 1.0
master_sites http://127.0.0.1:1/files
checksums rmd160 0 sha256 0 size 0
fetch.use_epsv no
fetch.ignore_sslcert yes
fetch.user_agent "Mozilla/5.0 dockhand-test"
`)
	fi, err := e.FetchInfo(context.Background(), dir, "", "", false)
	require.NoError(t, err)
	require.True(t, fi.DisableEPSV)
	require.True(t, fi.IgnoreSSLCert)
	require.Equal(t, "Mozilla/5.0 dockhand-test", fi.UserAgent)

	// Defaults: epsv on, certificates verified, no UA override.
	plain := portdirWith(t, `PortSystem 1.0
name fetchplain
version 1.0
master_sites http://127.0.0.1:1/files
checksums rmd160 0 sha256 0 size 0
`)
	fi, err = e.FetchInfo(context.Background(), plain, "", "", false)
	require.NoError(t, err)
	require.False(t, fi.DisableEPSV)
	require.False(t, fi.IgnoreSSLCert)
	require.Empty(t, fi.UserAgent)
}

func TestFetchInfoNoMirrors(t *testing.T) {
	e := newEvaluator(t)
	dir := portdirWith(t, `PortSystem 1.0
name mirrorprobe
version 1.0
master_sites http://127.0.0.1:1/files
distname mirrorprobe-1.0
checksums rmd160 0 sha256 0 size 0
`)
	withMirrors, err := e.FetchInfo(context.Background(), dir, "", "", false)
	require.NoError(t, err)
	without, err := e.FetchInfo(context.Background(), dir, "", "", true)
	require.NoError(t, err)
	urls := withMirrors.Files["mirrorprobe-1.0.tar.gz"]
	bare := without.Files["mirrorprobe-1.0.tar.gz"]
	require.Greater(t, len(urls), len(bare), "mirrors must be appended in the default mode")
	require.Len(t, bare, 1, "no-mirrors mode leaves only the port's own site")
	require.Contains(t, bare[0], "127.0.0.1")
}
