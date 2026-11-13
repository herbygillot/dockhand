//go:build surveymeasure

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portedit"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/tcl/rpc"

	"github.com/stretchr/testify/require"
)

func TestToleranceCaptureReplaysWithoutItsTreeOrBase(t *testing.T) {
	root := filepath.Join(t.TempDir(), "tree")
	base := filepath.Join(t.TempDir(), "base")
	put := func(file, text string) {
		require.NoError(t, os.MkdirAll(filepath.Dir(file), 0o700))
		require.NoError(t, os.WriteFile(file, []byte(text), 0o600))
	}
	put(filepath.Join(base, "port1.0/portbuild.tcl"), "default build.cmd {[portbuild::build_getmaketype]}\n")
	put(filepath.Join(base, "port1.0/portutil.tcl"), "proc default_check {args} {}\n")
	data := []byte("PortSystem 1.0\nname fixture\nversion 1\nbuild.args ${build.cmd}\n")
	put(filepath.Join(root, "devel/fixture/Portfile"), string(data))
	problem := "modeled context depends on make, which findBinary looks for along this Mac's PATH"
	p := macports.PortObservation{ModeledHostAccess: true, BaseLibrary: base, Problems: []string{problem}, Declarations: []macports.Declaration{{Command: "dockhand.host-access", Values: []string{problem, "closed", "base", "findBinary make"}, Frames: []macports.SourceFrame{
		{Command: "default_check build.cmd {} read"},
		{File: filepath.Join(base, "port1.0/portutil.tcl"), Command: "uplevel #0 [list subst [dict get $option_defaults $optionName]]"},
		{Command: "subst {[portbuild::build_getmaketype]}"},
		{File: filepath.Join(base, "port1.0/portbuild.tcl"), Command: "return [findBinary make /usr/bin/make]"},
		{Command: "findBinary make /usr/bin/make"},
	}}}}
	c, err := captureTolerance(t.Context(), model.Target{Name: "fixture", Portfile: "devel/fixture/Portfile"}, model.Platform{OS: "darwin", Version: "25", Architecture: "x86_64"}, true, "", p, data, root)
	require.NoError(t, err)
	require.False(t, c.Pair.On.Inconclusive)
	require.True(t, c.Pair.Off.Inconclusive)
	gitRoot, source := recordTree(t)
	database := filepath.Join(t.TempDir(), "capture.db")
	journal, err := OpenJournal(database + ".jsonl")
	require.NoError(t, err)
	rec, err := OpenRecord(t.Context(), database, "current", true, RunInfo{Tree: gitRoot, Measurement: "capture-test-v1"}, gitRoot, "", journal)
	require.NoError(t, err)
	require.NoError(t, rec.Begin(source))
	require.NoError(t, rec.Record(Port{Selector: "fixture", portfile: "devel/fixture/Portfile", Assessment: portedit.Assessment{Outcome: portedit.InputFound}, observed: []observed{{Platform: c.Platform, Modeled: true, Root: root, Observation: p, Capture: c}}}))
	var encoded []byte
	require.NoError(t, rec.db.QueryRow("select payload from tolerance_capture limit 1").Scan(&encoded))
	decoded, err := decompress(encoded)
	require.NoError(t, err)
	var round toleranceCapture
	require.NoError(t, json.Unmarshal(decoded, &round))
	require.Equal(t, p, round.Observation, "the raw observation, including anonymous frames, survives storage")
	require.NoError(t, rec.Close())
	require.NoError(t, journal.Close())
	journalData, err := os.ReadFile(database + ".jsonl")
	require.NoError(t, err)
	var recorded Port
	require.NoError(t, json.Unmarshal(bytes.Split(bytes.TrimSpace(journalData), []byte("\n"))[1], &recorded))
	require.Len(t, recorded.Tolerance, 1)
	require.Equal(t, int64(1), recorded.Tolerance[0].Evaluation)
	require.True(t, recorded.Tolerance[0].Complete)
	require.NoError(t, os.Rename(root, root+"-absent"))
	require.NoError(t, os.Rename(base, base+"-absent"))
	replay, dir, err := c.materialize(t.TempDir())
	require.NoError(t, err)
	on, off, err := measurePolicies(t.Context(), replay, data, dir)
	require.NoError(t, err)
	require.Equal(t, c.Pair.On.Inconclusive, on.Inconclusive)
	require.Equal(t, c.Pair.Off.Inconclusive, off.Inconclusive)
	require.Equal(t, c.Pair.On.Accesses, on.Accesses)
	require.Equal(t, c.Pair.Off.Accesses, off.Accesses)
	totals, err := replayTolerance(t.Context(), database)
	require.NoError(t, err)
	require.Equal(t, 1, totals.Contexts)
	require.Equal(t, 1, totals.Parity)
	require.Equal(t, 1, totals.Flips)
	require.Equal(t, 0, totals.OnLeft)
	require.Equal(t, 1, totals.OffLeft)
	require.Positive(t, totals.CaptureBytes)
	require.Positive(t, totals.FileBytes)
	other, err := OpenRecord(t.Context(), database, "current", true, RunInfo{Tree: gitRoot, Measurement: "different-build"}, gitRoot, "", nil)
	require.NoError(t, err)
	require.ErrorContains(t, other.Begin(source), "measurement settings differ")
	require.NoError(t, other.Close())
	retry, err := OpenRecord(t.Context(), database, "current", true, RunInfo{Tree: gitRoot, Measurement: "capture-test-v1"}, gitRoot, "", nil)
	require.NoError(t, err)
	require.NoError(t, retry.Begin(source))
	extra := []byte("set extra 1\n")
	c.Files["source/additional.tcl"] = capturedFile{Hash: digest(extra), Data: extra}
	_, err = retry.db.Exec(`CREATE TRIGGER fail_capture BEFORE INSERT ON tolerance_capture BEGIN SELECT RAISE(ABORT,'capture fixture fault'); END`)
	require.NoError(t, err)
	next := Port{Selector: "retry-fixture", portfile: "devel/fixture/Portfile", observed: []observed{{Platform: c.Platform, Modeled: true, Root: root, Observation: p, Capture: c}}}
	require.ErrorContains(t, retry.Record(next), "capture fixture fault")
	_, err = retry.db.Exec(`DROP TRIGGER fail_capture`)
	require.NoError(t, err)
	require.NoError(t, retry.Record(next))
	require.NoError(t, retry.Close())
	totals, err = replayTolerance(t.Context(), database)
	require.NoError(t, err)
	require.Equal(t, 2, totals.Contexts, "a rolled-back file-cache entry must not hide missing file bytes")
}

func TestMeasurementKeepsADeadWorkerOutOfParity(t *testing.T) {
	root, source := recordTree(t)
	rec, err := OpenRecord(t.Context(), filepath.Join(t.TempDir(), "worker.db"), "current", true, RunInfo{Tree: root}, root, "", nil)
	require.NoError(t, err)
	require.NoError(t, rec.Begin(source))
	require.NoError(t, rec.Record(Port{Selector: "worker", observed: []observed{{Err: rpc.ErrBrokenSession}}}))
	totals, err := rec.measurementReport()
	require.NoError(t, err)
	require.Equal(t, 1, totals.Unavailable)
	require.Equal(t, 1, totals.Missing)
	require.Zero(t, totals.Parity)
	require.NoError(t, rec.Close())
}
