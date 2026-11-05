package portfile

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const familyBase = `PortSystem 1.0
name foo
version 1.0

subport foo-a {
    configure.args --a
}

subport foo-b {
    platform darwin 19 {
        configure.args-append --old
    }
}
`

// A change only a platform block or a platform condition sees is said by
// the subport whose block holds it, or as every subport's outside any;
// one this Mac's evaluation sees is none of these (Codex's review of
// 386ac2cc, finding 1).
func TestUnseenChangesSayWhichSubportsAnotherPlatformSees(t *testing.T) {
	native := []byte(`PortSystem 1.0
name foo
version 1.0

subport foo-a {
    configure.args --a --native
}

subport foo-b {
    platform darwin 19 {
        configure.args-append --old
    }
}
`)
	subports, all := UnseenChanges([]byte(familyBase), native)
	require.False(t, all)
	require.Empty(t, subports, "foo-a's change is native, which the record sees")

	mixed := []byte(`PortSystem 1.0
name foo
version 1.0

subport foo-a {
    configure.args --a --native
}

subport foo-b {
    platform darwin 19 {
        configure.args-append --older
    }
}
`)
	subports, all = UnseenChanges([]byte(familyBase), mixed)
	require.False(t, all)
	require.Equal(t, []string{"foo-b"}, subports)

	condition := []byte(familyBase + `
if {${os.major} < 20} {
    depends_build-append port:cctools
}
`)
	_, all = UnseenChanges([]byte(familyBase), condition)
	require.True(t, all, "a condition outside any subport block is every subport's")

	_, all = UnseenChanges([]byte(familyBase), []byte("subport {"))
	require.True(t, all, "a version that doesn't parse says nothing")
}

// A change inside a subport's literal block is that subport's alone, as
// MacPorts runs the block only when evaluating it; one anywhere else, or
// a Portfile that doesn't parse, is every subport's (field testing's
// terraform replay, 2026-10-22).
func TestBlockChangesSayWhoseBlockAChangeIsIn(t *testing.T) {
	t.Parallel()
	portfile := func(v16, v10, top string) []byte {
		return []byte("PortSystem 1.0\nname terraform\nversion " + top + "\n\nsubport terraform-1.16 {\n    version " + v16 + "\n    checksums sha256 aa\n}\n\nsubport terraform-1.0 {\n    version " + v10 + "\n}\n\nif {${subport} eq ${name}} {\n    subport terraform_select {\n        version 1\n    }\n}\n")
	}
	base := portfile("1.16.6", "1.0.11", "1.0")
	subports, all := BlockChanges(base, portfile("1.16.7", "1.0.11", "1.0"))
	require.False(t, all)
	require.Equal(t, []string{"terraform-1.16"}, subports)
	subports, all = BlockChanges(base, portfile("1.16.7", "1.0.12", "1.0"))
	require.False(t, all)
	require.Equal(t, []string{"terraform-1.0", "terraform-1.16"}, subports)
	_, all = BlockChanges(base, portfile("1.16.6", "1.0.11", "2.0"))
	require.True(t, all, "outside every block")
	_, all = BlockChanges(base, []byte("subport x {\n"))
	require.True(t, all, "doesn't parse")
	subports, all = BlockChanges(base, base)
	require.False(t, all)
	require.Empty(t, subports)
}

// omnidiffPortfile is omnidiff's Portfile as check-766 built it, before
// its openmaintainer line and its move to textproc.
const omnidiffPortfile = `# -*- coding: utf-8; mode: tcl; tab-width: 4; indent-tabs-mode: nil; c-basic-offset: 4 -*- vim:fenc=utf-8:ft=tcl:et:sw=4:ts=4:sts=4

PortSystem          1.0
PortGroup           github 1.0
PortGroup           golang 1.0

go.setup            github.com/example/omnidiff 1.2.0 v
revision            0
categories          devel textproc
maintainers         {@herbygillot gmail.com:herby.gillot}
license             MIT
description         A diff for every format
long_description    omnidiff compares two files of any format \
                    it can read, and says what changed.
homepage            https://github.com/example/omnidiff

platform darwin 21 {
    configure.args-append --legacy
}

checksums           rmd160  0 \
                    sha256  0 \
                    size    0

livecheck.type      none
`

