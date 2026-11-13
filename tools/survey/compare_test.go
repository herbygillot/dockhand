package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	portsurvey "github.com/herbygillot/dockhand/internal/macports/survey"
)

// A comparison reads the journals by selector, whatever their order, and
// says what moved, what regressed, and where ports stop.
func TestCompareReportsMovesRegressionsAndStops(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, lines ...string) string {
		path := filepath.Join(dir, name)
		var data []byte
		for _, line := range lines {
			data = append(data, line+"\n"...)
		}
		require.NoError(t, os.WriteFile(path, data, 0o644))
		return path
	}
	base := write("base.jsonl",
		`{"Source":{"Commit":"abd9fff84df525aef309d4f1ffddff52ce0f4a8e"}}`,
		`{"Selector":"jq","Outcome":"input-found","Portfile":"textproc/jq/Portfile","Findings":[{"Check":"fetch","Status":"passed","Code":"fetch-checked"}],"Coverage":[{"Fetch":{"Guards":["os.major"]}}]}`,
		`{"Selector":"yq","Outcome":"unsupported","Portfile":"textproc/yq/Portfile","Findings":[{"Check":"fetch","Status":"unsupported","Code":"unsupported-convention"}],"Coverage":[{"Fetch":{"Problem":"refused"}}]}`,
		`{"Selector":"gone","Outcome":"input-found","Portfile":"textproc/gone/Portfile"}`,
	)
	next := write("next.jsonl",
		`{"Source":{"Commit":"abd9fff84df525aef309d4f1ffddff52ce0f4a8e"}}`,
		`{"Selector":"yq","Outcome":"input-found","Portfile":"textproc/yq/Portfile","Findings":[{"Check":"fetch","Status":"passed","Code":"fetch-checked"}]}`,
		`{"Selector":"jq","Outcome":"unknown","Portfile":"textproc/jq/Portfile","Findings":[{"Check":"fetch","Status":"unknown","Code":"probe-inconclusive"}]}`,
	)
	var out bytes.Buffer
	require.NoError(t, compareJournals(&out, base, next, 5, true))
	report := out.String()
	require.Contains(t, report, "shared 2; only in the baseline 1; only in the new 0")
	require.Contains(t, report, "      1  input-found → unknown\n")
	require.Contains(t, report, "regressions: 1\n  jq: input-found → unknown (fetch/probe-inconclusive)\n")
	require.Contains(t, report, "improvements: 1\n  yq: unsupported → input-found\n")
	require.Contains(t, report, "fetch guards lost 1, changed 0; refused then accepted 1, accepted then refused 0")
	require.Contains(t, report, "Portfiles fully covered: 1 → 1")
	require.Contains(t, report, "       1 →      0  fetch/unsupported-convention")
	require.Contains(t, report, "       0 →      1  fetch/probe-inconclusive")
}

// A port whose version is MacPorts' own is covered: an improvement on
// unsupported, and its Portfile fully covered (batch 37).
func TestAnOwnVersionIsCovered(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, lines ...string) string {
		path := filepath.Join(dir, name)
		var data []byte
		for _, line := range lines {
			data = append(data, line+"\n"...)
		}
		require.NoError(t, os.WriteFile(path, data, 0o644))
		return path
	}
	base := write("base.jsonl",
		`{"Source":{"Commit":"abd9fff84df525aef309d4f1ffddff52ce0f4a8e"}}`,
		`{"Selector":"kubectl_select","Outcome":"unsupported","Portfile":"sysutils/kubectl/Portfile","Findings":[{"Check":"fetch","Status":"unsupported","Code":"unsupported-convention"}]}`,
	)
	next := write("next.jsonl",
		`{"Source":{"Commit":"abd9fff84df525aef309d4f1ffddff52ce0f4a8e"}}`,
		`{"Selector":"kubectl_select","Outcome":"own-version","Portfile":"sysutils/kubectl/Portfile","Findings":[{"Check":"version-input","Status":"not-tested","Code":"own-version"}]}`,
	)
	var out bytes.Buffer
	require.NoError(t, compareJournals(&out, base, next, 5, true))
	report := out.String()
	require.Contains(t, report, "regressions: 0\n")
	require.Contains(t, report, "improvements: 1\n  kubectl_select: unsupported → own-version\n")
	require.Contains(t, report, "Portfiles fully covered: 0 → 1")
}

