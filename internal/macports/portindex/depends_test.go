package portindex

import (
	"fmt"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// indexFrom writes a synthetic index from name -> depends-fields and
// opens it, so a dependency test states edges rather than payload text.
func indexFrom(t *testing.T, ports map[string]map[string]string) *Index {
	t.Helper()
	names := make([]string, 0, len(ports))
	for name := range ports {
		names = append(names, name)
	}
	sort.Strings(names)
	entries := make([][2]string, 0, len(names))
	for _, name := range names {
		payload := fmt.Sprintf("name %s portdir devel/%s version 1.0", name, name)
		for _, key := range dependencyKeys {
			if v := ports[name][key]; v != "" {
				payload += fmt.Sprintf(" %s {%s}", key, v)
			}
		}
		entries = append(entries, [2]string{name, payload})
	}
	root := t.TempDir()
	writeIndex(t, root, entries, true)
	ix, err := Open(root)
	require.NoError(t, err)
	return ix
}

func TestRequiresIsTheForwardLookup(t *testing.T) {
	ix := indexFrom(t, map[string]map[string]string{
		"leaf":   {},
		"middle": {DependsLib: "port:leaf"},
		"top":    {DependsLib: "port:middle path:lib/libz.dylib:zlib", DependsBuild: "port:leaf"},
	})
	got, unread, err := ix.Requires([]string{"top", "middle", "leaf"})
	require.NoError(t, err)
	assert.Empty(t, unread)
	assert.Equal(t, []string{"leaf", "middle", "zlib"}, got["top"], "sorted, lowercased, path: resolved to its port")
	assert.Equal(t, []string{"leaf"}, got["middle"])
	assert.Empty(t, got["leaf"])
	assert.Contains(t, got, "leaf", "a port with no dependencies is present and empty")
}

// "Declares nothing" and "this tree has never heard of it" are
// different answers, and a caller ordering a build cares which it got.
func TestRequiresOmitsANameTheIndexDoesNotHold(t *testing.T) {
	ix := indexFrom(t, map[string]map[string]string{"known": {}})
	got, _, err := ix.Requires([]string{"known", "stranger"})
	require.NoError(t, err)
	assert.Contains(t, got, "known")
	assert.NotContains(t, got, "stranger")
}
