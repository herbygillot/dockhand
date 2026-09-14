package prepare

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGoSetupEditPreservesPackageTagConventionAndFormatting(t *testing.T) {
	before := "PortGroup golang 1.0\n\ngo.setup  github.com/owner/project 1.0 release/ -stable ;# retain this\nrevision 4\n"
	after, err := versionEdits([]byte(before), "1.0", "2.0", 4)
	require.NoError(t, err)
	require.Equal(t, "PortGroup golang 1.0\n\ngo.setup  github.com/owner/project 2.0 release/ -stable ;# retain this\nrevision 0\n", string(after))
}

func TestGitLabSetupEditPreservesRepositoryAndTagConvention(t *testing.T) {
	before := "PortGroup gitlab 1.0\n\ngitlab.setup  group/subgroup project 1.0 release/ -stable ;# retain this\nrevision 4\n"
	after, err := versionEdits([]byte(before), "1.0", "2.0", 4)
	require.NoError(t, err)
	require.Equal(t, "PortGroup gitlab 1.0\n\ngitlab.setup  group/subgroup project 2.0 release/ -stable ;# retain this\nrevision 0\n", string(after))
}

func TestGoSetupRejectsAmbiguousOrCalculatedVersionSources(t *testing.T) {
	for _, source := range []string{
		"go.setup github.com/owner/project [format %s 1.0]\n",
		"go.setup github.com/owner/project 0.9\n",
		"go.setup github.com/owner/project\n",
		"go.setup github.com/owner/project 1.0 v suffix unexpected\n",
		"go.setup github.com/owner/project 1.0\ngo.setup github.com/owner/project 1.0\n",
		"go.setup github.com/owner/project 1.0\ngithub.setup owner project 1.0\n",
		"version 1.0\ngo.setup github.com/owner/project 1.0\n",
	} {
		after, err := versionEdits([]byte(source), "1.0", "2.0", 0)
		require.ErrorIs(t, err, ErrUnsupported, source)
		require.Nil(t, after)
	}
}
