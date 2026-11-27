package term

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// An exit code survives wrapping: a held bump or a failed check wrapped
// with more words still exits 3 or 2, not 1.
func TestAnExitCodeSurvivesWrapping(t *testing.T) {
	t.Parallel()
	held := Exitf(3, "jq-4k2p waits for your look")
	require.Equal(t, 3, ExitCode(held))
	require.Equal(t, 3, ExitCode(fmt.Errorf("bump: %w", held)))
	require.Equal(t, 1, ExitCode(errors.New("no port jq")))
}

// A command's final error is written as the process ends on it, another
// party's words in it made visible, newlines and tabs kept: Git's or a
// remote's control characters can't draw over the terminal (#365).
func TestAFinalErrorIsWrittenVisible(t *testing.T) {
	var out bytes.Buffer
	WriteError(&out, fmt.Errorf("the fork's branch couldn't be read (remote: \x1b[2Jdone\rtricked\tnow); nothing was done\nNext: dockhand submit"))
	require.Equal(t, "the fork's branch couldn't be read (remote: ␛[2Jdone␍tricked\tnow); nothing was done\nNext: dockhand submit\n", out.String())
	out.Reset()
	WriteError(&out, &ExitError{Code: ExitFailure})
	require.Empty(t, out.String(), "an error with no words writes nothing")
	WriteError(&out, nil)
	require.Empty(t, out.String())
}
