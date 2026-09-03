package tart

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/verify"
)

// The argv files are the guest's entire instruction set: the runner
// reads each line of each file into "$@" and hands it to port(1), so
// these bytes are what the machine is asked to do and nothing else
// pins them. They are asserted here as literal bytes rather than
// against build.InstallArgs, because agreeing with the same function
// that produced them would prove only that Join works — the claim is
// that a request naming one port produces the argv a request naming one
// port has always produced.
func TestArgvFilesAtOnePort(t *testing.T) {
	files := argvFiles(verify.Request{Ports: []string{"jq"}})

	require.Len(t, files, 2, "no test was asked for, so argv.test is not written at all")
	assert.Equal(t, "/tmp/dockhand-verify/argv", files[0].Dest())
	assert.Equal(t, "-d\n-N\ninstall\njq\n", files[0].Body)
	assert.Equal(t, "/tmp/dockhand-verify/argv.lint", files[1].Dest())
	assert.Equal(t, "lint\njq\n", files[1].Body)
}

// A test run adds a third file and moves nothing: `port test` is
// additive, so the install argv it accompanies must be the argv it
// would have been without it.
func TestArgvFilesWithTest(t *testing.T) {
	files := argvFiles(verify.Request{Ports: []string{"jq"}, Test: true})

	require.Len(t, files, 3)
	assert.Equal(t, "-d\n-N\ninstall\njq\n", files[0].Body)
	assert.Equal(t, "lint\njq\n", files[1].Body)
	assert.Equal(t, "/tmp/dockhand-verify/argv.test", files[2].Dest())
	assert.Equal(t, "-d\n-N\n-k\ntest\njq\n", files[2].Body)
}

// Absence is the control flow. The runner skips a file that is not
// there; an empty argv.test would run port(1) with no arguments, which
// is a usage message and a failed verification.
func TestArgvFilesOmitTheTestFileEntirely(t *testing.T) {
	for _, f := range argvFiles(verify.Request{Ports: []string{"jq"}}) {
		assert.NotEqual(t, "argv.test", f.Name)
	}
}

// The variant frame trails the port as separate words, and a source
// build is an option ahead of the subcommand. Neither has a caller
// today — nothing in the tree sets Variants or FromSource — which is
// exactly why they are pinned: they are the forms a cohort disturbs
// first, and an unpinned form drifts unnoticed.
func TestArgvFilesCarryVariantsAndSource(t *testing.T) {
	v, err := info.Variants("+ssl", "-doc")
	require.NoError(t, err)
	files := argvFiles(verify.Request{
		Ports:      []string{"jq"},
		Variants:   v,
		FromSource: []string{"jq"},
		Test:       true,
	})

	require.Len(t, files, 3)
	assert.Equal(t, "-d\n-N\n-s\ninstall\njq\n-doc\n+ssl\n", files[0].Body)
	assert.Equal(t, "lint\njq\n", files[1].Body, "lint takes no variant frame")
	assert.Equal(t, "-d\n-N\n-k\ntest\njq\n-doc\n+ssl\n", files[2].Body)
}

// One environment builds one port. The request can carry a cohort
// because the shape the record needs already exists, but nothing builds
// it yet: a second name reaching port(1) would be a build no caller
// asked for. The day that changes, this test is what has to change with
// it, deliberately.
func TestArgvFilesBuildOnlyTheHeadline(t *testing.T) {
	files := argvFiles(verify.Request{Ports: []string{"jq", "oniguruma"}})

	for _, f := range files {
		assert.NotContains(t, f.Body, "oniguruma", "%s named a port nobody asked to build", f.Name)
	}
	assert.Equal(t, "-d\n-N\ninstall\njq\n", files[0].Body)
}
