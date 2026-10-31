package portfile

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// RaiseEpoch raises the one epoch in the target's scope, or adds one after
// the revision, else the version, aligned with it, and refuses an epoch
// the Portfile reckons itself, where it ran only behind tclsh (the test
// analysis's step b).
func TestRaiseEpoch(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, src, subport string
		current            int
		want, refused      string
	}{
		{"its literal", "PortSystem 1.0\nname fixture\nepoch 1\nversion 2.0\n", "", 1, "PortSystem 1.0\nname fixture\nepoch 2\nversion 2.0\n", ""},
		{"after the revision", "name    fixture\nversion 2.0\nrevision 1\n", "", 0, "name    fixture\nversion 2.0\nrevision 1\nepoch    1\n", ""},
		{"after the version", "name    fixture\nversion 2.0\n", "", 0, "name    fixture\nversion 2.0\nepoch   1\n", ""},
		{"a subport's own", "name fixture\nsubport fixture-cli {\n    epoch 1\n}\n", "fixture-cli", 1, "name fixture\nsubport fixture-cli {\n    epoch 2\n}\n", ""},
		{"another scope's too", "name fixture\nepoch 3\nsubport fixture-cli {\n    epoch 1\n}\n", "fixture-cli", 1, "", "epoch is set outside the selected scope"},
		{"in a condition", "name fixture\nversion 2.0\nif {${os.major} > 20} {\n    epoch 1\n}\n", "", 1, "", "epoch is set outside the selected scope"},
		{"twice", "name fixture\nepoch 1\nepoch 1\n", "", 1, "", "multiple epoch commands"},
		{"not its literal", "name fixture\nepoch 2\n", "", 1, "", "epoch expression does not have a matching literal"},
		{"computed", "name fixture\nepoch [expr 1]\n", "", 1, "", "epoch expression does not have a matching literal"},
		{"nonzero with none", "name fixture\nversion 2.0\n", "", 2, "", "nonzero epoch is set outside the supported scope"},
		{"at its limit", "name fixture\n", "", -1, "", "epoch cannot be incremented"},
	} {
		got, err := RaiseEpoch([]byte(c.src), c.subport, c.current)
		if c.refused != "" {
			require.ErrorContains(t, err, c.refused, c.name)
			continue
		}
		require.NoError(t, err, c.name)
		require.Equal(t, c.want, string(got), c.name)
	}
}
