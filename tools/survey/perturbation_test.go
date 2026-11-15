package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portedit/observe"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/tcl/rpc"
	"github.com/stretchr/testify/require"
)

func TestProbeComparesEverySpecifiedField(t *testing.T) {
	original := makeProbePlan(macports.PortInfo{Options: map[string]string{"build.cmd": "make", "configure.args": "-j1"}}, macports.PortObservation{
		Distfiles:    []macports.Distfile{{Name: "x.tgz", URLs: []string{"https://example.invalid/x.tgz"}}},
		Declarations: []macports.Declaration{{Command: "checksums", Values: []string{"sha256", "abc"}}, {Command: "revision", Values: []string{"0"}}},
	})
	for _, c := range []struct {
		name, field string
		change      func(*probePlan)
	}{
		{"archive name", "distfiles[0].name", func(p *probePlan) { p.Distfiles[0].Name = "y.tgz" }},
		{"expanded locations", "distfiles[0].locations", func(p *probePlan) { p.Distfiles[0].URLs[0] = "https://other.invalid/x.tgz" }},
		{"archive count", "distfiles.length", func(p *probePlan) { p.Distfiles = nil }},
		{"checksum values", "checksums.declarations", func(p *probePlan) { p.Checksums[0][1] = "def" }},
		{"revision values", "revision.declarations", func(p *probePlan) { p.RevisionDeclarations[0][0] = "1" }},
		{"final revision", "revision", func(p *probePlan) { p.Revision = 1 }},
	} {
		t.Run(c.name, func(t *testing.T) {
			raw, err := json.Marshal(original)
			require.NoError(t, err)
			var changed probePlan
			require.NoError(t, json.Unmarshal(raw, &changed))
			c.change(&changed)
			require.Equal(t, c.field, firstProbeDifference(original, changed))
		})
	}
	for _, option := range original.Options {
		t.Run("read option "+option.Name, func(t *testing.T) {
			changed := original
			changed.Options = append([]probeOption{}, original.Options...)
			for i := range changed.Options {
				if changed.Options[i].Name == option.Name {
					changed.Options[i].Value += "changed"
				}
			}
			if observe.ActedOption(option.Name) {
				require.Equal(t, "read-options."+option.Name, firstProbeDifference(original, changed))
				require.Empty(t, buildOnlyProbeChanges(original, changed))
			} else {
				require.Empty(t, firstProbeDifference(original, changed))
				require.Equal(t, []string{option.Name}, buildOnlyProbeChanges(original, changed))
			}
		})
	}
	info := macports.PortInfo{Options: map[string]string{}}
	p := macports.PortObservation{Declarations: []macports.Declaration{{Command: "checksums", Values: []string{"a"}}}}
	a := makeProbePlan(info, p)
	p.Declarations[0].Frames = []macports.SourceFrame{{File: "elsewhere", Line: 99}}
	p.Declarations = append(p.Declarations, macports.Declaration{Command: "dockhand.host-access", Values: []string{"host", "standin"}})
	p.Ledger = []macports.LedgerEntry{{Source: "standin", Count: 1}}
	p.Problems = []string{"host event"}
	require.Empty(t, firstProbeDifference(a, makeProbePlan(info, p)))
	// Absence and a failed option read are also different from an empty value.
	info.Options["patchfiles"] = ""
	require.Equal(t, "read-options.patchfiles", firstProbeDifference(a, makeProbePlan(info, p)))
	delete(info.Options, "patchfiles")
	info.OptionErrors = map[string]string{"patchfiles": "missing"}
	require.Equal(t, "read-options.patchfiles", firstProbeDifference(a, makeProbePlan(info, p)))
}

func TestProbeRetainsErrorsWithoutCallingADeadWorkerADifference(t *testing.T) {
	for _, c := range []struct {
		err                 error
		status, field, code string
	}{
		{rpc.CallError{Msg: "Portfile failed"}, "differs", "evaluation", "check-inconclusive"},
		{rpc.ErrBrokenSession, "failed", "", "evaluation-unavailable"},
		{macports.ErrStartup, "failed", "", "check-inconclusive"},
	} {
		r := &probeResult{}
		probeFailure(r, c.err)
		require.Equal(t, c.status, r.Status)
		require.Equal(t, c.field, r.Field)
		require.Equal(t, c.code, r.ErrorCode)
	}
}

