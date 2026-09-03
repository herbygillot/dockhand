package tart

import (
	"strings"
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

// One environment now builds the cohort it was given, and this test is
// the one that changed to say so — it was written predicting its own
// replacement, on the day a second name reaching port(1) stopped being a
// build no caller asked for.
//
// What it asserts now is the boundary rather than the refusal: a request
// naming one port still produces exactly the files a request naming one
// port has always produced, and a second name is the only thing that
// changes anything. The cohort's own shape is guest_test.go's business;
// this file's is that one subject did not move.
func TestASecondPortIsTheOnlyThingThatMovesTheHeadlinesFiles(t *testing.T) {
	solo := argvFiles(verify.Request{Ports: []string{"jq"}})
	require.Len(t, solo, 2)
	assert.Equal(t, "/tmp/dockhand-verify/argv", solo[0].Dest())
	assert.Equal(t, "-d\n-N\ninstall\njq\n", solo[0].Body)

	cohort := argvFiles(verify.Request{Ports: []string{"jq", "oniguruma"}})
	assert.NotEqual(t, solo[0].Dest(), cohort[0].Dest(),
		"a cohort names its files by position, so no member owns the bare name")
	var named bool
	for _, f := range cohort {
		named = named || strings.Contains(f.Body, "oniguruma")
	}
	assert.True(t, named, "the second port is built now, not carried")
}
