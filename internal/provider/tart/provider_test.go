package tart

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/tart/channel"
)

// fakeMac stands for the Mac's Tart: its images, what runs, and a guest
// whose results a test scripts.
type fakeMac struct {
	mu       sync.Mutex
	images   []string
	cloneErr error
	running  []int // counts Running reports, one per call, the last repeating
	events   []string
	guest    *fakeGuest
	run      *fakeRun
}

func (m *fakeMac) log(event string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, event)
}
func (m *fakeMac) Images(context.Context) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.images), nil
}
func (m *fakeMac) Running(context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.running) == 0 {
		return 0, nil
	}
	count := m.running[0]
	if len(m.running) > 1 {
		m.running = m.running[1:]
	}
	return count, nil
}
func (m *fakeMac) Clone(_ context.Context, image, vm string) error {
	m.log("clone " + image + " " + vm)
	if m.cloneErr != nil {
		return m.cloneErr
	}
	m.mu.Lock()
	m.images = append(m.images, vm)
	m.mu.Unlock()
	return nil
}
func (m *fakeMac) Start(vm string) (run, error) {
	m.log("start " + vm)
	return m.run, nil
}
func (m *fakeMac) Stop(_ context.Context, vm string) error { m.log("stop " + vm); return nil }
func (m *fakeMac) Delete(_ context.Context, vm string) error {
	m.log("delete " + vm)
	m.mu.Lock()
	m.images = slices.DeleteFunc(m.images, func(name string) bool { return name == vm })
	m.mu.Unlock()
	return nil
}
func (m *fakeMac) Reach(_ context.Context, vm, image string) (guest, error) {
	m.log("reach " + vm + " as " + image)
	return m.guest, nil
}

type fakeRun struct {
	done    chan struct{}
	stopped bool
}

func (r *fakeRun) Done() <-chan struct{}                     { return r.done }
func (r *fakeRun) Err() error                                { return errors.New("it crashed") }
func (r *fakeRun) Stop(context.Context, time.Duration) error { r.stopped = true; return nil }

// fakeGuest answers the provider's commands. Each read of results.json
// gives the next of results, the last repeating; exited says whether the
// guest program's launchd job has exited.
type fakeGuest struct {
	mu       sync.Mutex
	results  []guestResults
	exited   bool
	uploaded string
	input    guestInput
	logs     map[string]string
}

func (g *fakeGuest) Command(_ context.Context, _ io.Reader, args ...string) ([]byte, error) {
	if slices.Contains(args, "launchctl") || slices.Contains(args, "/bin/launchctl") {
		g.mu.Lock()
		defer g.mu.Unlock()
		if g.exited {
			return []byte("state = not running"), nil
		}
		return []byte("state = running"), nil
	}
	return nil, nil
}
func (g *fakeGuest) Upload(_ context.Context, local, path string, _ bool) error {
	g.uploaded = path
	data, err := os.ReadFile(local)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &g.input)
}
func (g *fakeGuest) Read(_ context.Context, path string, _ bool) ([]byte, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if strings.HasSuffix(path, "runner.log") {
		return []byte("the runner's last words"), nil
	}
	if len(g.results) == 0 {
		return nil, channel.ErrTransport
	}
	next := g.results[0]
	if len(g.results) > 1 {
		g.results = g.results[1:]
	}
	return json.Marshal(next)
}
func (g *fakeGuest) Download(_ context.Context, path, local string, _ bool) error {
	return os.WriteFile(local, []byte(g.logs[filepath.Base(path)]), 0o644)
}
func (g *fakeGuest) Close(context.Context) {}

// fakeBuild records what the provider records.
type fakeBuild struct {
	mu       sync.Mutex
	blocked  map[model.TargetID]bool
	results  []model.TargetResult
	progress []string
	observed []model.Observed
	refs     []string
}

func (b *fakeBuild) Refer(ref string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refs = append(b.refs, ref)
	return nil
}

func (b *fakeBuild) Observe(observed model.Observed) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.observed = append(b.observed, observed)
	return nil
}

