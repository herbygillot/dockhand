package command

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBareDockhandSaysWhereTheWorkingToolIs(t *testing.T) {
	var out bytes.Buffer
	err := Run(t.Context(), nil, Streams{In: strings.NewReader(""), Out: &out, Err: &out})
	require.NoError(t, err)
	require.Contains(t, out.String(), "v2-final")
	require.Contains(t, out.String(), "docs/design-v3.md")
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
