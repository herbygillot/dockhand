package observe

import (
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/stretchr/testify/require"
)

func TestHostAccessDiagnosticsBoundCallText(t *testing.T) {
	call := strings.Repeat("x", 600) + "TAIL-MARKER"
	p := macports.PortObservation{ModeledHostAccess: true, Problems: []string{call}, Declarations: []macports.Declaration{{Command: "dockhand.host-access", Values: []string{call, "closed", "portfile", call}, Frames: []macports.SourceFrame{{File: "/tree/Portfile", Line: 1, Command: call}}}}}
	require.NotContains(t, HostInputs(p), "TAIL-MARKER")
	require.NotContains(t, unansweredWords(call), "TAIL-MARKER")
	var lines []string
	ctx := progress.WithReporter(t.Context(), func(u progress.Update) { lines = append(lines, u.Message) })
	_, left := Tolerate(ctx, p, nil, "")
	require.True(t, left)
	require.NotEmpty(t, lines)
	require.NotContains(t, strings.Join(lines, "\n"), "TAIL-MARKER")
	require.Equal(t, call, p.Declarations[0].Values[3])
}
