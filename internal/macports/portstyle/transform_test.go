package portstyle

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/tcl/rpc"
	"github.com/herbygillot/dockhand/internal/tcl/shell"
	"github.com/herbygillot/dockhand/internal/testenv"
)

// Expected values verified against the real Tcl proc before this table was
// written; the differential test below re-verifies them continuously.
func TestPerl5ConvertVersion(t *testing.T) {
	cases := map[string]string{
		"0.96":    "0.960.0",
		"1.23":    "1.230.0",
		"0.9601":  "0.960.100",
		"2.034":   "2.34.0",
		"1.2":     "1.200.0",
		"0.083":   "0.83.0",
		"3.45_01": "3.45.10",
		"v0.5.4":  "0.5.4",
		"1.2.3":   "1.2.3",
		"5":       "5",
	}
	for in, want := range cases {
		assert.Equal(t, want, perl5ConvertVersion(in), "input %q", in)
	}
}

// The Go port is differentially tested against the PortGroup's own proc,
// extracted from the fixture and evaluated in a plain tclsh: the oracle is
// never dockhand, converter included.
func TestPerl5ConvertVersionDifferential(t *testing.T) {
	src, err := os.ReadFile("../testdata/portgroups/perl5-1.0.tcl")
	require.NoError(t, err)
	text := string(src)
	i := strings.Index(text, "proc perl5_convert_version")
	require.Positive(t, i)
	j := strings.Index(text[i:], "\n}")
	require.Positive(t, j)
	procDef := text[i : i+j+2]

	path := testenv.Tclsh(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	proc, err := shell.Start(ctx, path)
	require.NoError(t, err)
	s, err := rpc.New(ctx, proc, rpc.WithInit(procDef))
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })

	inputs := []string{
		"0.96", "1.23", "0.9601", "2.034", "1.2", "0.083", "3.45_01",
		"v0.5.4", "1.2.3", "5", "0.01", "10.100001", "2.121_050", "v1.2",
		"0.000001", "1.000", "12.3456789",
	}
	for _, in := range inputs {
		want, err := s.Call(ctx, "eval", "perl5_convert_version {"+in+"}")
		require.NoError(t, err, in)
		assert.Equal(t, want, perl5ConvertVersion(in), "input %q", in)
	}
}

// End to end through Locate: the span carries the literal, Value the
// evaluated form, related by the style's transform.
func TestLocatePerl5Transform(t *testing.T) {
	src := "perl5.setup         Module-Signature 0.96\n"
	loc := mustLocate(t, src, info.Values{Name: "p5-module-signature", Version: "0.960.0"})
	require.Equal(t, Perl5Setup, loc.Style)
	require.Equal(t, "0.96", loc.Span.Text([]byte(src)), "span carries the literal edit target")
	require.Equal(t, "0.960.0", loc.Value, "Value carries the evaluated form")
}