func (b *fakeBuild) Blocked(target model.TargetID) (model.TargetID, bool) {
	return "", b.blocked[target]
}
func (b *fakeBuild) Record(result model.TargetResult) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.results = append(b.results, result)
	return nil
}
func (b *fakeBuild) Progress(message string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.progress = append(b.progress, message)
}
func (b *fakeBuild) Canceled() bool { return false }

var tahoe = model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}

func tartJob(t *testing.T, attempt int) engine.Job {
	return engine.Job{
		Run:         model.Run{ID: "run_7", Number: 3},
		Execution:   model.GuestExecution{ID: "ex_1", Attempt: attempt},
		Plan:        model.Plan{Tests: model.TestsDeclared},
		Environment: model.Environment{Provider: "tart", Platform: tahoe},
		Directory:   t.TempDir(),
		Targets: []model.PlanTarget{
			{ID: "libharbor", Target: model.Target{Name: "libharbor", Portfile: "devel/libharbor/Portfile"}},
			{ID: "harbor-cli", Target: model.Target{Name: "harbor", Subport: "harbor-cli", Portfile: "devel/harbor/Portfile"}, DependsOn: []model.TargetID{"libharbor"}},
		},
	}
}

// testProvider builds on a fake Mac, staging only the guest's input.
func testProvider(mac *fakeMac) *Provider {
	return &Provider{machine: mac, Poll: time.Millisecond, host: 25,
		stager: func(_ context.Context, _ engine.Job, input guestInput, archive string) error {
			data, err := json.Marshal(input)
			if err != nil {
				return err
			}
			return os.WriteFile(archive, data, 0o644)
		}}
}

func newMac(results ...guestResults) *fakeMac {
	for i := range results {
		results[i].Protocol = Protocol
	}
	return &fakeMac{images: []string{"dockhand-base-tahoe"}, run: &fakeRun{done: make(chan struct{})},
		guest: &fakeGuest{results: results, logs: map[string]string{"target-1.log": "built libharbor", "target-2.log": "built harbor-cli"}}}
}

// Each target's result is recorded as the guest's results grow, with its
// log copied out, and the clone is deleted after.
func TestTheProviderRecordsEachTargetAsTheGuestFinishesIt(t *testing.T) {
	t.Parallel()
	libharbor := guestResult{ID: "libharbor", Outcome: "passed", Tests: "passed", Log: "target-1.log"}
	cli := guestResult{ID: "harbor-cli", Outcome: "failed", Phase: "install", Log: "target-2.log", Detail: "Failed to install harbor-cli"}
	mac := newMac(
		guestResults{State: "running"},
		guestResults{State: "running", Targets: []guestResult{libharbor}},
		guestResults{State: "finished", Targets: []guestResult{libharbor, cli}},
	)
	job := tartJob(t, 1)
	build := &fakeBuild{}
	require.NoError(t, testProvider(mac).Execute(t.Context(), job, build))

	require.Len(t, build.results, 2)
	require.Equal(t, model.TargetResult{Target: "libharbor", Outcome: model.OutcomePassed, Tests: model.TestsPassed, Log: filepath.Join(job.Directory, "target-1.log")}, build.results[0])
	require.Equal(t, model.TargetResult{Target: "harbor-cli", Outcome: model.OutcomeFailed, Phase: model.PhaseInstall, Tests: model.TestsNone, Log: filepath.Join(job.Directory, "target-2.log")}, build.results[1])
	log, err := os.ReadFile(filepath.Join(job.Directory, "target-1.log"))
	require.NoError(t, err)
	require.Equal(t, "built libharbor", string(log))
	require.Contains(t, build.progress, "harbor-cli: Failed to install harbor-cli")

	vm := "dockhand-check-run-7-tahoe-1"
	require.Equal(t, []string{"clone dockhand-base-tahoe " + vm, "start " + vm, "reach " + vm + " as dockhand-base-tahoe", "delete " + vm}, mac.events)
	require.Equal(t, []string{vm}, build.refs, "the clone is the provider's own name for the run")
	require.True(t, mac.run.stopped)
	require.Equal(t, "harbor-cli", mac.guest.input.Targets[1].Name, "a subport is built by its own name")
	require.Equal(t, []string{"libharbor"}, mac.guest.input.Targets[1].DependsOn)
	require.Equal(t, "declared", mac.guest.input.Tests)
	require.Equal(t, 1800, mac.guest.input.TestTimeout)
	require.NoFileExists(t, filepath.Join(job.Directory, "input.tar"), "the staged archive is removed once it's in the guest")
}

