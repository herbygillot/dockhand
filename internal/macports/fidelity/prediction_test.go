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
	require.Equal(t, []string{"child: port set changed", "evaluation context changed", "extra: port set changed", "main.checksums changed", "main.revision: expected 3, got 4"},
		prediction.Observe(before, wrong).UnexpectedChanges)
}
