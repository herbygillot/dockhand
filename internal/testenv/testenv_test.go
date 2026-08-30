package testenv

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRequired(t *testing.T) {
	cases := []struct {
		env  string
		tool string
		want bool
	}{
		{"", "tclsh", false},
		{"1", "tclsh", true},
		{"all", "port-tclsh", true},
		{"tclsh", "tclsh", true},
		{"tclsh", "port-tclsh", false},
		{"tclsh,port-tclsh", "port-tclsh", true},
		{"tclsh, port-tclsh", "port-tclsh", true},
		{"git", "tclsh", false},
	}
	for _, c := range cases {
		t.Setenv("DOCKHAND_TEST_REQUIRE", c.env)
		assert.Equal(t, c.want, required(c.tool), "env=%q tool=%q", c.env, c.tool)
	}
}

// Network rides the same switch as the tools, so a job that means to
// check against upstream asks for it by name.
func TestNetworkIsOptIn(t *testing.T) {
	for _, c := range []struct {
		env  string
		want bool
	}{
		{"", false},
		{"tclsh", false},
		{"tclsh,port-tclsh", false},
		{"network", true},
		{"tclsh,network", true},
		{"1", true},
		{"all", true},
	} {
		t.Setenv("DOCKHAND_TEST_REQUIRE", c.env)
		assert.Equal(t, c.want, required("network"), "env=%q", c.env)
	}
}
