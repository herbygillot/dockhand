package verifytest

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/verify"
)

// The optional capabilities are optional in fact, not only in the
// documentation: a caller's branch for a provider that cannot answer
// is dead code until some double actually cannot.
func TestIncapableAnswersOnlyTheContract(t *testing.T) {
	var v verify.Verifier = Incapable{Fake: &Fake{}}

	_, isLister := v.(verify.WorkerLister)
	assert.False(t, isLister, "Incapable must not list workers")
	_, isExecutor := v.(verify.Executor)
	assert.False(t, isExecutor, "Incapable must not reach inside an environment")

	var full verify.Verifier = &Fake{}
	_, isLister = full.(verify.WorkerLister)
	assert.True(t, isLister, "Fake must list workers, or nothing exercises the audit")
}

func TestFakeWorkersAreScripted(t *testing.T) {
	f := &Fake{Live: []verify.Worker{{Name: "dockhand-worker-1", Owner: "/elsewhere"}}}
	got, err := f.Workers(t.Context())
	require.NoError(t, err)
	assert.Equal(t, f.Live, got)

	// A machine that will not answer is not an idle machine, and the
	// double has to be able to say so.
	boom := errors.New("no answer")
	_, err = (&Fake{WorkersErr: boom}).Workers(t.Context())
	require.ErrorIs(t, err, boom)
}
