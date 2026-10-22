package term

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDevNullIsNotATerminal(t *testing.T) {
	t.Parallel()
	null, err := os.Open(os.DevNull)
	require.NoError(t, err)
	defer null.Close()
	require.False(t, Streams{In: null}.Terminal(), "commands never prompt when input is /dev/null")
	require.False(t, Streams{In: strings.NewReader("y\n")}.Terminal())
}

func TestAnEndedInputTakesTheDefault(t *testing.T) {
	t.Parallel()
	answer, err := Streams{In: strings.NewReader(""), Err: &bytes.Buffer{}}.Ask("Keep? [Y/n] ")
	require.NoError(t, err)
	require.Empty(t, answer)
}
