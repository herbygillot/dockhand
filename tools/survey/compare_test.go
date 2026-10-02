package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
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
	require.NoError(t, compareJournals(&out, base, next, 5))
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
	require.NoError(t, compareJournals(&out, base, next, 5))
	report := out.String()
	require.Contains(t, report, "regressions: 0\n")
	require.Contains(t, report, "improvements: 1\n  kubectl_select: unsupported → own-version\n")
	require.Contains(t, report, "Portfiles fully covered: 0 → 1")
}
