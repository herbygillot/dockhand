package tart

import (
	"testing"

	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/workflow/choice"
	"github.com/stretchr/testify/require"
)

// Callers find these capabilities by type assertion, so the compiler checks
// none of them: a provider that lost a method would lose the feature
// quietly. Each is stated here with what goes without it.
func TestTartSatisfiesItsOptionalCapabilities(t *testing.T) {
	var provider any = &Provider{}
	_, ok := provider.(verify.LogReader)
	require.True(t, ok, "--trace and status read Tart build logs through verify.LogReader")
	_, ok = provider.(verify.ArtifactPruner)
	require.True(t, ok, "retention prunes Tart diagnostic directories through verify.ArtifactPruner")
	_, ok = provider.(choice.LocalImages)
	require.True(t, ok, "--target-image configures Tart builds through choice.LocalImages")
	var machine any = &native{}
	_, ok = machine.(guestLogReader)
	require.True(t, ok, "a running guest's log is read through guestLogReader")
}
