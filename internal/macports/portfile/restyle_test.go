package portfile

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A Portfile restyled whole: trailing whitespace and tabs gone, commands
// indented in fours with their continuations, and each run of
// declarations lined up at the column most of it uses; a braced value of
// several lines, and code's commands, keep their spaces.
func TestAPortfileIsRestyledWhole(t *testing.T) {
	t.Parallel()
	src := Modeline + `

PortSystem 1.0
PortGroup github 1.0

github.setup        foo bar 1.0 v
revision            0
categories  devel   
maintainers         nomaintainer
python.pep517_backend flit

long_description    {a thing
   that keeps   its spaces}

post-destroot {
  set doc ${destroot}${prefix}/share/doc
	if {1} {
	  xinstall -m 0644 a \
	      ${doc}
	}
  }
`
	want := Modeline + `

PortSystem 1.0
PortGroup github 1.0

github.setup        foo bar 1.0 v
revision            0
categories          devel
maintainers         nomaintainer
python.pep517_backend flit

long_description    {a thing
   that keeps   its spaces}

post-destroot {
    set doc ${destroot}${prefix}/share/doc
    if {1} {
        xinstall -m 0644 a \
            ${doc}
    }
}
`
	got, changed := Restyle([]byte(src))
	require.True(t, changed)
	require.Equal(t, want, string(got))
	again, changed := Restyle(got)
	require.False(t, changed, "a restyled Portfile is restyled already")
	require.Equal(t, want, string(again))
}
