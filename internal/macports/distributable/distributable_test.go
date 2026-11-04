package distributable

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/tcl/rpc"
	"github.com/herbygillot/dockhand/internal/tcl/shell"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// The checker, MacPorts' own, judges as the buildbot does: a license no
// name of which it knows, a dependency whose license conflicts, the same
// exempted by license_noconflict, an empty license, and a distributable
// port, each in its own words.
func TestTheCheckerJudgesAsMacPortsDoes(t *testing.T) {
	process, err := shell.Start(t.Context(), testsupport.MacPortsTclsh(t))
	require.NoError(t, err)
	session, err := rpc.New(t.Context(), process)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	_, err = session.Call(t.Context(), "eval", Script())
	require.NoError(t, err)
	openssl := Info{License: "OpenSSL", InstallsLibs: true}
	for name, test := range map[string]struct {
		port  string
		infos map[string]Info
		want  Verdict
	}{
		"unknown license": {"wasi-sdk", map[string]Info{"wasi-sdk": {License: "Apache-2 LLVM-exception"}},
			Verdict{Known: true, Reason: `"wasi-sdk" is not distributable because its license "llvm-exception" is not known to be distributable`}},
		"distributable": {"wasmtime", map[string]Info{"wasmtime": {License: "Apache-2", Dependencies: []string{"zlib"}}, "zlib": {License: "zlib", InstallsLibs: true}},
			Verdict{Known: true, OK: true, Reason: `"wasmtime" is distributable`}},
		"conflict": {"harbor", map[string]Info{"harbor": {License: "GPL-2", Dependencies: []string{"openssl3"}}, "openssl3": openssl},
			Verdict{Known: true, Reason: `"harbor" is not distributable because its license "gpl" conflicts with license "OpenSSL" of dependency "openssl3"`}},
		"noconflict": {"harbor", map[string]Info{"harbor": {License: "GPL-2", Dependencies: []string{"openssl3"}, NoConflict: []string{"openssl3"}}, "openssl3": openssl},
			Verdict{Known: true, OK: true, Reason: `"harbor" is distributable`}},
		"empty": {"harbor", map[string]Info{"harbor": {}},
			Verdict{Known: true, Reason: `"harbor" is not distributable because its license option is empty`}},
		"unknown port": {"harbor", map[string]Info{},
			Verdict{Reason: `"harbor" couldn't be judged`}},
	} {
		got, err := Judge(t.Context(), session, test.port, test.infos)
		require.NoError(t, err, name)
		require.Equal(t, test.want, got, name)
	}
}
