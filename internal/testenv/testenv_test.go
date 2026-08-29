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
