package outdated_test

import (
	"go/build"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCapabilityDependencies(t *testing.T) {
	pkg, err := build.Default.ImportDir(".", 0)
	require.NoError(t, err)
	const prefix = "github.com/herbygillot/dockhand/internal/"
	allowed := map[string]bool{
		"git": true, "macports": true, "macports/portedit": true,
		"macports/portindex": true, "macports/survey": true, "progress": true, "record": true, "upstream": true,
	}
	for _, path := range pkg.Imports {
		if strings.Contains(strings.Split(path, "/")[0], ".") {
			require.True(t, strings.HasPrefix(path, prefix) && allowed[strings.TrimPrefix(path, prefix)],
				"outdated must receive its integrations without importing app, workflow state, or concrete forge clients: %s", path)
		}
	}
}