// The lines decide whether a Portfile's change builds anything
// differently: omnidiff's openmaintainer line and its categories' reorder
// don't, nor a comment, nor an inert option set inside a platform's body;
// a value with a command substitution, license, and anything else do.
func TestInertChangesAreOnlyOptionsNoBuildReads(t *testing.T) {
	t.Parallel()
	edit := func(old, new string) []byte {
		require.Contains(t, omnidiffPortfile, old)
		return []byte(strings.Replace(omnidiffPortfile, old, new, 1))
	}
	before := []byte(omnidiffPortfile)
	for _, c := range []struct {
		name    string
		after   []byte
		options []string
		ok      bool
	}{
		{"omnidiff's openmaintainer line, check-766 to check-767's first", edit("{@herbygillot gmail.com:herby.gillot}\n", "{@herbygillot gmail.com:herby.gillot} \\\n                    openmaintainer\n"), []string{"maintainers"}, true},
		{"omnidiff's categories reordered for its move to textproc", edit("categories          devel textproc", "categories          textproc devel"), []string{"categories"}, true},
		{"a continuation line of long_description", edit("it can read, and says what changed.", "it can read, and says what moved."), []string{"long_description"}, true},
		{"a comment and a blank line", edit("PortSystem          1.0\n", "PortSystem          1.0\n\n# Built with Go's modules.\n"), nil, true},
		{"livecheck", edit("livecheck.type      none", "livecheck.regex     {v([0-9.]+)}"), []string{"livecheck.regex", "livecheck.type"}, true},
		{"a modifier", edit("homepage            https://github.com/example/omnidiff\n", "homepage            https://github.com/example/omnidiff\nmaintainers-append  openmaintainer\n"), []string{"maintainers"}, true},
		{"an inert option inside a platform's body", edit("    configure.args-append --legacy\n", "    configure.args-append --legacy\n    description  An older diff\n"), []string{"description"}, true},
		{"a line with a command substitution", edit("description         A diff for every format", "description         [string toupper {A diff}]"), nil, false},
		{"license", edit("license             MIT", "license             BSD"), nil, false},
		{"a build option inside a platform's body", edit("configure.args-append --legacy", "configure.args-append --older"), nil, false},
		{"the platform's own line", edit("platform darwin 21 {", "platform darwin 22 {"), nil, false},
		{"a separator joining an inert option with another", edit("categories          devel textproc", "categories          devel textproc; revision 1"), nil, false},
		{"the revision", edit("revision            0", "revision            1"), nil, false},
		{"a version that doesn't parse", edit("homepage            https://github.com/example/omnidiff", "homepage            {https://github.com/example/omnidiff"), nil, false},
	} {
		options, ok := InertChanges(before, c.after)
		require.Equal(t, c.ok, ok, c.name)
		require.Equal(t, c.options, options, c.name)
	}
	options, ok := InertChanges(before, before)
	require.True(t, ok)
	require.Empty(t, options)
}

// An inert option the Portfile reads elsewhere certifies nothing: a
// destroot hook writing ${description} into a file builds differently
// when it changes (Codex's sixth review, #70). One read by another inert
// option's line, as long_description's, is still metadata, and one named
// in a comment isn't read.
func TestAnInertOptionReadByAPhaseIsntInert(t *testing.T) {
	t.Parallel()
	port := func(description, rest string) []byte {
		return []byte("PortSystem 1.0\nname fixture\nversion 1\ndescription " + description + "\n" + rest)
	}
	for _, c := range []struct {
		rest string
		ok   bool
	}{
		{"long_description {*}${description}\n", true},
		{"# ${description} is said in the README\n", true},
		{"destroot {\n    puts [open ${destroot}/d w] ${description}\n}\n", false},
		{"post-destroot { system \"echo $description\" }\n", false},
		{"if {[option description] eq \"alpha\"} { revision 1 }\n", false},
	} {
		options, ok := InertChanges(port("alpha", c.rest), port("beta", c.rest))
		require.Equal(t, c.ok, ok, c.rest)
		if ok {
			require.Equal(t, []string{"description"}, options)
		}
	}
}
