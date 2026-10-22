package fidelity

import (
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/stretchr/testify/require"
)

// An evaluation is the prediction and nothing more: each side normalized
// by its own root; a port's revision, a field, the port set, or the
// context that differs is unexpected; the prediction's maps are its own.
func TestAnEvaluationIsItsPrediction(t *testing.T) {
	before := snapshot(map[string]macports.PortInfo{
		"main":  {Name: "main", Version: "1", Revision: 2, Options: map[string]string{"filespath": "/before/devel/main/files", "checksums": "sha256 aa"}},
		"child": {Name: "child", Version: "1", Revision: 0, Options: map[string]string{}},
	})
	before.Root = "/before"
	prediction := Predict(before)
	main := prediction.Port("main")
	main.Revision, main.Options["checksums"] = 3, "sha256 bb"
	prediction.Expect("main", main, "main.revision +1", "main.checksums")
	require.Equal(t, "sha256 aa", before.Ports["main"].Options["checksums"], "predicting mutates no observation")

	after := snapshot(map[string]macports.PortInfo{
		"main":  {Name: "main", Version: "1", Revision: 3, Options: map[string]string{"filespath": "/after/devel/main/files", "checksums": "sha256 bb"}},
		"child": {Name: "child", Version: "1", Revision: 0, Options: map[string]string{}},
	})
	after.Root = "/after"
	report := prediction.Observe(before, after)
	require.Empty(t, report.UnexpectedChanges)
	require.Equal(t, []string{"main.revision +1", "main.checksums"}, report.ExpectedChanges)

	wrong := snapshot(map[string]macports.PortInfo{
		"main":  {Name: "main", Version: "1", Revision: 4, Options: map[string]string{"filespath": "/after/devel/main/files", "checksums": "sha256 cc"}},
		"extra": {Name: "extra", Version: "1", Options: map[string]string{}},
	})
	wrong.Root = "/after"
	wrong.Platform.Version = "24"
	report = prediction.Observe(before, wrong)
	require.Equal(t, []string{"child: port set changed", "evaluation context changed", "extra: port set changed", "main.checksums changed", "main.revision: expected 3, got 4"},
		report.UnexpectedChanges)
	// Each of its kind: what the prediction changed that didn't come
	// out so, what it didn't predict, and a port it left as it was moving.
	require.ElementsMatch(t, []Observed{
		{Kind: Unpredicted}, {Kind: Unpredicted, Port: "extra"}, {Kind: PredictedMissing, Port: "child"},
		{Kind: PredictedMissing, Port: "main", Field: "revision"}, {Kind: PredictedMissing, Port: "main", Field: "checksums"},
	}, report.Observed)
	moved := after
	moved.Ports = map[string]macports.PortInfo{"main": after.Ports["main"], "child": {Name: "child", Version: "1", Revision: 1, Options: map[string]string{"homepage": "x"}}}
	moved.Ports["main"].Options["homepage"] = "y"
	require.ElementsMatch(t, []Observed{
		{Kind: ProtectedMoved, Port: "child", Field: "revision"}, {Kind: ProtectedMoved, Port: "child", Field: "homepage"},
		{Kind: Unpredicted, Port: "main", Field: "homepage"},
	}, prediction.Observe(before, moved).Observed)
}
