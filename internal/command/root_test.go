package command

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildinfo"
)

// Outside a ports checkout, dockhand alone says how to begin.
func TestBareDockhandSaysHowToStart(t *testing.T) {
	var out bytes.Buffer
	err := Run(t.Context(), nil, Streams{In: strings.NewReader(""), Out: &out, Err: &out})
	require.NoError(t, err)
	require.Contains(t, out.String(), "dockhand init sets up")
	require.Contains(t, out.String(), "docs/usage.md is the guide")
	require.NotContains(t, out.String(), "v2-final")
}

func TestVersionNamesTheBuild(t *testing.T) {
	var out bytes.Buffer
	err := Run(t.Context(), []string{"--version"}, Streams{In: strings.NewReader(""), Out: &out, Err: &out})
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(out.String(), "dockhand "), out.String())
}

func TestAnUnknownCommandIsRefused(t *testing.T) {
	var out bytes.Buffer
	err := Run(t.Context(), []string{"bump", "jq"}, Streams{In: strings.NewReader(""), Out: &out, Err: &out})
	require.Error(t, err)
}

// The main help opens with dockhand's logo, as earlier generations' did,
// and the build's version on the line under it.
func TestTheHelpOpensWithTheLogoAndTheVersion(t *testing.T) {
	out, _, err := dockhand(t, "--help")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(out, logo+" "+buildinfo.Current().String()+"\n\nAuthor, check, and submit changes to MacPorts ports.\n"), out)
}
