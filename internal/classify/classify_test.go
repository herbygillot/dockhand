package classify

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/portstyle"
	"github.com/herbygillot/dockhand/internal/tcl/shell"
	"github.com/herbygillot/dockhand/internal/testenv"
)

func newEvaluator(t *testing.T) *eval.Evaluator {
	t.Helper()
	path := testenv.PortTclsh(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	proc, err := shell.Start(ctx, path)
	require.NoError(t, err)
	e, err := eval.New(ctx, proc)
	require.NoError(t, err)
	t.Cleanup(func() { e.Close() })
	return e
}

func portdir(t *testing.T, portfile string) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Portfile"), []byte(portfile), 0o644))
	return dir
}

func fixturedir(t *testing.T, name string) string {
	t.Helper()
	src, err := os.ReadFile("../macports/testdata/portfiles/" + name)
	require.NoError(t, err)
	return portdir(t, string(src))
}

func TestClassifyLiteralVersion(t *testing.T) {
	e := newEvaluator(t)
	r := Port(context.Background(), e, portdir(t, "PortSystem 1.0\nname x\nversion 1.2.3\ncategories devel\n"))
	require.Equal(t, Located, r.Outcome)
	require.Equal(t, portstyle.VersionLine, r.Style)
	require.Equal(t, "x", r.Name)
}

func TestClassifyPortgroupStyle(t *testing.T) {
	e := newEvaluator(t)
	r := Port(context.Background(), e, fixturedir(t, "math__ivy"))
	require.Equal(t, Located, r.Outcome)
	require.Equal(t, portstyle.GoSetup, r.Style)
}

func TestClassifyComputedVersion(t *testing.T) {
	e := newEvaluator(t)
	r := Port(context.Background(), e, portdir(t,
		"PortSystem 1.0\nname x\nset major 1\nversion ${major}.5\ncategories devel\n"))
	require.Equal(t, NotLiteral, r.Outcome)
	require.NotEmpty(t, r.Detail)
}

func TestClassifyEvalFailure(t *testing.T) {
	e := newEvaluator(t)
	r := Port(context.Background(), e, filepath.Join(t.TempDir(), "missing"))
	require.Equal(t, EvalFailed, r.Outcome)
	require.NotEmpty(t, r.Detail)
}

func TestCensus(t *testing.T) {
	var c Census
	c.Add(Result{Outcome: Located, Style: portstyle.VersionLine})
	c.Add(Result{Outcome: Located, Style: portstyle.VersionLine})
	c.Add(Result{Outcome: Located, Style: portstyle.GoSetup})
	c.Add(Result{Outcome: NotLiteral})
	c.Add(Result{Outcome: EvalFailed})
	require.Equal(t, 5, c.Total)
	require.Equal(t, 3, c.ByOutcome[Located])
	require.Equal(t, 2, c.ByStyle[portstyle.VersionLine])

	report := c.String()
	require.Contains(t, report, "5 ports classified")
	require.Less(t, strings.Index(report, "version"), strings.Index(report, "go.setup"),
		"styles ordered by count")
}

func TestDeclineOutcomeMapping(t *testing.T) {
	require.Equal(t, UnknownStyle, declineOutcome(&portstyle.Decline{Type: portstyle.UnknownStyle}))
	require.Equal(t, NotLiteral, declineOutcome(&portstyle.Decline{Type: portstyle.NotLiteral}))
}
