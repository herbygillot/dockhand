package record_test

import (
	"testing"

	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/stretchr/testify/require"
)

func TestCompareTargetsUsesSelectionSemantics(t *testing.T) {
	target := record.Target{Name: "example", Portfile: "devel/example/Portfile", Subport: "example-tools"}
	require.Zero(t, record.CompareTargets(target, target))
	require.Zero(t, record.CompareTargets(target, record.Target{Name: target.Name, Portfile: target.Portfile, Subport: target.Subport, Variants: map[string]bool{}}))

	first := target
	first.Variants = map[string]bool{"docs": false, "ssl": true}
	second := target
	second.Variants = map[string]bool{"ssl": true, "docs": false}
	require.Zero(t, record.CompareTargets(first, second))

	second.Variants["docs"] = true
	require.NotZero(t, record.CompareTargets(first, second))
	require.Equal(t, -record.CompareTargets(first, second), record.CompareTargets(second, first))
}
