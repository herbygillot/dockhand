package command

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// An exit code survives wrapping: a held bump or a failed check wrapped
// with more words still exits 3 or 2, not 1.
func TestAnExitCodeSurvivesWrapping(t *testing.T) {
	held := exitf(3, "jq-4k2p waits for your look")
	require.Equal(t, 3, ExitCode(held))
	require.Equal(t, 3, ExitCode(fmt.Errorf("bump: %w", held)))
	require.Equal(t, 1, ExitCode(errors.New("no port jq")))
}
