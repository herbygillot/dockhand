package engine

import (
	"context"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// inertPorts are the harbor ports, evaluated as a family by reading each
// Portfile's version and maintainers lines from the revision, as a change
// record compares them, and linted on the host with what lint says.
type inertPorts struct {
	fakePorts
	e      *Engine
	lint   macports.Lint
	linted []string
}

var portfileLine = regexp.MustCompile(`(?m)^(version|maintainers) +(.*)$`)

func (p *inertPorts) Family(ctx context.Context, source model.Source, directory string) (macports.Snapshot, error) {
	files, err := p.e.Repo.ReadFiles(ctx, string(source.Tree), []string{directory + "/Portfile"})
	if err != nil {
		return macports.Snapshot{}, err
	}
	ports := map[string]macports.PortInfo{}
	for _, info := range p.directories[directory] {
		info.Options = map[string]string{}
		for _, match := range portfileLine.FindAllStringSubmatch(string(files[directory+"/Portfile"]), -1) {
			if match[1] == "version" {
				info.Version = match[2]
			} else {
				info.Options[macports.MaintainersOption] = match[2]
			}
		}
		ports[info.Name] = info
	}
	return macports.Snapshot{Source: source, Target: model.Target{Name: p.directories[directory][0].Name, Portfile: directory + "/Portfile"}, Ports: ports, Root: "/root"}, nil
}

func (p *inertPorts) Lint(_ context.Context, _ model.Source, directory, port string, _ bool) (macports.Lint, error) {
	p.linted = append(p.linted, directory+" "+port)
	return p.lint, nil
}

// A check after an edit that builds nothing differently, an openmaintainer
// line in libharbor's Portfile, reuses every build, starting nothing, and
// its result says what changed since and takes what lint says of the
// Portfile now, run on this Mac; the pull request's Tested on and the
// check's report say it (the omnidiff session's note 8). A license line
// builds again.
func TestACheckIsCarriedOverAnEditThatBuildsNothingDifferently(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	tools := model.ActivePort{Name: "harbor-tools", Spec: "@1_0", Directory: "graphics/harbor-tools", Archive: "sha256:aa"}
	provider := &identified{scriptedProvider: scriptedProvider{active: []model.ActivePort{tools}}, identity: "origin a"}
	e.Providers = map[string]buildenv.Provider{"command": provider}
	queued := queuedHarborRun(t, e, tahoeArm)
	ports := &inertPorts{fakePorts: harborPorts(), e: e, lint: macports.Lint{Warnings: []string{"Using full email address for no/open maintainer"}}}
	usePorts(e, ports)
	first, err := e.Drive(t.Context(), session(t, e), queued.ID)
	require.NoError(t, err)
	require.Equal(t, model.RunPassed, first.State, first.Detail)
	require.Len(t, provider.jobs, 1)

	var branch model.Branch
	require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
		if branch, err = r.Branch(first.Branch); err != nil {
			return err
		}
		// The base the first check's files were captured on, as
		// harborBranch gave it.
		revision, err := r.Revision(first.Revision)
		branch.Base = revision.Source.Base
		return err
	}))
	check := func(files map[string]string) model.Run {
		t.Helper()
		testsupport.WriteFiles(t, branch.Worktree, files)
		capture, err := e.Capture(t.Context(), CaptureRequest{Branch: branch, Mode: CaptureWorking})
		require.NoError(t, err)
		plan, err := e.PlanCheck(t.Context(), PlanRequest{Revision: capture.Revision, Environments: []model.Environment{tahoeArm}})
		require.NoError(t, err)
		queued, err := e.Enqueue(t.Context(), branch, plan, model.OriginPerson)
		require.NoError(t, err)
		run, err := e.Drive(t.Context(), session(t, e), queued.ID)
		require.NoError(t, err)
		require.Equal(t, model.RunPassed, run.State, run.Detail)
		return run
	}

	second := check(map[string]string{"devel/libharbor/Portfile": "name libharbor\nversion 4\nmaintainers openmaintainer\n"})
	require.Len(t, provider.jobs, 1, "nothing was built")
	require.Equal(t, []string{"devel/libharbor libharbor"}, ports.linted, "lint ran again on the Portfile that changed")
	evidence, err := e.RunEvidence(t.Context(), second.ID)
	require.NoError(t, err)
	var carried *model.Carried
	for _, target := range evidence.Targets {
		if target.Target.ID == "libharbor" {
			carried = target.Outcomes[0].Carried
			require.Equal(t, []string{"Using full email address for no/open maintainer"}, target.Outcomes[0].Lint, "lint's lines, the Portfile's now")
		} else {
			require.Nil(t, target.Outcomes[0].Carried, "%s's directory is the same", target.Target.ID)
		}
	}
	require.NotNil(t, carried)
	require.Equal(t, []model.InertPort{{Port: "libharbor", Options: []string{"maintainers"}}}, carried.Ports)
	require.Equal(t, first.Name(), carried.Check)
	line := second.Name() + " reused " + first.Name() + "'s builds: since then only libharbor's maintainers changed, which builds nothing differently, and lint ran again on this Portfile"
	if carried.Commit != "" {
		line = second.Name() + " reused " + first.Name() + "'s builds (commit " + carried.Commit.Short() + "): since then only libharbor's maintainers changed, which builds nothing differently, and lint ran again on this Portfile"
	}
	require.Equal(t, []string{line}, CarriedLines(evidence))
	body := pullRequestBody(bodyFacts{Evidence: &evidence})
	require.Contains(t, body, line+".")

	check(map[string]string{"devel/libharbor/Portfile": "name libharbor\nversion 4\nmaintainers openmaintainer\nlicense BSD\n"})
	require.Len(t, provider.jobs, 2, "a license line builds again")
}
