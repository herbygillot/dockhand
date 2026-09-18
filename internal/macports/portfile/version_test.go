package portfile_test

import (
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestCandidatesPreserveSourceAndExcludeHooksAndData(t *testing.T) {
	src := []byte("# keep\nset release {1.2} ;# trailing\nif {0} {set ignored 1.2}\ngithub.setup owner project $release v\nchecksums {set false 1.2}\npre-build {set false 1.2}\n")
	candidates, err := portfile.Candidates(src)
	require.NoError(t, err)
	require.Len(t, candidates, 2)
	after, err := candidates[0].Replace(src, "2.3")
	require.NoError(t, err)
	require.Equal(t, "# keep\nset release {2.3} ;# trailing\nif {0} {set ignored 1.2}\ngithub.setup owner project $release v\nchecksums {set false 1.2}\npre-build {set false 1.2}\n", string(after))
	_, err = candidates[0].Replace(src, "[exec bad]")
	require.Error(t, err)
}
func TestCandidatesFindSetupAndComposedInputs(t *testing.T) {
	for _, src := range []string{"go.setup github.com/owner/project 1.2 v\n", "gitlab.setup owner project 1.2 v\n", "github.setup owner project 2026-09-07\nversion [string map {- {}} ${github.version}]\n", "set patch 3\nproc release {} {global patch; return 1.2.${patch}}\nversion [release]\n", "perl5.setup App-cpanminus 1.7049 ../../authors/id/M/MI/MIYAGAWA\n", "R.setup cran jeroen jsonlite 1.8.9\n", "R.setup github tidyverse ggplot2 3.5.1 v\n", "ruby.setup 3llo 1.3.1 gem {} rubygems\n", "ruby.setup {rails railties} 7.1.2 gem {} rubygems ruby33\n"} {
		values, err := portfile.Candidates([]byte(src))
		require.NoError(t, err)
		require.Len(t, values, 1)
		require.NotEqual(t, values[0].Value, values[0].Probe())
	}
}
