package explain

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/buildlog"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/stretchr/testify/require"
)

// A failed step's tail in explain is the one a check's own failure report
// takes: port -d's DEBUG lines and blank lines left out, where the step's
// last lines were only those (#151).
func TestExplainTakesTheStepsTailAsTheCheckDoes(t *testing.T) {
	t.Parallel()
	var log strings.Builder
	log.WriteString("--->  Fetching jq\nDEBUG: fetched\n--->  Installing jq\n")
	for i := 1; i <= 10; i++ {
		fmt.Fprintf(&log, "compiling part %d\n", i)
	}
	log.WriteString("Error: Failed to install jq\n\n")
	for i := 0; i < 12; i++ {
		log.WriteString("DEBUG: Error code: CHILDSTATUS\n")
	}
	log.WriteString("--->  Testing jq\n")
	path := filepath.Join(t.TempDir(), "jq.log")
	require.NoError(t, os.WriteFile(path, []byte(log.String()), 0o644))

	result := model.TargetResult{
		Target: "jq", Outcome: model.OutcomeFailed, Phase: model.PhaseInstall, Log: path,
		Steps: []model.LogStep{{Name: model.StepFetch, Line: 1}, {Name: model.StepInstall, Line: 3}, {Name: model.StepTest, Line: 27}},
	}
	logs := engine.RunLogs{Executions: []engine.ExecutionLogs{{Results: []model.TargetResult{result}}}}
	var out strings.Builder
	writeStep(&out, logs, model.Run{}, "jq")

	file, err := buildlog.Open(path)
	require.NoError(t, err)
	defer file.Close()
	want := buildlog.Last(file, 3, 27, stepLines)
	require.Len(t, want, stepLines)
	require.Equal(t, "Error: Failed to install jq", want[len(want)-1])
	require.Contains(t, out.String(), "    its install step ended:\n      "+strings.Join(want, "\n      ")+"\n")
	require.NotContains(t, out.String(), "DEBUG")
}
