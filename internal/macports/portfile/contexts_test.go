package portfile

import (
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
