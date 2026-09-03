package testenv

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The point of resolving the corpus from this file's own path is that
// it does not depend on where the test runs from. Proving it means
// actually running from somewhere else, which is the one thing the
// old relative spellings could not survive.
func TestTheCorpusIsFoundFromAnyWorkingDirectory(t *testing.T) {
	wd, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.Chdir(wd)) })
	require.NoError(t, os.Chdir(t.TempDir()))

	src := Portfile(t, "math__ivy")
	assert.Contains(t, string(src), "go.setup", "the fixture the computed-version tests were chosen for")
}

func TestPortfileDirStagesAnEvaluablePortdir(t *testing.T) {
	dir := PortfileDir(t, "math__ivy")
	staged, err := os.ReadFile(filepath.Join(dir, "Portfile"))
	require.NoError(t, err)
	assert.Equal(t, Portfile(t, "math__ivy"), staged)

	// A fresh directory each time, so a test that edits its copy cannot
	// reach the next test's.
	assert.NotEqual(t, dir, PortfileDir(t, "math__ivy"))
}

func TestPortgroupCorpus(t *testing.T) {
	names := Portgroups(t)
	require.NotEmpty(t, names)
	assert.Contains(t, names, "perl5-1.0.tcl")
	assert.IsIncreasing(t, names, "sorted, so a sweep's failures are reported in a stable order")

	src := Portgroup(t, "perl5-1.0.tcl")
	assert.Contains(t, string(src), "proc perl5_convert_version")
}

func TestPortfileCorpusLists(t *testing.T) {
	names := Portfiles(t)
	require.NotEmpty(t, names)
	assert.Contains(t, names, "math__ivy")
	assert.IsIncreasing(t, names, "sorted, so a sweep's failures are reported in a stable order")
}
