//go:build surveymeasure

package main

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type probeGateContext struct {
	Port       string
	Platform   model.Platform
	Modeled    bool
	Evaluation int64
	probeSummary
	Expected string
	Passed   bool
}

type perturbationGate struct {
	Passed, RealPassed, FixturesPassed bool
	Real, Fixtures                     []probeGateContext
	Missing                            []string
}

// Gate records are opened read-only. This checks measurements already made;
// it never evaluates a Portfile or starts a larger survey.
func readProbeGate(t *testing.T, file string, expected map[string]string, fixtures bool) ([]probeGateContext, []string, bool) {
	t.Helper()
	absolute, err := filepath.Abs(file)
	require.NoError(t, err)
	db, err := sql.Open("sqlite", "file:"+absolute+"?mode=ro&immutable=1")
	require.NoError(t, err)
	defer db.Close()
	rows, err := db.Query(`select c.evaluation_id,c.hash,c.payload,p.name from tolerance_capture c join evaluation e on e.id=c.evaluation_id join port p on p.id=e.port_id order by c.evaluation_id`)
	require.NoError(t, err)
	defer rows.Close()
	var out []probeGateContext
	seen := map[string]int{}
	passed := true
	for rows.Next() {
		var id int64
		var hash, name string
		var packed []byte
		require.NoError(t, rows.Scan(&id, &hash, &packed, &name))
		want, required := expected[name]
		if !required {
			continue
		}
		raw, err := decompress(packed)
		require.NoError(t, err)
		require.Equal(t, hash, digest(raw))
		var c toleranceCapture
		require.NoError(t, json.Unmarshal(raw, &c))
		require.Equal(t, name, c.Target.Name)
		rules, benign := probeRules(&c)
		if len(rules) == 0 && !fixtures {
			continue
		}
		row := probeGateContext{Port: name, Platform: c.Platform, Modeled: c.Modeled, Evaluation: id, Expected: want}
		if c.Probe == nil {
			row.probeSummary = probeSummary{Status: "failed", ErrorCode: "probe-not-recorded", Rules: rules, FollowerBenign: benign}
		} else {
			row.probeSummary = *c.Probe.summary()
		}
		row.Expected, row.Passed = probeGatePass(&c, want, fixtures)
		passed = passed && row.Passed
		seen[name]++
		out = append(out, row)
	}
	require.NoError(t, rows.Err())
	var missing []string
	for name := range expected {
		if seen[name] == 0 || (fixtures && seen[name] != 1) {
			missing = append(missing, name)
			passed = false
		}
	}
	sort.Strings(missing)
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Port != b.Port {
			return a.Port < b.Port
		}
		if a.Platform.Version != b.Platform.Version {
			return a.Platform.Version < b.Platform.Version
		}
		return a.Platform.Architecture < b.Platform.Architecture
	})
	return out, missing, passed
}

// Run this explicitly after the two small records have been made. An absent
// pilot is a skip, never evidence that the release gate passed.
func TestPerturbationPilotGate(t *testing.T) {
	real, fixtures := os.Getenv("DOCKHAND_PROBE_PILOT_DB"), os.Getenv("DOCKHAND_PROBE_GATE_FIXTURES")
	if real == "" || fixtures == "" {
		t.Skip("requires explicit real-port and fixture pilot records")
	}
	requiredReal := map[string]string{}
	for _, p := range []string{"gpsd", "legacy-support", "legacy-support-devel", "alex", "ghc", "ksh93", "wireshark3", "cmake"} {
		requiredReal[p] = "same"
	}
	requiredFixtures := map[string]string{}
	for _, p := range []string{"set-field", "braced-script", "output-binding", "procedure-argument", "prefix-command", "registration", "computed-name", "control-exit"} {
		requiredFixtures["probe-"+p] = "differs"
	}
	requiredFixtures["probe-exact-value"] = "same"
	requiredFixtures["probe-build-only"] = probeBuildOnlyMoved
	var g perturbationGate
	var missing []string
	g.Real, g.Missing, g.RealPassed = readProbeGate(t, real, requiredReal, false)
	g.Fixtures, missing, g.FixturesPassed = readProbeGate(t, fixtures, requiredFixtures, true)
	g.Missing = append(g.Missing, missing...)
	g.Passed = g.RealPassed && g.FixturesPassed
	raw, err := json.MarshalIndent(g, "", "  ")
	require.NoError(t, err)
	if file := os.Getenv("DOCKHAND_PROBE_GATE_REPORT"); file != "" {
		require.NoError(t, os.WriteFile(file, append(raw, '\n'), 0600))
	}
	t.Logf("real gate=%v fixtures gate=%v (%d real contexts, %d fixture contexts)", g.RealPassed, g.FixturesPassed, len(g.Real), len(g.Fixtures))
	assert.True(t, g.Passed, "perturbation pilot gate failed; whole-tree run remains forbidden")
}

