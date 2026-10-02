package project

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// vectors are PyPA packaging's own test cases for PEP 440 and PEP 508, the
// specifications' reference implementation, as testdata/packaging's
// extract.py takes them from its tests (README.md says which release).
type vectors struct {
	Versions struct {
		Ordered []string `json:"ordered"`
		Invalid []string `json:"invalid"`
	} `json:"versions"`
	Specifiers struct {
		Contains [][3]any `json:"contains"`
	} `json:"specifiers"`
	Markers struct {
		Valid     []string `json:"valid"`
		Invalid   []string `json:"invalid"`
		Evaluates [][3]any `json:"evaluates"`
	} `json:"markers"`
}

func readVectors(t *testing.T) vectors {
	t.Helper()
	data, err := os.ReadFile("testdata/packaging/vectors.json")
	require.NoError(t, err)
	var found vectors
	require.NoError(t, json.Unmarshal(data, &found))
	return found
}

// Versions are read and ordered as packaging reads and orders them, but
// for local labels, which dockhand sets aside in ordering: what's asked
// is whether an installed version meets a requirement, and a local label
// is a build of the same public version.
func TestPEP440VersionsAsPackagingReadsThem(t *testing.T) {
	v := readVectors(t)
	parsed := make([]version, len(v.Versions.Ordered))
	for i, text := range v.Versions.Ordered {
		var err error
		parsed[i], err = parseVersion(text)
		require.NoError(t, err, text)
	}
	public := func(text string) string {
		before, _, _ := strings.Cut(text, "+")
		return before
	}
	for i := range parsed {
		for j := i + 1; j < len(parsed); j++ {
			c := parsed[i].compare(parsed[j])
			if public(v.Versions.Ordered[i]) == public(v.Versions.Ordered[j]) {
				require.Zero(t, c, "%s and %s differ only in their local labels", v.Versions.Ordered[i], v.Versions.Ordered[j])
				continue
			}
			require.Negative(t, c, "%s < %s", v.Versions.Ordered[i], v.Versions.Ordered[j])
		}
	}
	for _, text := range v.Versions.Invalid {
		_, err := parseVersion(text)
		require.Error(t, err, "%q", text)
	}
}

// A specifier admits what packaging's admits, pre-releases included, as
// it's asked with prereleases=True: an installed version meets a
// requirement wherever its version does.
func TestPEP440SpecifiersAsPackagingReadsThem(t *testing.T) {
	for _, c := range readVectors(t).Specifiers.Contains {
		installed, specifier, want := c[0].(string), c[1].(string), c[2].(bool)
		admits, err := Admits(specifier, installed)
		require.NoError(t, err, "%s %s", installed, specifier)
		assert.Equal(t, want, admits, "%s %s", installed, specifier)
	}
}

// Markers parse, and evaluate, as packaging's do.
func TestPEP508MarkersAsPackagingReadsThem(t *testing.T) {
	v := readVectors(t)
	for _, marker := range v.Markers.Valid {
		_, err := Evaluate(marker, map[string]string{})
		require.NoError(t, err, marker)
	}
	for _, marker := range v.Markers.Invalid {
		_, err := Evaluate(marker, map[string]string{})
		require.Error(t, err, marker)
	}
	for _, c := range v.Markers.Evaluates {
		marker, want := c[0].(string), c[2].(bool)
		environment := map[string]string{}
		for name, value := range c[1].(map[string]any) {
			environment[name] = value.(string)
		}
		applies, err := Evaluate(marker, environment)
		require.NoError(t, err, marker)
		expected := No
		if want {
			expected = Yes
		}
		assert.Equal(t, expected, applies, "%s in %v", marker, environment)
	}
}
