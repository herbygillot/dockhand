package portfile

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A rename sets the name on the Portfile's name line, or, where the name
// comes from a forge's setup, on a name line below it, keeping the port's
// source where it is; a name set otherwise is refused.
func TestRenameSetsTheNameWhereThePortfileSetsIt(t *testing.T) {
	named := "PortSystem 1.0\nPortGroup github 1.0\ngithub.setup goplus gop 1.7.5 v\nname goplus\nversion 1.7.5\n"
	out, err := Rename([]byte(named), "goplus", "xgo")
	require.NoError(t, err)
	require.Equal(t, "PortSystem 1.0\nPortGroup github 1.0\ngithub.setup goplus gop 1.7.5 v\nname xgo\nversion 1.7.5\n", string(out), "the name line, not the setup's project")

	setup := "PortSystem 1.0\nPortGroup github 1.0\ngithub.setup        acme foo 1.0 v\nrevision            0\n"
	out, err = Rename([]byte(setup), "foo", "bar")
	require.NoError(t, err)
	require.Equal(t, "PortSystem 1.0\nPortGroup github 1.0\ngithub.setup        acme foo 1.0 v\nname                bar\nrevision            0\n", string(out), "the source stays acme/foo")

	golang := "PortSystem 1.0\nPortGroup golang 1.0\ngo.setup github.com/goplus/gop 1.7.5 v"
	out, err = Rename([]byte(golang), "gop", "xgo")
	require.NoError(t, err)
	require.Equal(t, "PortSystem 1.0\nPortGroup golang 1.0\ngo.setup github.com/goplus/gop 1.7.5 v\nname     xgo\n", string(out), "the module's path is the person's")

	_, err = Rename([]byte("PortSystem 1.0\nname [string tolower Foo]\n"), "foo", "bar")
	require.ErrorIs(t, err, ErrUnsupported)
	require.ErrorContains(t, err, "the Portfile sets the name foo neither on a name line nor through a forge's setup")
}

// A port moved to another category's directory names it first on its
// categories line, where that line named the old one first.
func TestSetPrimaryCategory(t *testing.T) {
	out, changed, err := SetPrimaryCategory([]byte("name dockhand\ncategories devel macports\n"), "devel", "macports")
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "name dockhand\ncategories macports macports\n", string(out))
	_, changed, err = SetPrimaryCategory([]byte("name dockhand\ncategories sysutils devel\n"), "devel", "macports")
	require.NoError(t, err)
	require.False(t, changed, "devel isn't first")
}

// The stub a renamed port leaves: obsolete, replaced_by the new name, at
// its version with its revision raised.
func TestARenamedPortsStub(t *testing.T) {
	stub := Stub{Name: "goplus", ReplacedBy: "xgo", Version: "1.7.5", Revision: 1, Categories: []string{"lang", "devel"}}
	replaced, ok := ReplacedBy(stub.Text())
	require.True(t, ok)
	require.Equal(t, "xgo", replaced)
	_, ok = ReplacedBy([]byte("PortSystem 1.0\nname jq\nreplaced_by jq2\n"))
	require.False(t, ok, "no obsolete PortGroup")
	require.Equal(t, Modeline+`

PortSystem          1.0
PortGroup           obsolete 1.0

name                goplus
replaced_by         xgo
version             1.7.5
revision            1
categories          lang devel
`, string(stub.Text()))
}
