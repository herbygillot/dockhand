package provision

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type adoptionFixture struct {
	*fakeMachine
	clone func(context.Context, string, string) error
}

func (f adoptionFixture) Clone(ctx context.Context, from, to string) error {
	return f.clone(ctx, from, to)
}

func TestAdoptionRestoresOldImageAfterFailedClone(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(map[bool]string{false: "failure", true: "cancellation"}[canceled], func(t *testing.T) {
			m := newFakeMachine("ready", "candidate")
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			failure := errors.New("clone failed")
			f := adoptionFixture{m, func(_ context.Context, _, to string) error {
				m.images[to] = image{Name: to}
				if canceled {
					cancel()
					return context.Canceled
				}
				return failure
			}}
			err := adopt(ctx, f, "candidate", "ready", true)
			if canceled {
				require.ErrorIs(t, err, context.Canceled)
			} else {
				require.ErrorIs(t, err, failure)
			}
			require.Contains(t, m.images, "ready")
			require.Contains(t, m.images, "candidate")
			require.NotContains(t, m.images, "ready-previous")
			require.Contains(t, m.events, "rename:ready-previous:ready")
		})
	}
}

func TestAdoptionPreservesPriorRecoveryImage(t *testing.T) {
	m := newFakeMachine("ready", "ready-previous", "candidate")
	err := adopt(t.Context(), m, "candidate", "ready", true)
	require.ErrorContains(t, err, "interrupted replacement")
	require.Equal(t, []string{"images"}, m.events)
}

func TestSuccessfulAdoptionRemovesPreviousOnlyAfterClone(t *testing.T) {
	m := newFakeMachine("ready", "candidate")
	require.NoError(t, adopt(t.Context(), m, "candidate", "ready", true))
	require.Less(t, index(m.events, "clone:candidate:ready"), index(m.events, "delete:ready-previous"))
	require.NotContains(t, m.images, "ready-previous")
}

func (f adoptionFixture) Delete(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return f.fakeMachine.Delete(ctx, name)
}
func (f adoptionFixture) Rename(ctx context.Context, from, to string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return f.fakeMachine.Rename(ctx, from, to)
}

func TestFailedRollbackKeepsPreviousAndCandidate(t *testing.T) {
	m := newFakeMachine("ready", "candidate")
	m.fail = "rename:ready-previous:ready"
	f := adoptionFixture{m, func(context.Context, string, string) error { return errors.New("clone failed") }}
	err := adopt(t.Context(), f, "candidate", "ready", true)
	require.ErrorContains(t, err, "previous image retained at ready-previous")
	require.Contains(t, m.images, "ready-previous")
	require.Contains(t, m.images, "candidate")
}
