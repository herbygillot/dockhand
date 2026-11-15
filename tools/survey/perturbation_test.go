package main

import (
	"encoding/json"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
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
			require.Equal(t, "read-options."+option.Name, firstProbeDifference(original, changed))
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
	info.Options["build.cmd"] = ""
	require.Equal(t, "read-options.build.cmd", firstProbeDifference(a, makeProbePlan(info, p)))
	delete(info.Options, "build.cmd")
	info.OptionErrors = map[string]string{"build.cmd": "missing"}
	require.Equal(t, "read-options.build.cmd", firstProbeDifference(a, makeProbePlan(info, p)))
}
