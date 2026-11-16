package eval

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portedit/observe"
	"github.com/herbygillot/dockhand/internal/macports/workspace"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/stretchr/testify/require"
)

// Existing verdict fixtures go through the production Session, rather than
// passing a raw evaluator observation without its experiment evidence.
func observeForVerdict(t *testing.T, e *Evaluator, bound macports.Context, request macports.ObservationRequest) (macports.Observation, error) {
	t.Helper()
	s := productObservationSession(t, e, bound)
	data, err := os.ReadFile(filepath.Join(bound.Root(), bound.Target().Portfile))
	require.NoError(t, err)
	return s.One(t.Context(), data, request, request.SelectedOnly)
}
func productObservationSession(t *testing.T, e *Evaluator, bound macports.Context) *observe.Session {
	t.Helper()
	w, err := workspace.Adopt(bound.Root(), bound.Tree.Source())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, w.Close()) })
	// Bind against the interpreter's native platform, not a guessed host.
	native, err := e.NativePlatform(t.Context())
	require.NoError(t, err)
	return &observe.Session{Ports: e, Primary: bound.Target(), Target: bound.Target(), Native: native, Project: func(context.Context, []byte) (*workspace.Workspace, error) { return w, nil }}
}

func TestMachineReadProductFixtureGate(t *testing.T) {
	for _, f := range []struct{ name, body, want, field string }{
		{"set-field", `set distfiles x-${build.jobs}.tgz`, "differs", "distfiles"},
		{"braced-script", `build.args [eval {distfiles x-${build.jobs}.tgz}]`, "differs", "distfiles"},
		{"output-binding", "catch {return ${build.jobs}} m\ndistfiles x-$m.tgz", "differs", "distfiles"},
		{"procedure-argument", "proc p {v} {global build.jobs; build.args ${build.jobs}; distfiles x-$v.tgz; return {}}\nbuild.args [p ${build.jobs}]", "differs", "distfiles"},
		{"prefix-command", "proc build.foo {v} {distfiles x-$v.tgz}\nbuild.foo ${build.jobs}", "differs", "distfiles"},
		{"registration", `interp eval {} {distfiles x-${build.jobs}.tgz}`, "differs", "distfiles"},
		{"computed-name", `distfiles x-[set [string cat build .jobs]].tgz`, "differs", "distfiles"},
		{"control-exit", "set m ${build.jobs}\nif {$m > 8} {return}\ndistfiles later.tgz", "differs", "distfiles"},
		{"exact-value", "set m ${build.jobs}\nif {$m == 17} {return}\ndistfiles later.tgz", "same", ""},
		{"build-only", `build.args -j${build.jobs}`, "same, build-only moved", ""},
		{"epoch", `epoch ${build.jobs}`, "differs", "epoch"},
		{"custom-fetch", `if {${build.jobs} > 8} {fetch {return}}`, "differs", "phase registrations"},
		{"checksum-hook", `build.args -j${build.jobs}; pre-checksum {return}`, "unexplained", ""},
	} {
		t.Run(f.name, func(t *testing.T) {
			e := liveEvaluator(t)
			tree := fixtureTree(t)
			data := []byte("PortSystem 1.0\nname fixture\nversion 1\ncategories devel\nlicense MIT\nmaster_sites https://example.invalid\nchecksums sha256 " + strings.Repeat("a", 64) + " size 2\nbuild.cmd make\nconfigure.cmd ./configure\n" + f.body + "\n")
			putFile(t, tree.Root(), "devel/fixture/Portfile", string(data))
			bound, err := tree.Select(model.Target{Name: "fixture", Portfile: "devel/fixture/Portfile"})
			require.NoError(t, err)
			s := productObservationSession(t, e, bound)
			s.Baseline = data
			request := macports.ObservationRequest{Platform: model.Platform{OS: "darwin", Version: "24", Architecture: "x86_64"}, Declarations: true, SelectedOnly: true}
			got, err := s.One(t.Context(), data, request, true)
			require.NoError(t, err)
			p := got.Ports["fixture"]
			require.NotNil(t, p.MachineReadExperiment)
			summary := p.MachineReadExperiment
			t.Logf("gate %s: %s field=%s varied=%v moved=%v", f.name, summary.Outcome, summary.Field, summary.Varied, summary.BuildOnlyMoved)
			require.Equal(t, f.want, summary.Outcome)
			require.Equal(t, f.field, summary.Field)
			if f.name == "checksum-hook" {
				require.Equal(t, "checksum-phase-unread", summary.ErrorCode)
			}
			_, left := observe.Tolerate(t.Context(), p, data, tree.Root())
			require.Equal(t, f.want == "differs" || f.want == "unexplained", left)
			cached, err := s.One(t.Context(), data, request, true)
			require.NoError(t, err)
			require.Equal(t, got, cached)
			// The original/cache/JSON retain neither stand-in paths nor stand-in numbers.
			encoded, err := json.Marshal(cached)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "/dockhand-standin/")
			require.NotContains(t, string(encoded), "1099511627776")
		})
	}
}

