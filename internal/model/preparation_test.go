package model

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// An edit's result round-trips, a patch list never read kept apart from
// one read and found empty (#194).
func TestAnEditResultRoundTrips(t *testing.T) {
	t.Parallel()
	for _, result := range []EditResult{
		{Before: "1.8.0_0", After: "1.8.1_0", Patches: &PatchCheck{Scope: PatchScope("2.12.7", "8.6", "darwin 25.1.0 arm64"), Declared: []PatchResult{{Name: "fix.diff", State: PatchRejected}}},
			Findings: []Finding{{Kind: "patch-rejected", Subject: "fix.diff", Detail: []string{"Hunk #1 FAILED at 12."}}, {Kind: "patches-dropped", Subject: "merged.diff", Fixed: true}}},
		{Before: "1.8.0_0", After: "1.8.1_0", Patches: &PatchCheck{Scope: PatchScope("2.12.7"), Declared: []PatchResult{}}},
		{Before: "1.8.0_0", After: "1.8.1_0"},
	} {
		data, err := json.Marshal(result)
		require.NoError(t, err)
		var back EditResult
		require.NoError(t, json.Unmarshal(data, &back))
		require.Equal(t, result.Patches == nil, back.Patches == nil, "nil stays nil, an empty list stays a list")
		if result.Patches != nil {
			require.Equal(t, len(result.Patches.Declared), len(back.Patches.Declared))
		}
		require.Equal(t, result.Findings, back.Findings)
	}
}

// Two patch lists compare only where their scopes are equal and every part
// of them known: a part the evaluator couldn't name compares with nothing,
// itself included (#194).
func TestScopesCompareOnlyWhenEqualAndKnown(t *testing.T) {
	t.Parallel()
	tahoe := PatchScope("2.12.7", "8.6", "darwin 25.1.0 arm64", "26.1", "26.1", "jq", "")
	require.True(t, ScopesComparable(PatchScope("2.12.7", "8.6", "darwin 25.1.0 arm64", "26.1", "26.1", "jq", "+universal"), PatchScope("2.12.7", "8.6", "darwin 25.1.0 arm64", "26.1", "26.1", "jq", "+universal")))
	require.False(t, ScopesComparable(tahoe, tahoe), "an unknown part (here the variants) compares with nothing")
	require.False(t, ScopesComparable(PatchScope("2.12.7", "8.6", "darwin 25.1.0 arm64", "26.1", "26.1", "jq", "+universal"), PatchScope("2.12.7", "8.6", "darwin 25.2.0 arm64", "26.1", "26.1", "jq", "+universal")), "the minor version differs")
	require.False(t, ScopesComparable("", ""), "no scope compares with nothing")
}
