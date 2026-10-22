package term

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
)

// A decision is taken by --yes, asked on a terminal, and otherwise refused
// in one sentence, exit 1, or answered where a safe way on is given; each
// says how it was answered.
func TestADecisionSaysHowItWasAnswered(t *testing.T) {
	t.Parallel()
	d := Decision{Question: "? Remove 2 items? [y/N] ", Undone: "nothing was removed", ByYes: "--yes removes these"}
	script := Streams{In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}

	ok, how, err := script.Decide(true, d)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, model.AnsweredYes, how)

	ok, how, err = script.Decide(false, d)
	require.EqualError(t, err, "nothing was removed: without a terminal, --yes removes these")
	require.Equal(t, ExitFailure, ExitCode(err))
	require.False(t, ok)
	require.Equal(t, model.AnsweredNobody, how)

	safe := d
	safe.Nobody = Answer(true)
	ok, how, err = script.Decide(false, safe)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, model.AnsweredNobody, how)

	var asked bytes.Buffer
	terminal := Streams{In: strings.NewReader("y\n"), Out: &bytes.Buffer{}, Err: &asked}.AsTerminal()
	ok, how, err = terminal.Decide(false, d)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, model.AnsweredAsked, how)
	require.Equal(t, "? Remove 2 items? [y/N] ", asked.String())

	defaulted := Streams{In: strings.NewReader("\n"), Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}.AsTerminal()
	d.Yes = true
	ok, _, err = defaulted.Decide(false, d)
	require.NoError(t, err)
	require.True(t, ok, "return takes the question's default")
}
