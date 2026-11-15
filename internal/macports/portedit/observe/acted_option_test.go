package observe

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestActedOptionUsesTheFollowersSinkRule(t *testing.T) {
	for _, c := range []struct {
		name  string
		acted bool
	}{
		{"distfiles", true}, {"checksums", true}, {"patchfiles", true}, {"patchfiles-append", true},
		{"build.cmd", false}, {"configure.args", false}, {"cmake.source_dir", false},
		{"use_xcode", false}, {"macosx_deployment_target", false}, {"git.url", true},
	} {
		require.Equal(t, c.acted, ActedOption(c.name), c.name)
	}
}
