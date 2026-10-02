package project

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAnRPackagesDependencyFieldsAreRead(t *testing.T) {
	description := "Package: Matrix\nVersion: 1.7-4\nDepends: R (>= 4.4),\n    methods\nImports: grDevices, graphics, grid, lattice, stats,\n\tutils\nLinkingTo:\nDescription: A rich hierarchy of sparse and dense matrix classes,\n  with Imports: in its text.\n"
	require.Equal(t, map[string]string{
		"Depends":   "R (>= 4.4), methods",
		"Imports":   "grDevices, graphics, grid, lattice, stats, utils",
		"LinkingTo": "",
	}, RDependencies([]byte(description)))
}
