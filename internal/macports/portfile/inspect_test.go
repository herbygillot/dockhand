package portfile_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
)

// A change is revision-only where the only commands that change are
// revision declarations MacPorts runs; "revision 2" in a set's value is
// data, and changing it is a change (the private-helper review's finding
// 2, as it reproduced it).
func TestARevisionOnlyChangeIsProvedFromTheSource(t *testing.T) {
	t.Parallel()
	const before = "name demo\nversion 1.0\nrevision 1\nsubport demo-extra {\n    revision 3\n}\nif {${os.major} > 20} {\n    revision 2\n}\nset contents {\nrevision 1\n}\n"
	for _, test := range []struct {
		name, after string
		only        bool
	}{
		{"the port's revision", "name demo\nversion 1.0\nrevision 2\nsubport demo-extra {\n    revision 3\n}\nif {${os.major} > 20} {\n    revision 2\n}\nset contents {\nrevision 1\n}\n", true},
		{"a subport's and a condition's", "name demo\nversion 1.0\nrevision 1\nsubport demo-extra {\n    revision 4\n}\nif {${os.major} > 20} {\n    revision 3\n}\nset contents {\nrevision 1\n}\n", true},
		{"revision text in data", "name demo\nversion 1.0\nrevision 1\nsubport demo-extra {\n    revision 3\n}\nif {${os.major} > 20} {\n    revision 2\n}\nset contents {\nrevision 2\n}\n", false},
		{"anything else", "name demo\nversion 1.1\nrevision 0\nsubport demo-extra {\n    revision 3\n}\nif {${os.major} > 20} {\n    revision 2\n}\nset contents {\nrevision 1\n}\n", false},
		{"what isn't Tcl", "name demo\nversion 1.0\nrevision 2\nset contents {\n", false},
	} {
		require.Equal(t, test.only, portfile.RevisionOnly([]byte(before), []byte(test.after)), test.name)
	}
	require.True(t, portfile.RevisionOnly([]byte("name jq\nversion 1.7.1\n"), []byte("name jq\nversion 1.7.1\nrevision 1\n")), "a revision added where there was none")
	require.True(t, portfile.RevisionOnly([]byte("name jq\nversion 1.7.1\nrevision 1"), []byte("name jq\nversion 1.7.1\n")), "and one removed, at the end without a newline")
	require.False(t, portfile.RevisionOnly([]byte("name jq\nversion 1.7.1\n"), []byte("name jq\nversion 1.7.1\nrevision 1; set x 2\n")), "the rest of its line is the source's")
}

// A port's declared version is read where it's literal and unconditional,
// the last declaration MacPorts runs winning, and never from another
// subport's block, as git-devel's github.setup was read for git's (the git
// run's finding 5).
func TestADeclaredVersionIsTheOneThePortDeclares(t *testing.T) {
	t.Parallel()
	const git = "PortSystem 1.0\nname git\nversion 2.55.0\nrevision 1\nsubport ${name}-devel {\n    PortGroup github 1.0\n    github.setup git git 2.56.0 v\n}\n"
	for _, test := range []struct {
		name, src, subport, version string
		ok                          bool
	}{
		{"git's own, not git-devel's", git, "", "2.55.0", true},
		{"a subport whose name isn't literal", git, "git-devel", "", false},
		{"a forge's setup line", "PortGroup github 1.0\ngithub.setup cli cli 2.101.0 v\n", "", "2.101.0", true},
		{"go.setup's", "PortGroup golang 1.0\ngo.setup github.com/twpayne/chezmoi 2.73.0 v\n", "", "2.73.0", true},
		{"a later version command over a setup line", "github.setup a b 1.0 v\nversion 1.1\n", "", "1.1", true},
		{"a computed one", "set v 1.0\nversion ${v}\n", "", "", false},
		{"a braced word that isn't a version", "version {1.0 beta}\n", "", "", false},
		{"a bare word that isn't a version", "version 1.0*\n", "", "", false},
		{"a conditional one", "version 1.0\nif {${os.major} < 20} {\n    version 0.9\n}\n", "", "", false},
		{"a subport's own", "version 1.0\nsubport demo-legacy {\n    version 0.9\n}\n", "demo-legacy", "0.9", true},
		{"a subport's that it inherits", "version 1.0\nsubport demo-extra {\n    revision 1\n}\n", "demo-extra", "1.0", true},
		{"none", "name demo\n", "", "", false},
	} {
		version, ok := portfile.DeclaredVersion([]byte(test.src), test.subport)
		require.Equal(t, test.ok, ok, test.name)
		require.Equal(t, test.version, version, test.name)
	}
}

func TestADeclaredRevisionIsTheMainPorts(t *testing.T) {
	t.Parallel()
	value, line, ok := portfile.DeclaredRevision([]byte("name git\nversion 2.56.0\nrevision 1\nsubport git-devel {\n    revision 0\n}\n"))
	require.True(t, ok)
	require.Equal(t, "1", value)
	require.Equal(t, 3, line)
	_, _, ok = portfile.DeclaredRevision([]byte("name demo\nrevision ${r}\n"))
	require.False(t, ok, "a computed one isn't proven")
}

// A Portfile's PortGroups are read where MacPorts would run the command,
// and anything that could load one it doesn't name is inconclusive.
func TestPortGroupReferencesAreReadFromTheSource(t *testing.T) {
	t.Parallel()
	references, conclusive := portfile.PortGroupReferences([]byte("PortSystem 1.0\nPortGroup github 1.0\n# PortGroup commented 1.0\nsubport demo-extra {\n    PortGroup cmake 1.1\n}\nset note {PortGroup data 1.0}\n"))
	require.True(t, conclusive)
	require.Equal(t, []macports.PortGroup{{Name: "github", Version: "1.0"}, {Name: "cmake", Version: "1.1"}}, references, "not the comment's, nor data's")
	for _, src := range []string{"PortGroup ${group} 1.0\n", "PortGroup github\n", "source ${portpath}/../../_resources/port1.0/group/extra.tcl\n"} {
		_, conclusive := portfile.PortGroupReferences([]byte(src))
		require.False(t, conclusive, src)
	}
	_, conclusive = portfile.PortGroupReferences([]byte("# _resources in a comment\nPortGroup github 1.0\n"))
	require.True(t, conclusive)
}

// A Portfile names a build option where a word of its own, or a -D or -U
// definition, is the option, in any block or comment, and not where it's
// part of a longer name.
func TestAPortfileMentionsAWordOfItsOwn(t *testing.T) {
	src := []byte("variant avro {\n    configure.args-append -DFLB_AVRO_ENCODER=ON -UFLB_KAFKA\n}\n# FLB_ALL is too much\nset opt FLB_TLS_EXTRA\n")
	for word, named := range map[string]bool{"FLB_AVRO_ENCODER": true, "FLB_KAFKA": true, "FLB_ALL": true, "FLB_TLS": false, "FLB_AVRO": false, "AVRO_ENCODER": false, "": false} {
		require.Equal(t, named, portfile.Mentions(src, word), word)
	}
}
