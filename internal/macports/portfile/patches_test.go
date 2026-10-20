package portfile

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A merged patch comes out of the Portfile's literal patchfiles, the line
// with it where it was the only patch; where it's written otherwise, the
// Portfile is left for a person.
func TestDropPatchTakesAMergedPatchOut(t *testing.T) {
	for src, want := range map[string]string{
		"name x\npatchfiles          fix.diff\nversion 1\n":                                    "name x\nversion 1\n",
		"name x\npatchfiles          fix.diff \\\n                    other.diff\n":            "name x\npatchfiles \\\n                    other.diff\n",
		"name x\npatchfiles          other.diff \\\n                    fix.diff\nversion 1\n": "name x\npatchfiles          other.diff\nversion 1\n",
		"name x\npatchfiles-append   fix.diff\n":                                               "name x\n",
	} {
		got, ok := DropPatch([]byte(src), "fix.diff")
		require.True(t, ok, src)
		require.Equal(t, want, string(got), src)
	}
	for _, src := range []string{
		"name x\nvariant y { patchfiles fix.diff }\n",
		"name x\nif {$a} { patchfiles fix.diff }\n",
		"name x\npatchfiles ${p}.diff\n",
		"name x\npatchfiles fix.diff\npatchfiles-append fix.diff\n",
	} {
		got, ok := DropPatch([]byte(src), "fix.diff")
		require.False(t, ok, src)
		require.Equal(t, src, string(got))
	}
}

// Every literal patchfiles name counts, in a variant's or a platform's body
// too, each once; a computed one isn't a name the text gives.
func TestPatchfileNamesReadsEveryLiteralName(t *testing.T) {
	src := []byte(`name net-snmp
patchfiles patch-a.diff \
           patch-b.diff
patchfiles-append ${name}-c.diff
platform darwin {
    patchfiles-append patch-swinst_darwin-declaration.diff
}
variant extra {
    patchfiles-prepend patch-a.diff patch-d.diff
}
`)
	require.Equal(t, []string{"patch-a.diff", "patch-b.diff", "patch-swinst_darwin-declaration.diff", "patch-d.diff"}, PatchfileNames(src))
}
