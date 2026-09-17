package portindex_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/stretchr/testify/require"
)

type indexRecord struct {
	name    string
	payload string
}

func writeIndex(t *testing.T, records []indexRecord, quick map[string]int64) string {
	t.Helper()
	root := t.TempDir()
	var data strings.Builder
	for _, record := range records {
		payload := record.payload + "\n"
		fmt.Fprintf(&data, "%s %d\n%s", record.name, len(utf16.Encode([]rune(payload))), payload)
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, "PortIndex"), []byte(data.String()), 0o600))
	if quick != nil {
		var value strings.Builder
		for _, name := range []string{"alpha", "beta"} {
			if offset, ok := quick[name]; ok {
				fmt.Fprintf(&value, "%s %d\n", name, offset)
			}
		}
		require.NoError(t, os.WriteFile(filepath.Join(root, "PortIndex.quick"), []byte(value.String()), 0o600))
	}
	return root
}

func TestLookupRepairsStaleQuickIndex(t *testing.T) {
	t.Parallel()
	records := []indexRecord{
		{"alpha", "name alpha portdir devel/alpha description {first}"},
		{"beta", "name beta portdir devel/beta description {second}"},
	}
	index, err := portindex.Open(writeIndex(t, records, map[string]int64{"alpha": 0, "beta": 0}))
	require.NoError(t, err)
	require.Equal(t, 2, index.Len())

	entry, err := index.Lookup("BETA")
	require.NoError(t, err)
	require.Equal(t, "beta", entry.Name)
	require.Equal(t, "devel/beta", entry.Portdir)
	require.Equal(t, "second", entry.Fields["description"])
}

func TestSequentialReadUsesTclStringLength(t *testing.T) {
	t.Parallel()
	index, err := portindex.Open(writeIndex(t, []indexRecord{
		{"accent", "name accent portdir textproc/accent description {café 😀}"},
		{"next", "name next portdir textproc/next description {still aligned}"},
	}, nil))
	require.NoError(t, err)

	var names []string
	require.NoError(t, index.Each(func(entry portindex.Entry) bool {
		names = append(names, entry.Name)
		return true
	}))
	require.Equal(t, []string{"accent", "next"}, names)
}

func TestReverseDependenciesAndTransitiveClosure(t *testing.T) {
	t.Parallel()
	index, err := portindex.Open(writeIndex(t, []indexRecord{
		{"core", "name core portdir devel/core depends_run port:runtime"},
		{"runtime", "name runtime portdir devel/runtime"},
		{"tool", "name tool portdir devel/tool"},
		{"fetcher", "name fetcher portdir devel/fetcher"},
		{"builder", "name builder portdir devel/builder depends_build path:/opt/local/bin/tool:tool"},
		{"consumer", "name consumer portdir apps/consumer depends_lib port:Core depends_fetch {port:fetcher port:absent}"},
	}, nil))
	require.NoError(t, err)

	reverse, err := index.ReverseDependencies()
	require.NoError(t, err)
	require.Empty(t, reverse.Unread)
	require.Equal(t, "consumer", reverse.ByPort["core"][0].Name)
	require.Equal(t, []string{portindex.DependsLib}, reverse.ByPort["core"][0].Fields)
	require.Equal(t, []string{"core"}, reverse.ByPort["core"][0].Requires)
	require.Equal(t, "builder", reverse.ByPort["tool"][0].Name)
	require.Equal(t, []string{portindex.DependsBuild}, reverse.ByPort["tool"][0].Fields)
	require.Equal(t, "core", reverse.ByPort["runtime"][0].Name)

	closure, err := index.DependencyClosure([]string{"consumer"})
	require.NoError(t, err)
	require.Equal(t, []string{"absent", "core", "fetcher", "runtime"}, closure.Dependencies)
	require.Equal(t, []string{"absent"}, closure.Missing)
	require.Empty(t, closure.Unread)
}