func TestMachineReadProductMixedVariedAndUnvaried(t *testing.T) {
	e := liveEvaluator(t)
	tree := fixtureTree(t)
	data := "PortSystem 1.0\nname fixture\nversion 1\nbuild.cmd make\nconfigure.cmd ./configure\nbuild.args -j${build.jobs} ${use_xcode}\n"
	putFile(t, tree.Root(), "devel/fixture/Portfile", data)
	bound, err := tree.Select(model.Target{Name: "fixture", Portfile: "devel/fixture/Portfile"})
	require.NoError(t, err)
	got, err := observeForVerdict(t, e, bound, macports.ObservationRequest{Platform: model.Platform{OS: "darwin", Version: "9", Architecture: "i386"}, Declarations: true})
	require.NoError(t, err)
	p := got.Ports["fixture"]
	require.NotNil(t, p.MachineReadExperiment)
	require.NotEmpty(t, p.MachineReadExperiment.Varied)
	var unvaried int
	for id, d := range p.Declarations {
		if d.Command == "dockhand.host-access" {
			t.Logf("read %d: %v", id, d.Values)
		}
		if d.Command == "dockhand.host-access" && len(d.Values) > 3 && strings.Contains(d.Values[3], "libxcselect.dylib") {
			unvaried++
			require.NotContains(t, p.MachineReadExperiment.Varied, id)
		}
	}
	require.Positive(t, unvaried)
	_, left := observe.Tolerate(t.Context(), p, []byte(data), tree.Root())
	require.True(t, left)
}

type countedMachineObserver struct {
	*Evaluator
	ordinary, experiments int
	request               macports.ObservationRequest
}

func (c *countedMachineObserver) Observe(ctx context.Context, source macports.Context, request macports.ObservationRequest) (macports.Observation, error) {
	c.ordinary++
	c.request = request
	return c.Evaluator.Observe(ctx, source, request)
}
func (c *countedMachineObserver) MachineReadExperiments(ctx context.Context, source macports.Context, request macports.ObservationRequest, before macports.Observation, candidates map[string][]int, acted []string) map[string]macports.MachineReadExperiment {
	c.experiments++
	// A changed request would make the comparison evidence unusable.
	if !reflect.DeepEqual(c.request, request) {
		panic("experiment request changed")
	}
	return c.Evaluator.MachineReadExperiments(ctx, source, request, before, candidates, acted)
}
func TestMachineExperimentSharedBeforeOneAndObserveCache(t *testing.T) {
	e := liveEvaluator(t)
	tree := fixtureTree(t)
	data := []byte("PortSystem 1.0\nname fixture\nversion 1\nbuild.cmd make\nconfigure.cmd ./configure\nbuild.args -j${build.jobs}\n")
	putFile(t, tree.Root(), "devel/fixture/Portfile", string(data))
	bound, err := tree.Select(model.Target{Name: "fixture", Portfile: "devel/fixture/Portfile"})
	require.NoError(t, err)
	s := productObservationSession(t, e, bound)
	s.Baseline = data
	counter := &countedMachineObserver{Evaluator: e}
	s.Ports = counter
	platform := model.Platform{OS: "darwin", Version: "24", Architecture: "x86_64"}
	one, err := s.One(t.Context(), data, macports.ObservationRequest{Platform: platform, Declarations: true}, false)
	require.NoError(t, err)
	many, err := s.Observe(t.Context(), data, []model.Platform{platform}, true, false)
	require.NoError(t, err)
	require.Equal(t, one, many[0])
	require.Equal(t, 1, counter.ordinary)
	require.Equal(t, 1, counter.experiments)
	// A fresh Session's multi-profile entry point also runs the helper.
	s = s.WithBaseline(data)
	_, err = s.Observe(t.Context(), data, []model.Platform{platform}, true, false)
	require.NoError(t, err)
	require.Equal(t, 2, counter.ordinary)
	require.Equal(t, 2, counter.experiments)
}
