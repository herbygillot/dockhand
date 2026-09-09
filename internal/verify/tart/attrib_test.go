package tart

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// redirectCache points the sidecars at a directory the test owns.
func redirectCache(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	orig := cacheDir
	cacheDir = func() (string, error) { return tmp, nil }
	t.Cleanup(func() { cacheDir = orig })
}

// A GUEST THAT HAS STOPPED CANNOT SAY WHY IT STOPPED. The host process
// that was the virtual machine can, and this is the only place that
// answer is kept — so what tart printed has to come back whole.
func TestTheWorkersLastWordsSurviveTheWorker(t *testing.T) {
	redirectCache(t)
	noteExit("dockhand-worker-cafe", "guest has stopped the virtual machine\n", nil)
	assert.Equal(t, "guest has stopped the virtual machine", ExitOf("dockhand-worker-cafe"))
}

// BOTH HALVES, because tart uses both: a refusal is a non-zero status
// and a guest that stopped on its own is a sentence with a zero one.
func TestAnExitNoteCarriesTheOutputAndTheStatus(t *testing.T) {
	redirectCache(t)
	noteExit("w", "guest has stopped", errors.New("exit status 1"))
	assert.Equal(t, "guest has stopped (exit status 1)", ExitOf("w"))

	redirectCache(t)
	noteExit("w", "", errors.New("exit status 2"))
	assert.Equal(t, "exit status 2", ExitOf("w"))
}

// SILENCE IS NOT AN ANSWER (rule 7). A clean stop writes nothing, so an
// empty read means "it said nothing" and never "nobody looked".
func TestACleanStopLeavesNoNote(t *testing.T) {
	redirectCache(t)
	noteExit("w", "   \n", nil)
	assert.Empty(t, ExitOf("w"))
}

// The note is destined for a status line a person reads, not a log.
func TestALoudExitIsBounded(t *testing.T) {
	redirectCache(t)
	noteExit("w", strings.Repeat("x", 4000), nil)
	assert.LessOrEqual(t, len(ExitOf("w")), exitNoteMax+8)
}

// A WORKER THAT WENT BACK HAS NOTHING LEFT TO EXPLAIN, and leaving the
// note behind would attribute one guest's death to the next VM that
// happened to be given its name.
func TestReleasingAWorkerForgetsItsLastWords(t *testing.T) {
	redirectCache(t)
	noteExit("w", "guest has stopped", nil)
	require.NotEmpty(t, ExitOf("w"))
	clearAttribution("w")
	assert.Empty(t, ExitOf("w"))
}

// THE TWO SENTENCES ANSWER DIFFERENT QUESTIONS — what became of the run,
// and what became of the machine — so the detail carries both when both
// are known, and stands alone when only one is.
func TestTheStoppedDetailSaysWhyWhenTheHostKnows(t *testing.T) {
	redirectCache(t)
	assert.Equal(t,
		"the environment stopped before the run reported an outcome",
		stoppedDetail("w"))

	noteExit("w", "guest has stopped the virtual machine", nil)
	got := stoppedDetail("w")
	assert.Contains(t, got, "stopped before the run reported an outcome")
	assert.Contains(t, got, "guest has stopped the virtual machine")
}
