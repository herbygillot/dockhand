package portindex_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/stretchr/testify/require"
)

func TestMetadataSelection(t *testing.T) {
	index, err := portindex.Open(writeIndex(t, []indexRecord{
		{"alpha", "name alpha portdir devel/alpha maintainers {{example.org:owner @contributor} openmaintainer} categories {devel net}"},
		{"alpha-child", "name alpha-child portdir devel/alpha maintainers @contributor categories net"},
		{"beta", "name beta portdir sysutils/beta maintainers {other@example.org macporter} categories sysutils"},
	}, nil))
	require.NoError(t, err)
	for _, test := range []struct {
		name   string
		filter portindex.Filter
		want   []string
	}{
		{"all", portindex.Filter{All: true}, []string{"alpha", "alpha-child", "beta"}},
		{"github", portindex.Filter{Maintainers: []string{"contributor@github"}}, []string{"alpha", "alpha-child"}},
		{"MacPorts email", portindex.Filter{Maintainers: []string{"macporter@macports.org"}}, []string{"beta"}},
		{"email", portindex.Filter{Maintainers: []string{"owner@example.org"}}, []string{"alpha"}},
		{"intersection", portindex.Filter{Maintainers: []string{"@CONTRIBUTOR"}, Categories: []string{"devel"}}, []string{"alpha"}},
		{"alternatives", portindex.Filter{Categories: []string{"net", "sysutils"}}, []string{"alpha", "alpha-child", "beta"}},
		{"exact", portindex.Filter{Maintainers: []string{"@contrib"}}, nil},
		{"none", portindex.Filter{Categories: []string{"missing"}}, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := index.Select(t.Context(), test.filter)
			require.NoError(t, err)
			require.Empty(t, result.Problems)
			var names []string
			for _, entry := range result.Entries {
				names = append(names, entry.Name)
			}
			require.Equal(t, test.want, names)
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = index.Select(ctx, portindex.Filter{Categories: []string{"net"}})
	require.ErrorIs(t, err, context.Canceled)
}

func TestSelectionExposesOmittedPortfilesAndSubports(t *testing.T) {
	root := writeIndex(t, []indexRecord{{"alpha", "name alpha portdir devel/alpha categories devel subports {alpha child}"}}, nil)
	missing := filepath.Join(root, "other", "broken", "Portfile")
	require.NoError(t, os.MkdirAll(filepath.Dir(missing), 0700))
	require.NoError(t, os.WriteFile(missing, []byte("error broken"), 0600))
	index, err := portindex.Open(root)
	require.NoError(t, err)
	result, err := index.Select(t.Context(), portindex.Filter{Categories: []string{"devel"}})
	require.NoError(t, err)
	require.Len(t, result.Entries, 1)
	require.Len(t, result.Problems, 2)
	require.Equal(t, "child", result.Problems[0].Port)
	require.Equal(t, "other/broken/Portfile", result.Problems[1].Port)
}

func TestSelectionPreservesMalformedMetadata(t *testing.T) {
	root := writeIndex(t, []indexRecord{{"broken", `name broken portdir devel/broken maintainers \{ categories devel`}, {"good", "name good portdir devel/good maintainers @contributor categories devel"}}, nil)
	index, err := portindex.Open(root)
	require.NoError(t, err)
	result, err := index.Select(t.Context(), portindex.Filter{Maintainers: []string{"@contributor"}})
	require.NoError(t, err)
	require.Len(t, result.Entries, 1)
	require.Len(t, result.Problems, 1)
	require.Equal(t, "broken", result.Problems[0].Port)
}

func TestAllSelectionRequiresAnExplicitMode(t *testing.T) {
	require.Error(t, (portindex.Filter{}).Validate())
	require.Error(t, (portindex.Filter{All: true, Categories: []string{"devel"}}).Validate())
	require.NoError(t, (portindex.Filter{All: true}).Validate())
}
