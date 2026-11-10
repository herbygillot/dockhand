package guest

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/refusal"
)

// What builds from source is said with what the guest is building.
func TestTheProgressSaysWhatBuildsFromSource(t *testing.T) {
	job := buildenv.Job{Environment: model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}},
		Targets: []buildenv.Target{{PlanTarget: model.PlanTarget{ID: "libharbor", Target: model.Target{Name: "libharbor"}}}, {PlanTarget: model.PlanTarget{ID: "harbor-cli", Target: model.Target{Name: "harbor-cli"}}}}}
	tahoe, err := macos.ReleaseForDarwin(25)
	require.NoError(t, err)
	line := buildingWords(job, tahoe, Results{Building: "libharbor", Phase: "dependencies", FromSource: []string{"rust", "cargo"}})
	require.Equal(t, "building libharbor (1 of 2): dependencies; rust, cargo: no archive for macOS 26 arm64; building from source", line)
}

// stalledGuest is a guest whose program says it's building libharbor,
// and whose log of it never grows.
type stalledGuest struct{ Guest }

func (stalledGuest) Read(context.Context, string, bool) ([]byte, error) {
	return []byte(fmt.Sprintf(`{"protocol": %d, "state": "running", "building": "libharbor"}`, Protocol)), nil
}
func (stalledGuest) Append(context.Context, string, string, bool) error { return nil }
func (stalledGuest) Download(context.Context, string, string, bool) error {
	return errors.New("no log")
}

// running is a program that hasn't ended.
type running struct{}

func (running) Done() <-chan struct{}                     { return nil }
func (running) Err() error                                { return nil }
func (running) Stop(context.Context, time.Duration) error { return nil }

// saying is a build that keeps what it was told.
type saying struct {
	buildenv.Build
	said     []string
	recorded []buildenv.TargetReport
}

func (b *saying) Progress(line string) { b.said = append(b.said, line) }
func (b *saying) Record(report buildenv.TargetReport) error {
	b.recorded = append(b.recorded, report)
	return nil
}

// A target whose log grows by nothing for the stall bound ends the
// program as infrastructure trouble, to be tried again, said at half of
// it: a guest hung in MacPorts' check_supported_archs held a Tart slot for
// the six hours of its build timeout, saying nothing (field testing,
// rmlint, 2026-11-08).
func TestAStalledBuildIsEndedAndTriedAgain(t *testing.T) {
	t.Parallel()
	job := buildenv.Job{Directory: t.TempDir(), Environment: model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}},
		Targets: []buildenv.Target{{PlanTarget: model.PlanTarget{ID: "libharbor", Target: model.Target{Name: "libharbor"}}}}}
	tahoe, err := macos.ReleaseForDarwin(25)
	require.NoError(t, err)
	build := &saying{}
	f := &Follower{Root: "/guest", Poll: time.Millisecond, Stall: 200 * time.Millisecond}
	err = f.Follow(t.Context(), stalledGuest{}, running{}, job, build, tahoe)
	require.ErrorContains(t, err, "libharbor's log grew by nothing for 200ms, so its build is taken as stalled, and tried again (providers.tart.stall_timeout)")
	require.Equal(t, refusal.Infrastructure, refusal.KindOf(err), "%v", err)
	require.Len(t, build.recorded, 1)
	require.Equal(t, model.OutcomeInterrupted, build.recorded[0].Result.Outcome)
	require.True(t, slices.ContainsFunc(build.said, func(line string) bool { return strings.Contains(line, "libharbor's log has grown by nothing for") }), "%q", build.said)
}
