package engine

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/macports"
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

// Impact reads each changed port's history by its own name: a py-
// directory's subports by theirs, and a directory whose name isn't its
// port's by the port's.
func TestImpactReadsEachPortsHistoryByItsOwnName(t *testing.T) {
	t.Parallel()
	f := setup(t)
	testsupport.WriteFiles(t, f.upstream, map[string]string{
		"python/py-harbor/Portfile": "name py-harbor\nversion 1\n",
		"devel/harbor/Portfile":     "name libharbor\nversion 1\n",
	})
	testsupport.Git(t, f.upstream, "add", "-A")
	testsupport.Git(t, f.upstream, "commit", "-q", "-m", "the harbor ports")
	e := f.open(t)
	usePorts(e, portsByDirectory{fakePorts{directories: map[string][]macports.PortInfo{
		"python/py-harbor": {port("py-harbor"), port("py313-harbor"), port("py314-harbor")},
		"devel/harbor":     {port("libharbor")},
		"devel/new-port":   {port("new-port")},
	}}})
	branch, err := e.Start(t.Context(), StartRequest{Name: "harbor-2", Here: true})
	require.NoError(t, err)
	testsupport.WriteFiles(t, branch.Worktree, map[string]string{
		"python/py-harbor/Portfile": "name py-harbor\nversion 2\n",
		"devel/harbor/Portfile":     "name libharbor\nversion 2\n",
		"devel/new-port/Portfile":   "name new-port\nversion 1\n",
	})
	py313 := latest("py313-harbor", [2]string{"26_arm64", "build successful"})
	harbor := latest("libharbor", [2]string{"15_arm64", "failed install-port"})
	e.Builders = histories{"py313-harbor": py313, "libharbor": harbor, "harbor": latest("harbor"), "new-port": latest("new-port")}

	impact, err := e.Impact(t.Context(), branch, nil)
	require.NoError(t, err)
	var read []string
	for _, builds := range impact.Builds {
		read = append(read, builds.Port)
	}
	require.ElementsMatch(t, []string{"libharbor", "py313-harbor"}, read, "py-harbor and py314-harbor had no answer; harbor isn't a port; a new port has no history")
}

// portsByDirectory names each directory's ports as the fake reads them.
type portsByDirectory struct{ fakePorts }

func (d portsByDirectory) PortsDefined(_ context.Context, _ model.Source, directories []string) (map[string][]string, error) {
	found := map[string][]string{}
	for _, directory := range directories {
		for _, info := range d.directories[directory] {
			found[directory] = append(found[directory], info.Name)
		}
	}
	return found, nil
}
