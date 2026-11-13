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

	"github.com/stretchr/testify/require"
)

// These are explicit synthetic measurement fixtures, not claimed corpus
// ports. Their assertions hold capture/parity/replay, not the known gaps.
func TestToleranceBypassFamilyCaptures(t *testing.T) {
	executable := os.Getenv("DOCKHAND_PILOT_TCLSH")
	if executable == "" {
		executable = testsupport.MacPortsTclsh(t)
	}
	e := &eval.Evaluator{Executable: executable, Recording: true}
	gitRoot, source := recordTree(t)
	database := os.Getenv("DOCKHAND_SURVEY_BYPASS_DB")
	if database == "" {
		database = filepath.Join(t.TempDir(), "bypasses.db")
	} else {
		_, err := os.Stat(database)
		require.True(t, os.IsNotExist(err), "pilot must use a fresh DB")
	}
	rec, err := OpenRecord(t.Context(), database, "current", true, RunInfo{Tree: gitRoot, Measurement: "synthetic-bypass-pilot-v1"}, gitRoot, "", nil)
	require.NoError(t, err)
	require.NoError(t, rec.Begin(source))
	for _, fixture := range []struct{ name, body string }{
		{"set-field", `set distfiles x-${build.cmd}.tgz`},
		{"braced-script", `build.args [eval {distfiles x-${build.cmd}.tgz}]`},
		{"output-binding", "catch {return ${build.cmd}} m\ndistfiles x-$m.tgz"},
		{"procedure-argument", "proc p {v} {global build.cmd; build.args ${build.cmd}; distfiles x-$v.tgz; return {}}\nbuild.args [p ${build.cmd}]"},
		{"prefix-command", "proc build.foo {v} {distfiles x-$v.tgz}\nbuild.foo ${build.cmd}"},
		{"registration", `interp eval {} {distfiles x-${build.cmd}.tgz}`},
		{"computed-name", `distfiles x-[set [string cat build .cmd]].tgz`},
		{"control-exit", "set m ${build.cmd}\nif {$m eq {}} {return}\ndistfiles later.tgz"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			root := t.TempDir()
			name := "follower-" + fixture.name
			target := model.Target{Name: name, Portfile: "devel/" + name + "/Portfile"}
			data := []byte("PortSystem 1.0\nname " + name + "\nversion 1\ncategories devel\nlicense MIT\nmaster_sites https://example.invalid\nchecksums sha256 " + strings.Repeat("a", 64) + " size 2\n" + fixture.body + "\n")
			file := filepath.Join(root, target.Portfile)
			require.NoError(t, os.MkdirAll(filepath.Dir(file), 0o700))
			require.NoError(t, os.WriteFile(file, data, 0o600))
			tree, err := macports.NewTree(model.Source{Tree: model.ObjectID(strings.Repeat("a", 40))}, root, model.Platform{})
			require.NoError(t, err)
			bound, err := tree.Select(target)
			require.NoError(t, err)
			profile := model.Platform{OS: "darwin", Version: "24", Architecture: "x86_64"}
			got, err := e.Observe(t.Context(), bound, macports.ObservationRequest{Platform: profile, Declarations: true, SelectedOnly: true})
			require.NoError(t, err)
			cap, err := captureTolerance(t.Context(), target, profile, true, "", got.Ports[name], data, root)
			require.NoError(t, err)
			t.Logf("normal inconclusive=%v off=%v distfiles=%q", cap.Pair.On.Inconclusive, cap.Pair.Off.Inconclusive, got.Snapshot.Ports[name].Options["distfiles"])
			require.NoError(t, rec.Record(Port{Selector: name, portfile: target.Portfile, Assessment: portedit.Assessment{Outcome: "fixture"}, observed: []observed{{Platform: profile, Modeled: true, Root: root, Info: got.Snapshot.Ports[name], Observation: got.Ports[name], Capture: cap}}}))
		})
	}
	require.NoError(t, rec.Close())
	totals, err := replayTolerance(t.Context(), database)
	require.NoError(t, err)
	require.Equal(t, 8, totals.Contexts)
	require.Equal(t, 8, totals.Parity)
}