func TestProbeRecordRetainsBothPlansAndNamedFindings(t *testing.T) {
	root, source := recordTree(t)
	rec, err := OpenRecord(t.Context(), filepath.Join(t.TempDir(), "probe.db"), "current", true, RunInfo{Tree: root}, root, "", nil)
	require.NoError(t, err)
	defer rec.Close()
	require.NoError(t, rec.Begin(source))
	before := makeProbePlan(macports.PortInfo{Options: map[string]string{"patchfiles": "old.patch"}}, macports.PortObservation{})
	after := makeProbePlan(macports.PortInfo{Options: map[string]string{"patchfiles": "new.patch"}}, macports.PortObservation{})
	result := &probeResult{Status: "differs", Field: "read-options.patchfiles", Rules: []observe.Rule{observe.RuleBuildDefault, observe.RuleBaseLookup}, FollowerBenign: true, Seconds: 0.25, Standins: 1, Before: before, After: &after}
	c := &toleranceCapture{Version: toleranceVersion, Target: model.Target{Name: "fixture"}, Probe: result}
	require.NoError(t, rec.Record(Port{Selector: "fixture", observed: []observed{{Capture: c}}}))
	for _, zero := range []struct {
		name                string
		modeled, conclusive bool
	}{
		{"modeled-zero", true, true}, {"native-zero", false, true}, {"already-inconclusive", true, false},
	} {
		probe := &probeResult{Version: probeVersion, Status: probeNotPerturbed, SubstitutionsKnown: true, FollowerBenign: true, Rules: []observe.Rule{observe.RuleBuildDefault}, Before: before, After: &before, UnperturbedClosedReads: []string{"sysctl hw.optional.arm64", "sysctl hw.optional.arm64", "exec uname -m"}}
		cap := &toleranceCapture{Version: toleranceVersion, Target: model.Target{Name: zero.name}, Probe: probe, Pair: tolerancePair{Complete: true, On: observe.Explanation{Inconclusive: !zero.conclusive}}}
		require.NoError(t, rec.Record(Port{Selector: zero.name, observed: []observed{{Modeled: zero.modeled, Capture: cap}}}))
	}
	report, err := rec.measurementReport()
	require.NoError(t, err)
	require.Equal(t, 1, report.Probes.Total.Differs)
	require.Equal(t, 0, report.Probes.Total.Same)
	require.Equal(t, 3, report.Probes.Total.NotPerturbed)
	require.Equal(t, 1, report.Probes.Total.ModeledFollowerNotPerturbed)
	require.Equal(t, map[string]int{"sysctl hw.optional.arm64": 2, "exec uname -m": 2}, report.Probes.UnperturbedClosedReads, "count contexts, exclude originally inconclusive contexts")
	require.Equal(t, 0.25, report.Probes.Total.Seconds)
	require.Equal(t, 1, report.Probes.ByPort["fixture"].Differs)
	require.Equal(t, 1, report.Probes.ByRule[observe.RuleBaseLookup].Differs)
	require.Equal(t, 1, report.Probes.ByRule[observe.RuleBuildDefault].Differs)
	require.Len(t, report.Probes.Findings, 1)
	require.Equal(t, "fixture", report.Probes.Findings[0].Port)
	var packed []byte
	require.NoError(t, rec.db.QueryRow("select payload from tolerance_capture").Scan(&packed))
	raw, err := decompress(packed)
	require.NoError(t, err)
	var round toleranceCapture
	require.NoError(t, json.Unmarshal(raw, &round))
	require.Equal(t, before, round.Probe.Before)
	require.Equal(t, after, *round.Probe.After)
}

func TestProbeReadOptionsPartition(t *testing.T) {
	split := map[string][]string{"acted": {}, "excluded": {}}
	for _, name := range macports.ReadOptions {
		side := "excluded"
		if observe.ActedOption(name) {
			side = "acted"
		}
		split[side] = append(split[side], name)
	}
	require.Contains(t, split["acted"], "patchfiles")
	require.NotContains(t, split["excluded"], "patchfiles")
	raw, err := json.Marshal(split)
	require.NoError(t, err)
	t.Log("ReadOptions split: " + strings.TrimSpace(string(raw)))
}

func TestProbeClassifiesActedBuildOnlyAndUnperturbedChanges(t *testing.T) {
	before := makeProbePlan(macports.PortInfo{Options: map[string]string{"build.cmd": "make", "patchfiles": "old.patch"}}, macports.PortObservation{})
	for _, c := range []struct {
		name, status string
		standins     int
		options      map[string]string
		moved        []string
		field        string
	}{
		{"same", probeSame, 1, map[string]string{"build.cmd": "make", "patchfiles": "old.patch"}, nil, ""},
		{"build only", probeBuildOnlyMoved, 1, map[string]string{"build.cmd": "standin", "patchfiles": "old.patch"}, []string{"build.cmd"}, ""},
		{"patchfiles acted", probeDiffers, 1, map[string]string{"build.cmd": "standin", "patchfiles": "new.patch"}, []string{"build.cmd"}, "read-options.patchfiles"},
		{"no change or substitution", probeNotPerturbed, 0, map[string]string{"build.cmd": "make", "patchfiles": "old.patch"}, nil, ""},
		{"unperturbed drift", probeNotPerturbed, 0, map[string]string{"build.cmd": "make", "patchfiles": "new.patch"}, nil, "read-options.patchfiles"},
	} {
		t.Run(c.name, func(t *testing.T) {
			after := makeProbePlan(macports.PortInfo{Options: c.options}, macports.PortObservation{})
			r := &probeResult{Before: before, After: &after, Standins: c.standins}
			classifyProbePlans(r)
			require.Equal(t, c.status, r.Status)
			require.Equal(t, c.field, r.Field)
			require.Equal(t, c.moved, r.BuildOnlyMoved)
			require.True(t, r.SubstitutionsKnown)
		})
	}
}

func TestProbeTotalsSeparateEveryClassAndModeledNonCoverage(t *testing.T) {
	var counts probeCounts
	for _, p := range []probeSummary{
		{Status: probeSame, Standins: 1, SubstitutionsKnown: true},
		{Status: probeBuildOnlyMoved, Standins: 2, SubstitutionsKnown: true},
		{Status: probeDiffers, Standins: 1, SubstitutionsKnown: true},
		{Status: probeFailed},
		{Status: probeNotPerturbed, SubstitutionsKnown: true, FollowerBenign: true},
	} {
		counts.add(p, true)
	}
	// Native zero-substitution observations are also excluded from same totals.
	counts.add(probeSummary{Status: probeNotPerturbed, SubstitutionsKnown: true, FollowerBenign: true}, false)
	require.Equal(t, 3, counts.Perturbed)
	require.Equal(t, 1, counts.Same)
	require.Equal(t, 1, counts.BuildOnlyMoved)
	require.Equal(t, 1, counts.Differs)
	require.Equal(t, 1, counts.Failed)
	require.Equal(t, 2, counts.NotPerturbed)
	require.Equal(t, 1, counts.ModeledFollowerNotPerturbed)
	require.Equal(t, 1, counts.UnknownSubstitutions)
}
