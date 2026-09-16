package portedit

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
)

func assessmentFixture(t *testing.T, declaration string) (*VersionProbe, *sourceInput) {
	t.Helper()
	s, request, input := probeFixture(t, declaration+"\nmaster_sites https://example.invalid/${github.version}")
	return &VersionProbe{editor: s, request: request, input: input}, input
}

func TestAssessmentDistinguishesInputsFromCandidateFidelity(t *testing.T) {
	for _, tc := range []struct{ name, body, raw, version, status string }{
		{"literal", "github.setup owner fixture 1.2.3 v", "1.2.4", "1.2.4", CandidateChecked},
		{"calculation", "github.setup owner fixture 2026-09-07 v\nversion [string map {- {}} ${github.version}]", "2026-09-14", "20260914", CandidateChecked},
		{"fragment", "set patchNumber 3\nproc release {} {global patchNumber; return 1.2.${patchNumber}}\ngithub.setup owner fixture [release] v", "1.2.4", "1.2.4", CandidateChecked},
		{"ambiguous", "set a 1.2.3\nset b 1.2.3\nif {$a ne {1.2.3}} {github.setup owner fixture $a v} else {github.setup owner fixture $b v}", "1.2.4", "1.2.4", Unsupported},
		{"sibling", "github.setup owner fixture 1.2.3 v\nsubport fixture-child {}", "1.2.4", "1.2.4", Unsupported},
		{"inconclusive", "set release 1.2.3\nif {$release ne {1.2.3}} {error {candidate is not evaluable}}\ngithub.setup owner fixture $release v", "1.2.4", "1.2.4", Unknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, input := assessmentFixture(t, tc.body)
			local, err := p.Assess(t.Context(), nil)
			require.NoError(t, err)
			require.Equal(t, InputFound, local.Outcome, "%+v", local.Findings)
			require.NotEmpty(t, local.Inputs)
			require.Greater(t, local.Inputs[0].Line, 1)
			require.Equal(t, NotTested, assessmentFinding(t, local, "candidate").Status)
			release := record.Release{Requested: tc.raw, Version: tc.version, Tag: "v" + tc.raw}
			actual, err := p.Assess(t.Context(), &release)
			require.NoError(t, err)
			require.Equal(t, tc.status, actual.Outcome, "%+v", actual.Findings)
			if tc.status == Unknown {
				require.Equal(t, "probe-inconclusive", assessmentFinding(t, actual, "candidate").Code)
			}
			require.Equal(t, NotTested, assessmentFinding(t, actual, "archives").Status)
			require.Equal(t, NotTested, assessmentFinding(t, actual, "verification").Status)
			data, err := os.ReadFile(filepath.Join(p.request.Root, input.target.Portfile))
			require.NoError(t, err)
			require.Equal(t, input.data, data)
		})
	}
}

func TestAssessmentReportsMissingHelperWithoutRunningIt(t *testing.T) {
	p, _ := assessmentFixture(t, "github.setup owner fixture 1.2.3 v\noptions go.vendors\ngo.vendors example.invalid/module v1.0 1234")
	p.editor.DependencyTools.Go2Port = filepath.Join(t.TempDir(), "missing-helper")
	result, err := p.Assess(t.Context(), nil)
	require.NoError(t, err)
	require.Equal(t, Blocked, result.Outcome, "%+v", result.Findings)
	require.Equal(t, "missing-helper", assessmentFinding(t, result, "helper").Code)
	helper := filepath.Join(t.TempDir(), "helper")
	marker := filepath.Join(t.TempDir(), "executed")
	require.NoError(t, os.WriteFile(helper, []byte("#!/bin/sh\ntouch '"+marker+"'\nexit 1\n"), 0700))
	p.editor.DependencyTools.Go2Port = helper
	result, err = p.Assess(t.Context(), nil)
	require.NoError(t, err)
	require.Equal(t, InputFound, result.Outcome, "%+v", result.Findings)
	require.Equal(t, NotTested, assessmentFinding(t, result, "regeneration").Status)
	require.NoFileExists(t, marker)
}

func TestAssessmentRetainsFetchAndChecksumLimitations(t *testing.T) {
	p, _ := assessmentFixture(t, "github.setup owner fixture 1.2.3 v\nfetch.type git")
	result, err := p.Assess(t.Context(), nil)
	require.NoError(t, err)
	require.Equal(t, Unsupported, result.Outcome)
	require.Equal(t, Unsupported, assessmentFinding(t, result, "fetch").Status)
	p, _ = assessmentFixture(t, "github.setup owner fixture 1.2.3 v")
	p.input.data = append(p.input.data, []byte("checksums sha256 1234\n")...)
	result, err = p.Assess(t.Context(), nil)
	require.NoError(t, err)
	require.Equal(t, Unsupported, assessmentFinding(t, result, "checksums").Status)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = p.Assess(ctx, nil)
	require.ErrorIs(t, err, context.Canceled)
}

func assessmentFinding(t *testing.T, a Assessment, check string) Finding {
	t.Helper()
	for _, f := range a.Findings {
		if f.Check == check {
			return f
		}
	}
	t.Fatalf("missing finding %s: %+v", check, a.Findings)
	return Finding{}
}

func TestLocalAssessmentKeepsFailedCounterfactualUnknown(t *testing.T) {
	p, input := assessmentFixture(t, `set patchNumber 3
if {$patchNumber ne "3"} {error "artificial patch not allowed"}
proc release {} {global patchNumber; return 1.2.${patchNumber}}
github.setup owner fixture [release] v`)
	result, err := p.Assess(t.Context(), nil)
	require.NoError(t, err)
	require.Equal(t, Unknown, result.Outcome, "%+v", result.Findings)
	require.Equal(t, "probe-inconclusive", assessmentFinding(t, result, "version-input").Code)
	data, err := os.ReadFile(filepath.Join(p.request.Root, input.target.Portfile))
	require.NoError(t, err)
	require.Equal(t, input.data, data)
}
