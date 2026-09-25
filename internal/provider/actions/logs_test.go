package actions

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
)

// A runner's log, abridged, as GitHub serves it: jq builds and its tests
// fail, jq-devel fails to lint, oniguruma's dependencies don't install,
// and harbor fails to install.
const runnerLog = `2026-09-25T10:00:00.0000000Z ##[group]Run set -eu
2026-09-25T10:00:01.0000000Z ##[group]Listing subports
2026-09-25T10:00:01.1000000Z jq
2026-09-25T10:00:01.1000000Z jq-devel
2026-09-25T10:00:01.1000000Z oniguruma
2026-09-25T10:00:01.1000000Z harbor
2026-09-25T10:00:01.2000000Z ##[endgroup]
2026-09-25T10:00:02.0000000Z ##[error]port lint jq-devel:%0AError: Unknown dependency: foo
2026-09-25T10:01:00.0000000Z ##[group]Installing dependencies for jq
2026-09-25T10:01:30.0000000Z ##[endgroup]
2026-09-25T10:01:31.0000000Z ##[group]Installing jq
2026-09-25T10:02:00.0000000Z ##[endgroup]
2026-09-25T10:02:01.0000000Z ##[group]Testing jq
2026-09-25T10:02:30.0000000Z ##[error]Tests failed for jq
2026-09-25T10:02:31.0000000Z ##[group]Installing dependencies for oniguruma
2026-09-25T10:03:00.0000000Z ##[error]Failed to install dependencies for oniguruma
2026-09-25T10:03:01.0000000Z ##[group]Installing dependencies for harbor
2026-09-25T10:03:02.0000000Z ##[endgroup]
2026-09-25T10:03:03.0000000Z ##[group]Installing harbor
2026-09-25T10:04:00.0000000Z ##[error]Failed to install harbor
`

func TestTheWorkflowsMarkersSayWhatHappened(t *testing.T) {
	built := ReadLog([]byte(runnerLog))
	outcome := func(name string) string {
		o, phase := built[name].Outcome()
		return string(o) + " " + string(phase)
	}
	require.Equal(t, "passed ", outcome("jq"))
	require.Equal(t, model.TestsFailed, built["jq"].Tests())
	require.Equal(t, "failed lint", outcome("jq-devel"))
	require.Equal(t, "failed install", outcome("oniguruma"))
	require.Equal(t, "failed install", outcome("harbor"))
	for _, name := range []string{"jq", "jq-devel", "oniguruma", "harbor"} {
		require.True(t, built[name].Listed, name)
	}
	require.Equal(t, model.TestsNone, built["harbor"].Tests())

	// The workflow's own spelling, before GitHub renders it, reads the same.
	raw := ReadLog([]byte("::group::Listing subports\njq\n::endgroup::\n::error file=textproc/jq/Portfile,line=1,col=1::port lint jq:%0AError\n"))
	o, phase := raw["jq"].Outcome()
	require.Equal(t, model.OutcomeFailed, o)
	require.Equal(t, model.PhaseLint, phase)

	o, _ = (&Built{Listed: true}).Outcome()
	require.Equal(t, model.OutcomeNotRun, o, "listed but never reached")
}