// A target an earlier attempt found blocked goes to the guest marked so.
func TestABlockedTargetGoesToTheGuestMarked(t *testing.T) {
	t.Parallel()
	mac := newMac(guestResults{State: "finished"})
	require.NoError(t, testProvider(mac).Execute(t.Context(), tartJob(t, 2), &fakeBuild{blocked: map[model.TargetID]bool{"harbor-cli": true}}))
	require.False(t, mac.guest.input.Targets[0].Blocked)
	require.True(t, mac.guest.input.Targets[1].Blocked)
}

// A guest that errors, a program that stops without finishing, or a VM
// that stops is trouble with the environment, which the runner tries
// again; what was recorded stays, and the clone goes.
func TestGuestTroubleIsInfrastructure(t *testing.T) {
	t.Parallel()
	passed := guestResult{ID: "libharbor", Outcome: "passed", Log: "target-1.log"}
	for name, test := range map[string]struct {
		mac  *fakeMac
		want string
	}{
		"errored":  {newMac(guestResults{State: "running", Targets: []guestResult{passed}}, guestResults{State: "errored", Detail: "the image already has ports installed"}), "the image already has ports installed"},
		"exited":   {newMac(guestResults{State: "running", Targets: []guestResult{passed}}), "the guest program stopped before it finished: the runner's last words"},
		"protocol": {newMac(guestResults{State: "running", Targets: []guestResult{passed}}), "protocol 9"},
	} {
		t.Run(name, func(t *testing.T) {
			switch name {
			case "exited":
				test.mac.guest.exited = true
			case "protocol":
				test.mac.guest.results[0].Protocol = 9
			}
			build := &fakeBuild{}
			err := testProvider(test.mac).Execute(t.Context(), tartJob(t, 1), build)
			require.ErrorIs(t, err, engine.ErrInfrastructure)
			require.ErrorContains(t, err, test.want)
			if name != "protocol" {
				require.Len(t, build.results, 1, "what the guest finished stays recorded")
			}
			require.Contains(t, test.mac.events, "delete dockhand-check-run-7-tahoe-1")
		})
	}
	mac := newMac(guestResults{State: "running"})
	close(mac.run.done)
	err := testProvider(mac).Execute(t.Context(), tartJob(t, 1), &fakeBuild{})
	require.ErrorIs(t, err, engine.ErrInfrastructure)
	require.ErrorContains(t, err, "the VM stopped")
}

// An attempt removes what an earlier attempt of its run and release left,
// and waits while the Mac already runs its two VMs.
func TestAnAttemptSweepsAndWaitsForRoom(t *testing.T) {
	t.Parallel()
	mac := newMac(guestResults{State: "finished"})
	mac.images = append(mac.images, "dockhand-check-run-7-tahoe-1", "dockhand-check-run-7-sonoma-1", "dockhand-check-run-8-tahoe-1")
	mac.running = []int{2, 2, 1}
	build := &fakeBuild{}
	require.NoError(t, testProvider(mac).Execute(t.Context(), tartJob(t, 2), build))
	require.Equal(t, []string{"stop dockhand-check-run-7-tahoe-1", "delete dockhand-check-run-7-tahoe-1"}, mac.events[:2], "only this run and release's earlier attempt")
	require.Contains(t, mac.images, "dockhand-check-run-7-sonoma-1")
	require.Contains(t, mac.images, "dockhand-check-run-8-tahoe-1")
	require.Contains(t, build.progress, "waiting for the Mac's VMs: 2 are running, and macOS runs two at most")
}

