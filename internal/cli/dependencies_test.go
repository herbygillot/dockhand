package cli

import (
	"go/build"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The CLI composes commands from the application and reports in the
// projection's words. A verification provider's facts reach it through
// app; it does not import a provider.
func TestCommandPackageDependencies(t *testing.T) {
	t.Parallel()
	pkg, err := build.Default.ImportDir(".", 0)
	require.NoError(t, err)
	const prefix = "github.com/herbygillot/dockhand/internal/"
	allowed := map[string]bool{"app": true, "assess": true, "credential": true, "git": true, "github": true, "macos": true, "macports": true, "macports/portedit": true, "macports/version": true, "outdated": true, "progress": true, "publish": true, "record": true, "state": true, "tui": true, "upstream": true, "verify": true, "version": true, "workflow": true, "workflow/view": true}
	for _, path := range pkg.Imports {
		if strings.HasPrefix(path, "github.com/herbygillot/dockhand/") {
			require.True(t, strings.HasPrefix(path, prefix) && allowed[strings.TrimPrefix(path, prefix)], "cli must not import a verification provider or another concrete integration: %s", path)
		}
	}
}
