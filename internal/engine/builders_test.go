package engine

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/macports/buildhistory"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// histories stand in for ports.macports.org's build history, by port;
// a port it doesn't hold has no answer.
type histories map[string]buildhistory.History

func (h histories) History(_ context.Context, port string) (buildhistory.History, bool) {
	history, ok := h[port]
	return history, ok
}

// latest is a history of one build on each builder, each "builder status".
func latest(port string, builds ...[2]string) buildhistory.History {
	history := buildhistory.History{Port: port, Builds: []buildhistory.Build{}}
	for _, build := range builds {
		history.Builds = append(history.Builds, buildhistory.Build{Builder: build[0], Status: build[1], Started: time.Date(2026, 10, 25, 20, 0, 0, 0, time.UTC)})
	}
	return history
}

// A failed check's offer of a baseline says what MacPorts' builders made
// of each offered port on each release it failed on here, by their latest
// build there: that they failed it too, which can save the baseline's
// build, or that they built it. A port the site gave no answer for, and a
// release its builders never built it on, say nothing.
func TestBuildersAreSaidBesideABaseline(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch := twoPortBranch(t, e)
	e.Providers = map[string]buildenv.Provider{"command": &scriptedProvider{outcomes: map[model.TargetID]model.Outcome{"jq": model.OutcomeFailed, "libharbor": model.OutcomeFailed}}}
	capture, err := e.Capture(t.Context(), CaptureRequest{Branch: branch, Mode: CaptureHead})
	require.NoError(t, err)
	sequoia := model.Environment{Provider: "command", Platform: model.Platform{OS: "darwin", Version: "24", Architecture: "arm64"}}
	plan, err := e.PlanCheck(t.Context(), PlanRequest{Revision: capture.Revision, Environments: []model.Environment{tahoeArm, tahoeX86, sequoia}})
	require.NoError(t, err)
	queued, err := e.Enqueue(t.Context(), branch, plan, model.OriginPerson)
	require.NoError(t, err)
	run, err := e.Drive(t.Context(), session(t, e), queued.ID)
	require.NoError(t, err)
	require.Equal(t, model.RunFailed, run.State)
	offer, err := e.BaselineCandidates(t.Context(), run)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"jq", "libharbor"}, offer.Ports)

	require.Empty(t, e.BuildersBeside(t.Context(), run, offer.Ports), "nothing read without a reader")
	e.Builders = histories{"jq": latest("jq",
		[2]string{"26_arm64", "failed install-port"},
		[2]string{"26_x86_64", "build successful"},
		[2]string{"10.13_x86_64", "failed install-dependencies"})}
	require.Equal(t, []string{
		"MacPorts' builders failed jq on macOS 26 arm64 too, at their latest build there, 2026-10-25 (ports.macports.org)",
		"MacPorts' builders built jq on macOS 26 x86_64 at their latest build there, 2026-10-25 (ports.macports.org)",
	}, e.BuildersBeside(t.Context(), run, offer.Ports), "macOS 15 never built jq, and libharbor had no answer")

	e.Builders = histories{"libharbor": latest("libharbor", [2]string{"15_arm64", "failed install-dependencies"})}
	require.Equal(t, []string{
		"MacPorts' builders failed the dependencies of libharbor on macOS 15 arm64 too, at their latest build there, 2026-10-25 (ports.macports.org)",
	}, e.BuildersBeside(t.Context(), run, []string{"libharbor"}))
}

// Impact reads what MacPorts' builders made of each port the branch
// changes, once a port; a port the site gave no answer for is left out.
func TestImpactReadsTheBuildersHistory(t *testing.T) {
	t.Parallel()
	f := setup(t)
	harborMaster(t, f)
	e := f.open(t)
	usePorts(e, harborPorts())
	branch, err := e.Start(t.Context(), StartRequest{Name: "libharbor-3", Here: true})
	require.NoError(t, err)
	testsupport.WriteFiles(t, branch.Worktree, map[string]string{
		"devel/libharbor/Portfile":  "name libharbor\nversion 3\n",
		"devel/harbor-cli/Portfile": "name harbor-cli\nrevision 1\n",
	})
	harbor := latest("libharbor", [2]string{"26_arm64", "build successful"})
	e.Builders = histories{"libharbor": harbor}

	impact, err := e.Impact(t.Context(), branch, nil)
	require.NoError(t, err)
	require.Equal(t, []PortBuilds{{Port: "libharbor", History: harbor}}, impact.Builds, "harbor-cli had no answer")
}