// The clone is named on the execution before it is made, so a process
// that dies while cloning leaves a clone clean can trace to its check.
func TestTheCloneIsNamedBeforeItIsMade(t *testing.T) {
	t.Parallel()
	mac := newMac(guestResults{State: "finished"})
	mac.cloneErr = errors.New("tart clone failed")
	build := &fakeBuild{}
	err := testProvider(mac).Execute(t.Context(), tartJob(t, 1), build)
	require.ErrorIs(t, err, engine.ErrInfrastructure)
	require.Equal(t, []string{"dockhand-check-run-7-tahoe-1"}, build.refs)
}

// The leftovers are the check clones in dockhand's Tart home and nothing
// else, and removing one stops it first. The images checks clone from
// can't be removed this way, whatever is asked.
func TestLeftoversAreOnlyCheckClones(t *testing.T) {
	t.Parallel()
	mac := newMac()
	mac.images = []string{"dockhand-base-tahoe", "dockhand-golden-tahoe", "dockhand-xcode-tahoe", "dockhand-golden-xcode-tahoe",
		"dockhand-base-tahoe-check", "ghcr.io/cirruslabs/macos-tahoe-vanilla:latest", "dockhand-check-run-7-tahoe-1", "dockhand-check-run-8-sonoma-2"}
	p := testProvider(mac)
	leftovers, err := p.Leftovers(t.Context())
	require.NoError(t, err)
	require.Equal(t, []engine.Leftover{
		{Ref: "dockhand-check-run-7-tahoe-1", What: "Tart clone dockhand-check-run-7-tahoe-1"},
		{Ref: "dockhand-check-run-8-sonoma-2", What: "Tart clone dockhand-check-run-8-sonoma-2"},
	}, leftovers)

	require.NoError(t, p.RemoveLeftover(t.Context(), "dockhand-check-run-7-tahoe-1"))
	require.Equal(t, []string{"stop dockhand-check-run-7-tahoe-1", "delete dockhand-check-run-7-tahoe-1"}, mac.events)
	for _, image := range []string{"dockhand-base-tahoe", "dockhand-golden-tahoe", "dockhand-xcode-tahoe", "dockhand-base-tahoe-check"} {
		require.ErrorContains(t, p.RemoveLeftover(t.Context(), image), "is not a check's clone", image)
	}
	require.Len(t, mac.events, 2, "nothing else was stopped or deleted")
	require.Contains(t, mac.images, "dockhand-base-tahoe")
}

