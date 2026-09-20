package portfile_test

import (
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/stretchr/testify/require"
	"strings"
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
	for _, src := range []string{"go.setup github.com/owner/project 1.2 v\n", "gitlab.setup owner project 1.2 v\n", "github.setup owner project 2026-09-07\nversion [string map {- {}} ${github.version}]\n", "set patch 3\nproc release {} {global patch; return 1.2.${patch}}\nversion [release]\n", "perl5.setup App-cpanminus 1.7049 ../../authors/id/M/MI/MIYAGAWA\n", "R.setup cran jeroen jsonlite 1.8.9\n", "R.setup github tidyverse ggplot2 3.5.1 v\n", "ruby.setup 3llo 1.3.1 gem {} rubygems\n", "ruby.setup {rails railties} 7.1.2 gem {} rubygems ruby33\n",
		"aspelldict.setup af 0.50-0 {Afrikaans}\n", "hunspelldict.setup af_ZA 2006-01-17 {Afrikaans (South Africa)} ooo\n", "x11font.setup font-adobe-100dpi 1.0.3 100dpi\n", "pure.setup faust2pd 2.16\n", "crossbinutils.setup aarch64-elf 2.47\n",
		"bitbucket.setup Coin3D coin 3.1.3 Coin-\n", "codeberg.setup mrirecon bart 1.0.01 v\n", "octave.setup github gnu-octave pkg-apa 1.2.2 v\n", "octave.setup pkg-apa 1.2.2\n"} {
		values, err := portfile.Candidates([]byte(src))
		require.NoError(t, err)
		require.Len(t, values, 1)
		require.NotEqual(t, values[0].Value, values[0].Probe())
	}
}

// php sets its version inside a switch on the subport's branch. The arms'
// bodies are scripts and their literals are inputs; the patterns that select
// them are not, and a "-" body falls through to the next.
func TestCandidatesDescendIntoSwitchArms(t *testing.T) {
	t.Parallel()
	src := "set branch 8.4\nswitch ${branch} {\n    5.2 -\n    5.3 {\n        version 5.3.29\n    }\n    8.4 {\n        version 8.4.25\n        set patch 8.4.25-1\n    }\n    default {\n        version 0\n    }\n}\nswitch -exact -- ${branch} 5.2 { version 1.0 } 8.4 { version 2.0 }\n"
	candidates, err := portfile.Candidates([]byte(src))
	require.NoError(t, err)
	var values []string
	for _, candidate := range candidates {
		values = append(values, candidate.Value)
	}
	require.Equal(t, []string{"8.4", "5.3.29", "8.4.25", "8.4.25-1", "0", "1.0", "2.0"}, values, "bodies in both forms, and no pattern")
	edited, err := candidates[2].Replace([]byte(src), "8.4.26")
	require.NoError(t, err)
	require.Equal(t, strings.Replace(src, "version 8.4.25\n", "version 8.4.26\n", 1), string(edited), "the span is the literal's own")
}
