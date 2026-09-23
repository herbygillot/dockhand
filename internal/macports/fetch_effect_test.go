package macports_test

import (
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/stretchr/testify/require"
)

func TestAffectsFetchNamesTheFetchPhasesInputs(t *testing.T) {
	t.Parallel()
	for _, option := range []string{"distfiles", "distfiles-append", "master_sites-delete", "checksums", "patchfiles", "fetch.type", "fetch.ignore_sslcert", "use_xz", "github.setup", "git.branch", "version", "distname", "worksrcdir", "extract.suffix", "go.vendors", "cargo.crates"} {
		require.True(t, macports.AffectsFetch(option), option)
	}
	for _, option := range []string{"configure.env", "configure.env-append", "build.env-append", "java.home", "java.fallback", "depends_lib", "depends_lib-append", "notes-append", "supported_archs", "known_fail", "test.run", "revision", "long_description", "homepage", "livecheck.regex", "default_variants"} {
		require.False(t, macports.AffectsFetch(option), option)
	}
}
