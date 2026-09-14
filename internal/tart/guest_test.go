package tart

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
)

func localGuest(ctx context.Context, input io.Reader, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, args[0], args[1:]...)
	command.Stdin = input
	out, err := command.Output()
	if err != nil {
		return out, fmt.Errorf("remote exec: %w", err)
	}
	return out, nil
}

func TestGuestTransportProbesRoundTripAndFailure(t *testing.T) {
	require.NoError(t, CheckGuestTransport(t.Context(), localGuest))
	for _, mode := range []string{"lost-input", "lost-output", "swallowed-exit", "transport-error"} {
		t.Run(mode, func(t *testing.T) {
			run := func(ctx context.Context, input io.Reader, args ...string) ([]byte, error) {
				if mode == "lost-input" {
					input = nil
				}
				out, err := localGuest(ctx, input, args...)
				if mode == "lost-output" {
					out = nil
				}
				if err != nil && mode == "swallowed-exit" {
					err = nil
				}
				if err != nil && mode == "transport-error" {
					err = errors.New("disconnected")
				}
				return out, err
			}
			require.Error(t, CheckGuestTransport(t.Context(), run))
		})
	}
}

func TestGuestAgentVersionIsOnlyDiagnostic(t *testing.T) {
	for _, value := range []string{"tart-guest-agent version 0.14.1-extra", "nightly development snapshot", ""} {
		run := func(context.Context, io.Reader, ...string) ([]byte, error) { return []byte(value), nil }
		observed, err := ObserveGuestAgentVersion(t.Context(), run)
		require.NoError(t, err)
		if value == "tart-guest-agent version 0.14.1-extra" {
			require.Equal(t, "0.14.1-extra", observed)
		} else {
			require.Equal(t, value, observed)
		}
	}
	observed, err := ObserveGuestAgentVersion(t.Context(), func(context.Context, io.Reader, ...string) ([]byte, error) {
		return []byte("no such option"), errors.New("exit 1")
	})
	require.NoError(t, err)
	require.Empty(t, observed)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = ObserveGuestAgentVersion(ctx, localGuest)
	require.ErrorIs(t, err, context.Canceled)
	require.Error(t, CheckGuestTransport(ctx, localGuest))
}
