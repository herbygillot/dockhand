//go:build surveymeasure

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/portedit"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/testsupport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPerturbationFixtureGate(t *testing.T) {
	executable := os.Getenv("DOCKHAND_PILOT_TCLSH")
	if executable == "" {
		executable = testsupport.MacPortsTclsh(t)
	}
	e := &eval.Evaluator{Executable: executable, Recording: true}
	gitRoot, source := recordTree(t)
	db := os.Getenv("DOCKHAND_PROBE_FIXTURE_DB")
	if db == "" {
		db = filepath.Join(t.TempDir(), "fixtures.db")
	} else {
		_, err := os.Stat(db)
		require.True(t, os.IsNotExist(err), "fixture pilot needs a fresh record")
	}
	rec, err := OpenRecord(t.Context(), db, "current", true, RunInfo{Tree: gitRoot, Measurement: "perturbation-fixtures-v1"}, gitRoot, "", nil)
	require.NoError(t, err)
	require.NoError(t, rec.Begin(source))
	for _, f := range []struct{ name, body, want string }{
		{"set-field", `set distfiles x-${build.jobs}.tgz`, "differs"},
		{"braced-script", `build.args [eval {distfiles x-${build.jobs}.tgz}]`, "differs"},
		{"output-binding", "catch {return ${build.jobs}} m\ndistfiles x-$m.tgz", "differs"},
		{"procedure-argument", "proc p {v} {global build.jobs; build.args ${build.jobs}; distfiles x-$v.tgz; return {}}\nbuild.args [p ${build.jobs}]", "differs"},
		{"prefix-command", "proc build.foo {v} {distfiles x-$v.tgz}\nbuild.foo ${build.jobs}", "differs"},
		{"registration", `interp eval {} {distfiles x-${build.jobs}.tgz}`, "differs"},
		{"computed-name", `distfiles x-[set [string cat build .jobs]].tgz`, "differs"},
		{"control-exit", "set m ${build.jobs}\nif {$m > 8} {return}\ndistfiles later.tgz", "differs"},
		{"exact-value", "set m ${build.jobs}\nif {$m == 17} {return}\ndistfiles later.tgz", "same"},
	} {
		t.Run(f.name, func(t *testing.T) {
			root := t.TempDir()
			name := "probe-" + f.name
			target := model.Target{Name: name, Portfile: "devel/" + name + "/Portfile"}
			// Hold unrelated exported tool options fixed. V is the CPU-derived
			// build.jobs value; otherwise a changed build.cmd would mask the exact
			// value fixture's deliberately missed branch in the all-options compare.
			data := []byte("PortSystem 1.0\nname " + name + "\nversion 1\ncategories devel\nlicense MIT\nmaster_sites https://example.invalid\nchecksums sha256 " + strings.Repeat("a", 64) + " size 2\nbuild.cmd make\nconfigure.cmd ./configure\n" + f.body + "\n")
			file := filepath.Join(root, target.Portfile)
			require.NoError(t, os.MkdirAll(filepath.Dir(file), 0700))
			require.NoError(t, os.WriteFile(file, data, 0600))
			tree, err := macports.NewTree(model.Source{Tree: model.ObjectID(strings.Repeat("a", 40))}, root, model.Platform{})
			require.NoError(t, err)
			bound, err := tree.Select(target)
			require.NoError(t, err)
			profile := model.Platform{OS: "darwin", Version: "24", Architecture: "x86_64"}
			got, err := e.Observe(t.Context(), bound, macports.ObservationRequest{Platform: profile, Declarations: true, SelectedOnly: true})
			require.NoError(t, err)
			info := got.Snapshot.Ports[name]
			p := got.Ports[name]
			groups, known := info.LoadedPortGroups()
			require.True(t, known)
			cap, err := captureTolerance(t.Context(), target, profile, true, "", p, data, root, groups)
			require.NoError(t, err)
			pair := cap.Pair
			cap.Probe = perturbContext(t.Context(), e, got.Snapshot.Source, got.Snapshot.Runtime.Platform, info, cap)
			require.NotNil(t, cap.Probe)
			require.NoError(t, rec.Record(Port{Selector: name, portfile: target.Portfile, Assessment: portedit.Assessment{Outcome: "fixture"}, observed: []observed{{Platform: profile, Modeled: true, Root: root, Info: info, Observation: p, Capture: cap}}}))
			var after []macports.Distfile
			if cap.Probe.After != nil {
				after = cap.Probe.After.Distfiles
			}
			t.Logf("gate: on-inconclusive=%v probe=%s field=%s code=%s standins=%d seconds=%.4f before=%v after=%v", pair.On.Inconclusive, cap.Probe.Status, cap.Probe.Field, cap.Probe.ErrorCode, cap.Probe.Standins, cap.Probe.Seconds, cap.Probe.Before.Distfiles, after)
			assert.Equal(t, pair, cap.Pair, "the experiment cannot change the ordinary verdict")
			assert.False(t, pair.On.Inconclusive, "the fixture exercises an accepted follower shape")
			assert.True(t, cap.Probe.FollowerBenign)
			assert.Greater(t, cap.Probe.Standins, 0)
			assert.Equal(t, f.want, cap.Probe.Status)
		})
	}
	require.NoError(t, rec.Close())
}