// Gate v2 accepts unperturbed contexts as explicit non-evidence. Only
// substituted contexts can satisfy either same class; all fixture probes
// must apply a stand-in, including the known-hole and build-only fixtures.
func probeGatePass(c *toleranceCapture, expected string, fixture bool) (string, bool) {
	p := c.Probe
	if p == nil || p.Version != probeVersion || !p.SubstitutionsKnown || p.After == nil || !c.Pair.Complete || !c.Pair.Parity {
		return expected, false
	}
	if !fixture {
		if p.Standins == 0 {
			expected = probeNotPerturbed
		} else {
			expected = "same or same, build-only moved"
		}
	}
	field := firstProbeDifference(p.Before, *p.After)
	moved := buildOnlyProbeChanges(p.Before, *p.After)
	if field != p.Field || !slices.Equal(moved, p.BuildOnlyMoved) {
		return expected, false
	}
	valid := false
	switch p.Status {
	case probeSame:
		valid = p.Standins > 0 && field == "" && len(moved) == 0
	case probeBuildOnlyMoved:
		valid = p.Standins > 0 && field == "" && len(moved) > 0
	case probeNotPerturbed:
		valid = p.Standins == 0
	case probeDiffers:
		valid = p.Standins > 0 && field != ""
	}
	if fixture {
		return expected, valid && p.Status == expected && p.Standins > 0 && p.FollowerBenign && !c.Pair.On.Inconclusive
	}
	return expected, valid && (p.Status == probeSame || p.Status == probeBuildOnlyMoved || p.Status == probeNotPerturbed)
}

func TestRevisedProbeGateRejectsStaleOrUnsupportedEvidence(t *testing.T) {
	before := makeProbePlan(macports.PortInfo{Options: map[string]string{"build.cmd": "make", "patchfiles": "old"}}, macports.PortObservation{})
	after := makeProbePlan(macports.PortInfo{Options: map[string]string{"build.cmd": "standin", "patchfiles": "old"}}, macports.PortObservation{})
	p := &probeResult{Version: probeVersion, Before: before, After: &after, Standins: 1, FollowerBenign: true}
	classifyProbePlans(p)
	c := &toleranceCapture{Pair: tolerancePair{Complete: true, Parity: true}, Probe: p}
	_, pass := probeGatePass(c, probeSame, false)
	require.True(t, pass)
	_, pass = probeGatePass(c, probeBuildOnlyMoved, true)
	require.True(t, pass)
	p.BuildOnlyMoved = nil
	_, pass = probeGatePass(c, probeSame, false)
	require.False(t, pass, "missing moved names")
	classifyProbePlans(p)
	p.Standins = 0
	classifyProbePlans(p)
	_, pass = probeGatePass(c, probeSame, false)
	require.True(t, pass, "native non-evidence is explicit")
	_, pass = probeGatePass(c, probeSame, true)
	require.False(t, pass, "fixtures must really perturb")
	p.Status = probeSame
	_, pass = probeGatePass(c, probeSame, false)
	require.False(t, pass, "zero cannot pass as same")
	p.Standins = 1
	classifyProbePlans(p)
	p.Version = 0
	_, pass = probeGatePass(c, probeSame, false)
	require.False(t, pass, "old policy records cannot pass")
	p.Version = probeVersion
	p.SubstitutionsKnown = false
	_, pass = probeGatePass(c, probeSame, false)
	require.False(t, pass, "unknown is not a successful zero")
}