// A canceled check stops, and its clone goes.
func TestACanceledCheckStopsItsGuest(t *testing.T) {
	t.Parallel()
	mac := newMac(guestResults{State: "running"})
	ctx, cancel := context.WithCancel(t.Context())
	build := &fakeBuild{}
	go func() {
		for {
			build.mu.Lock()
			started := slices.Contains(build.progress, "building in dockhand-check-run-7-tahoe-1")
			build.mu.Unlock()
			if started {
				cancel()
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()
	err := testProvider(mac).Execute(ctx, tartJob(t, 1), build)
	require.ErrorIs(t, err, context.Canceled)
	require.Contains(t, mac.events, "delete dockhand-check-run-7-tahoe-1")
	require.True(t, mac.run.stopped)
}

// A guest whose tools differ from the facts table's row is reported,
// never judged (decision 10).
func TestDriftFromTheFactsTableIsReported(t *testing.T) {
	t.Parallel()
	facts, ok := macos.Table().Lookup(25, "arm64", macos.ProfileTools)
	require.True(t, ok)
	mac := newMac(guestResults{State: "finished", Environment: map[string]string{"macos": "26.6.2", "build": "25G71", "architecture": "arm64", "tools": "27.0.0.0.1788430756"}})
	build := &fakeBuild{}
	require.NoError(t, testProvider(mac).Execute(t.Context(), tartJob(t, 1), build))
	require.Contains(t, build.progress, "drift: the guest has Command Line Tools 27.0.0.0.1788430756; the facts table has "+facts.Tools+", from "+facts.Source.From+" on "+facts.Source.Date)

	mac = newMac(guestResults{State: "finished", Environment: map[string]string{"macos": "26.6.2", "tools": facts.Tools}})
	build = &fakeBuild{}
	require.NoError(t, testProvider(mac).Execute(t.Context(), tartJob(t, 1), build))
	for _, message := range build.progress {
		require.NotContains(t, message, "drift")
	}

	// An image with Xcode is compared with the Xcode row, which its plan
	// was read with.
	xcode, ok := macos.Table().Lookup(25, "arm64", macos.ProfileXcode)
	require.True(t, ok)
	job := tartJob(t, 1)
	job.Environment.DeveloperTools = model.DeveloperToolsXcode
	mac = newMac(guestResults{State: "finished", Environment: map[string]string{"macos": "26.6.2", "build": "25G71", "architecture": "arm64", "tools": xcode.Tools, "xcode": "26.1", "xcode_build": "17B55"}})
	mac.images = append(mac.images, "dockhand-xcode-tahoe")
	build = &fakeBuild{}
	require.NoError(t, testProvider(mac).Execute(t.Context(), job, build))
	require.Contains(t, build.progress, "drift: the guest has Xcode 26.1; the facts table has "+xcode.Xcode+", from "+xcode.Source.From+" on "+xcode.Source.Date)
	require.Equal(t, []model.Observed{{MacOS: "26.6.2", Build: "25G71", Architecture: "arm64", Xcode: "26.1", XcodeBuild: "17B55", Tools: xcode.Tools}}, build.observed,
		"what the guest reported is kept, for the pull request's Tested on")
}

// --on tart builds on the Mac's own release, tart:all on every release
// with an image, and named releases on those; a release without its base
// image is refused with the command that makes one (decision 6). Xcode is
// an add-on: a release with its Xcode image builds with Xcode, and one
// without it with the Command Line Tools alone.
func TestEnvironmentsAreReleasesWithImages(t *testing.T) {
	t.Parallel()
	mac := newMac()
	mac.images = []string{"dockhand-base-tahoe", "dockhand-xcode-tahoe", "dockhand-base-sonoma", "dockhand-xcode-sequoia"}
	p := testProvider(mac)
	withXcode := model.Environment{Provider: "tart", Platform: tahoe, DeveloperTools: model.DeveloperToolsXcode}
	sonoma := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "23", Architecture: "arm64"}, DeveloperTools: model.DeveloperToolsCommandLine}
	for releases, want := range map[string][]model.Environment{
		"":            {withXcode},
		"all":         {sonoma, withXcode},
		"sonoma,26":   {sonoma, withXcode},
		"tahoe,tahoe": {withXcode},
	} {
		got, err := p.Environments(t.Context(), releases)
		require.NoError(t, err, releases)
		require.Equal(t, want, got, releases)
	}
	_, err := p.Environments(t.Context(), "sequoia")
	require.ErrorContains(t, err, "no Tart image for macOS 15 (Sequoia): dockhand providers setup tart sequoia makes dockhand-base-sequoia", "an Xcode image is an add-on to the base image")
	_, err = p.Environments(t.Context(), "leopard")
	require.ErrorContains(t, err, "unknown release")
}

// A guest that finishes between a read and the look at whether it's still
// running is read once more; what it reported about itself is kept then
// too, since it writes that only with its first result. From prometheus's
// check, whose pull request had lost its macOS and Xcode.
func TestWhatAGuestReportedIsKeptWhenItFinishesBetweenReads(t *testing.T) {
	t.Parallel()
	passed := guestResult{ID: "libharbor", Outcome: "passed", Log: "target-1.log"}
	mac := newMac(
		guestResults{State: "running"},
		guestResults{State: "finished", Environment: map[string]string{"macos": "26.6.2", "build": "25G83", "architecture": "arm64", "xcode": "26.6", "xcode_build": "17F113"},
			Targets: []guestResult{passed, {ID: "harbor-cli", Outcome: "passed", Log: "target-2.log"}}},
	)
	mac.guest.exited = true
	build := &fakeBuild{}
	require.NoError(t, testProvider(mac).Execute(t.Context(), tartJob(t, 1), build))
	require.Len(t, build.results, 2)
	require.Equal(t, []model.Observed{{MacOS: "26.6.2", Build: "25G83", Architecture: "arm64", Xcode: "26.6", XcodeBuild: "17F113"}}, build.observed)
}
