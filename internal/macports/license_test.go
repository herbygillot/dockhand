package macports

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A license expression, as a forge detects one or a manifest declares one,
// is said in MacPorts' words, as the Guide writes them: a choice braced,
// licenses that all apply side by side, versions without ".0", and a work
// in the public domain by CC0 or the Unlicense public-domain, as the tree
// says it. What it can't say is no answer, never a guess.
func TestALicenseExpressionInMacPortsWords(t *testing.T) {
	t.Parallel()
	for expression, want := range map[string]string{
		"MIT":                             "MIT",
		"Apache-2.0":                      "Apache-2",
		"MIT OR Apache-2.0":               "{MIT Apache-2}",
		"MIT/Apache-2.0":                  "{MIT Apache-2}",
		"Apache-2.0 or MIT":               "{Apache-2 MIT}",
		"MIT AND Zlib":                    "MIT zlib",
		"(MIT OR Apache-2.0) AND BSL-1.0": "{MIT Apache-2} Boost-1",
		"BSD-3-Clause AND (MIT OR Zlib)":  "BSD {MIT zlib}",
		"GPL-3.0-or-later":                "GPL-3+",
		"CC0-1.0":                         "public-domain",
		"EUPL-1.2":                        "EUPL-1.2",
		"Unlicense OR MIT":                "{public-domain MIT}",
		"BSD-2-Clause OR BSD-3-Clause":    "BSD",
		"(MIT)":                           "MIT",
		"((MIT))":                         "MIT",
		"(MIT) OR (ISC)":                  "{MIT ISC}",
		"(MIT OR ISC) AND (Zlib)":         "{MIT ISC} zlib",
	} {
		got, ok := License(expression)
		require.True(t, ok, expression)
		require.Equal(t, want, got, expression)
	}
	for _, expression := range []string{"", "NOASSERTION", "Unicode-3.0", "MIT OR Unicode-3.0", "Apache-2.0 WITH LLVM-exception",
		"(MIT AND Zlib) OR ISC", "MIT AND", "(MIT OR ISC"} {
		_, ok := License(expression)
		require.False(t, ok, expression)
	}
}

// A license line reads for a person as MacPorts means it: a braced choice
// with "or", licenses that all apply with "and".
func TestALicenseLineInWords(t *testing.T) {
	t.Parallel()
	for line, want := range map[string]string{
		"MIT":                    "MIT",
		"{MIT Apache-2}":         "MIT or Apache-2",
		"MIT zlib":               "MIT and zlib",
		"{MIT Apache-2} Boost-1": "(MIT or Apache-2) and Boost-1",
		"{MIT":                   "{MIT",
	} {
		require.Equal(t, want, LicenseWords(line), line)
	}
}

// A port's description is its words, as the evaluation lists them.
func TestAPortsDescription(t *testing.T) {
	t.Parallel()
	require.Equal(t, "A fast, intuitive terminal text editor", PortInfo{Options: map[string]string{"description": "{A fast, intuitive terminal text editor}"}}.Description())
	require.Equal(t, "Harbor tools", PortInfo{Options: map[string]string{"description": "Harbor tools"}}.Description())
	require.Empty(t, PortInfo{}.Description())
}

// A license line names another's licenses where it has each, as one that
// applies or among a choice: zola's "EUPL-1.2 MIT" names Cargo.toml's
// EUPL-1.2, and master's "MIT" didn't.
func TestALicenseLineNamesAnothersLicenses(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		line, other string
		names       bool
	}{
		{"EUPL-1.2 MIT", "EUPL-1.2", true},
		{"MIT", "EUPL-1.2", false},
		{"{MIT Apache-2}", "{MIT Apache-2}", true},
		{"{MIT Apache-2}", "MIT", true},
		{"MIT", "{MIT Apache-2}", false},
		{"GPL-2+", "GPL-2", false},
		{"MIT", "", false},
		{"{MIT", "MIT", false},
	} {
		require.Equal(t, test.names, LicenseNames(test.line, test.other), "%q names %q", test.line, test.other)
	}
}
