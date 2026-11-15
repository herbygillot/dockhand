//go:build surveymeasure

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portedit/observe"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/stretchr/testify/require"
)

type probeObserverFunc func(context.Context, macports.Context, macports.ObservationRequest) (macports.Observation, error)

func (f probeObserverFunc) Observe(ctx context.Context, c macports.Context, r macports.ObservationRequest) (macports.Observation, error) {
	return f(ctx, c, r)
}

func TestProbeBindsThePinnedSourceInsteadOfStrippedSnapshotMetadata(t *testing.T) {
	root := t.TempDir()
	target := model.Target{Name: "fixture", Portfile: "devel/fixture/Portfile"}
	require.NoError(t, os.MkdirAll(filepath.Join(root, "devel/fixture"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(root, target.Portfile), []byte("PortSystem 1.0"), 0600))
	pinned := model.Source{Tree: model.ObjectID(strings.Repeat("a", 40))}
	native := model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}
	profile := model.Platform{OS: "darwin", Version: "24", Architecture: "x86_64"}
	info := macports.PortInfo{Options: map[string]string{"build.cmd": "make"}}
	cap := &toleranceCapture{Target: target, Root: root, Platform: profile, Pair: tolerancePair{Complete: true, On: observe.Explanation{Accesses: []observe.AccessExplanation{{Rule: observe.RuleBuildDefault, Followers: []observe.FollowerUse{{Benign: true}}}}}}}
	called := false
	observer := probeObserverFunc(func(_ context.Context, c macports.Context, r macports.ObservationRequest) (macports.Observation, error) {
		called = true
		require.Equal(t, pinned, c.Source())
		require.Equal(t, native, c.Platform())
		require.Equal(t, profile, r.Platform)
		require.True(t, r.SelectedOnly)
		require.True(t, r.Declarations)
		// As in VersionProbe's cached observations, no source identity is
		// carried in the snapshot. The survey supplies its pin separately.
		return macports.Observation{Snapshot: macports.Snapshot{Ports: map[string]macports.PortInfo{"fixture": info}}, Ports: map[string]macports.PortObservation{"fixture": {}}}, nil
	})
	r := perturbContext(t.Context(), observer, pinned, native, info, cap)
	require.True(t, called)
	require.Equal(t, probeNotPerturbed, r.Status)
	require.True(t, r.SubstitutionsKnown)
}
