package term

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// stageLine is a sentence of a stage's own, wider than 20 columns, and
// stageRaw a payload line wider still.
const (
	stageLine = "a sentence of the stage's own words\n"
	stageRaw  = "raw payload that runs past twenty columns\n"
)

// stagedAt20 are streams begun on out at 20 columns, and a stage of them.
func stagedAt20(out, errs *bytes.Buffer, args ...string) (Streams, Streams, *Stage) {
	streams := Begin(Streams{In: strings.NewReader("y\n"), Out: out, Err: errs}.WithDisplay(Display{Columns: 20}), args)
	staged, stage := streams.Staged("editing")
	return streams, staged, stage
}

// A stage's report dropped is never said: neither writer's lines come
// out, by Say or by a question after it (#94).
func TestAStagesDropSaysNothing(t *testing.T) {
	t.Parallel()
	var out, errs bytes.Buffer
	_, staged, stage := stagedAt20(&out, &errs)
	_, _ = staged.Prose().Write([]byte(stageLine))
	_, _ = staged.Out.Write([]byte(stageRaw))
	stage.Drop()
	stage.Say()
	require.Empty(t, out.String(), "dropped")
	_, err := staged.Ask("? go on ")
	require.NoError(t, err)
	require.Empty(t, out.String(), "a question has nothing held to say")
}

// A question on a stage's streams says what the stage held first, both
// writers in the order written, and what follows comes as it's written;
// Done then says its line, the report already said (#94).
func TestAQuestionSaysWhatTheStageHeld(t *testing.T) {
	t.Parallel()
	var out, errs bytes.Buffer
	_, staged, stage := stagedAt20(&out, &errs)
	_, _ = staged.Out.Write([]byte(stageRaw))
	_, _ = staged.Prose().Write([]byte(stageLine))
	require.Empty(t, out.String(), "held")
	answer, err := staged.Ask("? go on ")
	require.NoError(t, err)
	require.Equal(t, "y", answer)
	require.Equal(t, "? go on ", errs.String())
	held := stageRaw + "a sentence of the\n  stage's own words\n"
	require.Equal(t, held, out.String(), "said before the question, in order")
	_, _ = staged.Out.Write([]byte(stageRaw))
	require.Equal(t, held+stageRaw, out.String(), "once said, written as it comes")
	stage.Done("✓ edited")
	require.Equal(t, held+stageRaw+"✓ edited\n", out.String())
}

// With -v a stage holds nothing: both writers come out as they're
// written, the prose wrapped and the payload whole (#94).
func TestVerboseStageHoldsNothing(t *testing.T) {
	t.Parallel()
	var out, errs bytes.Buffer
	streams := Begin(Streams{Out: &out, Err: &errs}.WithDisplay(Display{Columns: 20}), nil)
	streams.SetVerbosity(1)
	staged, stage := streams.Staged("editing")
	require.False(t, staged.Held())
	_, _ = staged.Prose().Write([]byte(stageLine))
	_, _ = staged.Out.Write([]byte(stageRaw))
	require.Equal(t, "a sentence of the\n  stage's own words\n"+stageRaw, out.String(), "said as written")
	stage.Done("✓ edited")
	require.Equal(t, "a sentence of the\n  stage's own words\n"+stageRaw, out.String(), "no line of its own: the report was said")
}

// --json silences both writers, Out and Prose, staged or not, until the
// envelope (#94).
func TestJSONSilencesBothWriters(t *testing.T) {
	t.Parallel()
	var out, errs bytes.Buffer
	streams, staged, stage := stagedAt20(&out, &errs, "--json")
	streams.Reports("edit", true)
	for _, s := range []Streams{streams, staged} {
		_, _ = s.Prose().Write([]byte(stageLine))
		_, _ = s.Out.Write([]byte(stageRaw))
	}
	stage.Say()
	stage.Done("✓ edited")
	require.Empty(t, out.String())
}

// Raw and prose written in turn read back in the order written, the
// prose wrapped and the payload whole, staged and not (#94).
func TestRawAndProseInTurnKeepTheirOrder(t *testing.T) {
	t.Parallel()
	want := "a sentence of the\n  stage's own words\n" + stageRaw + "a sentence of the\n  stage's own words\n" + stageRaw
	var out, errs bytes.Buffer
	streams, staged, stage := stagedAt20(&out, &errs)
	for _, s := range []Streams{streams, staged} {
		for range 2 {
			_, _ = s.Prose().Write([]byte(stageLine))
			_, _ = s.Out.Write([]byte(stageRaw))
		}
		if s.Held() {
			require.Empty(t, out.String())
			stage.Say()
		}
		require.Equal(t, want, out.String())
		out.Reset()
	}
}

// On a terminal a table's line is cut at the width on Prose, staged or
// not, and never on Out, which writes a payload line that reads as a
// table whole (#94).
func TestCutIsProsesOnly(t *testing.T) {
	t.Parallel()
	table := "  jq  update  a long note\n"
	var out, errs bytes.Buffer
	streams := Begin(Streams{Out: &out, Err: &errs}.AsTerminal().WithDisplay(Display{Columns: 20}), nil)
	staged, stage := streams.Staged("editing")
	for _, s := range []Streams{streams, staged} {
		_, _ = s.Prose().Write([]byte(table))
		_, _ = s.Out.Write([]byte(table))
		if s.Held() {
			stage.Say()
		}
		require.Equal(t, "  jq  update  a lon…\n"+table, out.String())
		out.Reset()
	}
}
