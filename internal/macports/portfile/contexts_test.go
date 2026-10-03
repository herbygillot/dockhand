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
