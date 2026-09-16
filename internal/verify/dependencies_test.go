package verify

import (
	"go/build"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContractPackageDependencies(t *testing.T) {
	pkg, err := build.Default.ImportDir(".", 0)
	require.NoError(t, err)
	allowed := map[string]bool{
		"github.com/herbygillot/dockhand/internal/record": true,
		"github.com/herbygillot/dockhand/internal/git":    true,
	}
	for _, path := range pkg.Imports {
		if strings.Contains(strings.Split(path, "/")[0], ".") {
			require.True(t, allowed[path], "verify contracts must not import native discovery or concrete integrations: %s", path)
		}
	}
}
