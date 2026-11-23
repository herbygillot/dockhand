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
		require.Equal(t, result, back, "the whole value, nil kept apart from an empty list (Codex's read of step 1)")
		require.Equal(t, result.Patches == nil, back.Patches == nil, "nil stays nil, an empty list stays a list")
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

	// The developer tools' part is recorded with its three states and
	// not compared (amended 2026-11-22): different Xcode values, read
	// against not read, and unknown tools are all equal where every other
	// part is equal and known.
	scope := func(darwin, tools string) string {
		return PatchScope("2.12.7", "8.6", "darwin 25 arm64", darwin, tools, "jq", "-")
	}
	read := func(xcode string) string { return ScopeTools(xcode, xcode+".0.0.1") }
	require.True(t, ScopesComparable(scope("25.1.0", read("26.1")), scope("25.1.0", read("26.2"))), "another Xcode under the same Darwin")
	require.True(t, ScopesComparable(scope("25.1.0", ScopeTools("(not read)", "(not read)")), scope("25.1.0", read("26.1"))), "read against not read")
	require.True(t, ScopesComparable(scope("25.1.0", ScopeTools("", "")), scope("25.1.0", ScopeTools("", ""))), "unknown tools with all else known")
	require.False(t, ScopesComparable(scope("25.1.0", read("26.1")), scope("25.2.0", read("26.1"))), "the Darwin version still counts")
	require.False(t, ScopesComparable(scope("", read("26.1")), scope("", read("26.1"))), "any other unknown part still compares with nothing")
}

// A change request's flags round-trip, a flag left out told from one given
// false or empty (#195).
func TestAChangeRequestKeepsWhatWasGivenApartFromWhatWasLeftOut(t *testing.T) {
	t.Parallel()
	request := ChangeRequest{ID: "rq_1", Origin: "ship", Shared: Set(false), Source: SourceAsk{Source: Set("releases"), KeepHTTP: Set(false)},
		Actions: ActionsAsk{Except: Set([]string{"py-foo"}), Closes: Set([]string{})}, To: Set("check"),
		Check: CheckAsk{On: Set([]string{"tahoe"}), Tests: Set("required")}, Publish: PublishAsk{Draft: Set(true), Type: Set([]string{"bugfix"})}}
	data, err := json.Marshal(request)
	require.NoError(t, err)
	var back ChangeRequest
	require.NoError(t, json.Unmarshal(data, &back))
	require.Equal(t, request.Shared, back.Shared, "given false is given")
	require.Equal(t, request.Source, back.Source)
	require.Equal(t, request.Check, back.Check)
	require.Equal(t, request.Publish, back.Publish)
	require.Equal(t, request.To, back.To)
	require.True(t, back.Actions.Closes.Given, "given empty is given")
	require.False(t, back.Actions.WithObsolete.Given, "left out is left out")
	require.Equal(t, "fallback", Flag[string]{}.Or("fallback"))
	require.Empty(t, Set("").Or("fallback"), "given empty is the value given")
}