// Two runs compare only like for like: a pair whose settings differ is
// refused, each difference named, and so is a run that records none,
// unless the comparison is unchecked, when it says so (#222).
func TestCompareRefusesRunsMadeDifferently(t *testing.T) {
	dir := t.TempDir()
	journal := func(name string, made *settings) string {
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, []byte(`{"Source":{"Commit":"abd9fff84df525aef309d4f1ffddff52ce0f4a8e"}}`+"\n"+`{"Selector":"jq","Outcome":"input-found","Portfile":"textproc/jq/Portfile"}`+"\n"), 0o644))
		if made != nil || name == "old.jsonl" {
			data, err := json.Marshal(run{Settings: made})
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(path+".run.json", data, 0o644))
		}
		return path
	}
	gating := settings{Mode: "current", Record: true, Recording: true, Selection: "every port", Commit: "abd9fff8", Prefix: "/opt/macports-test"}
	sample := gating
	sample.Record, sample.Recording, sample.Selection = false, false, "826 ports, sha256:0123456789ab"
	base, same, other, old := journal("base.jsonl", &gating), journal("same.jsonl", &gating), journal("other.jsonl", &sample), journal("old.jsonl", nil)

	var out bytes.Buffer
	require.NoError(t, compareJournals(&out, base, same, 5, false))
	require.NotContains(t, out.String(), "unchecked")

	err := compareJournals(&out, base, other, 5, false)
	require.ErrorContains(t, err, "record (-db): true against false")
	require.ErrorContains(t, err, "selection: every port against 826 ports")
	require.NotContains(t, err.Error(), "prefix")

	require.ErrorContains(t, compareJournals(&out, base, old, 5, false), "made before surveys recorded their settings")
	require.ErrorContains(t, compareJournals(&out, base, filepath.Join(dir, "none.jsonl"), 5, false), "no run.json beside it")

	out.Reset()
	require.NoError(t, compareJournals(&out, base, other, 5, true))
	require.Contains(t, out.String(), "unchecked: the runs differ:\n  record (-db): true against false")
}

// A before/after pair is one command: each binary runs with the flags the
// pair was given and a record of its own, and the two are compared, here
// with stand-ins that write what a survey writes (#222).
func TestAPairRunsBothAndCompares(t *testing.T) {
	dir := t.TempDir()
	stand := func(name, outcome string) string {
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, []byte(`#!/bin/sh
for last; do :; done
journal="${last%.db}.jsonl"
echo "$@" > "$journal.args"
printf '%s\n' '{"Source":{"Commit":"abd9fff84df525aef309d4f1ffddff52ce0f4a8e"}}' '{"Selector":"jq","Outcome":"`+outcome+`","Portfile":"textproc/jq/Portfile"}' > "$journal"
printf '%s\n' '{"settings":{"mode":"current","record":true,"recording":true,"selection":"every port","commit":"abd9fff8","prefix":"/opt/macports-test"}}' > "$journal.run.json"
`), 0o755))
		return path
	}
	before, after := stand("before", "unknown"), stand("after", "input-found")
	out := filepath.Join(dir, "pair")
	var report, log bytes.Buffer
	require.NoError(t, runPair(&report, &log, before, after, out, []string{"-tree=/tmp/ports", "-prefix=/opt/macports-test"}, 5))
	require.Contains(t, report.String(), "improvements: 1\n  jq: unknown → input-found\n")
	args, err := os.ReadFile(filepath.Join(out, "before.jsonl.args"))
	require.NoError(t, err)
	require.Equal(t, "-tree=/tmp/ports -prefix=/opt/macports-test -db "+filepath.Join(out, "before.db")+"\n", string(args))
}

// A selection is recorded by what it names, not the order it was named
// in.
func TestASelectionIsRecordedByItsPorts(t *testing.T) {
	a := selectionOf(portsurvey.Selection{Ports: []string{"jq", "yq", "jq"}})
	require.Equal(t, a, selectionOf(portsurvey.Selection{Ports: []string{"yq", "jq"}}))
	require.Contains(t, a, "2 ports, sha256:")
	require.NotEqual(t, a, selectionOf(portsurvey.Selection{Ports: []string{"jq"}}))
	require.Equal(t, "every port", selectionOf(portsurvey.Selection{All: true}))
	require.Equal(t, "categories devel,textproc", selectionOf(portsurvey.Selection{Categories: []string{"textproc", "devel"}}))
}
