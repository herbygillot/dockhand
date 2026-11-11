package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// tools/shard-test.sh, which runs the command package's tests in CI and
// the gate, fails a test binary that can't list its tests, as one whose
// init panics, where `|| true` printed ok (Codex's sixth review, #75, its
// probe). It sits here, beside the corpus, as the other harness of the
// repository's own tests.
func TestShardTestFailsABinaryThatCantListItsTests(t *testing.T) {
	t.Parallel()
	repo, err := filepath.Abs("../..")
	require.NoError(t, err)
	fixture := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(fixture, "go.mod"), []byte("module example.invalid/shardprobe\n\ngo 1.21\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(fixture, "probe.go"), []byte("package shardprobe\nfunc init(){ panic(\"fixture init failed\") }\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(fixture, "probe_test.go"), []byte("package shardprobe\nimport \"testing\"\nfunc TestSentinel(t *testing.T){}\n"), 0o600))
	sharded := exec.Command("/bin/sh", filepath.Join(repo, "tools/shard-test.sh"), ".", "2", "30s")
	sharded.Dir = fixture
	goBin, err := exec.LookPath("go")
	require.NoError(t, err)
	sharded.Env = append(os.Environ(), "GO="+goBin, "GOFLAGS=-mod=mod")
	output, err := sharded.CombinedOutput()
	require.Error(t, err, string(output))
	require.Contains(t, string(output), "fixture init failed")
	require.Contains(t, string(output), "listing its tests failed")
}

// A shard count below one is refused before anything is built, where 0
// ran no shard and printed ok (#188).
func TestShardTestRefusesACountBelowOne(t *testing.T) {
	t.Parallel()
	repo, err := filepath.Abs("../..")
	require.NoError(t, err)
	for _, count := range []string{"0", "-1", "two", ""} {
		sharded := exec.Command("/bin/sh", filepath.Join(repo, "tools/shard-test.sh"), "example.invalid/none", count, "30s")
		sharded.Env = append(os.Environ(), "GO=/nonexistent/go")
		output, err := sharded.CombinedOutput()
		require.Error(t, err, string(output))
		require.Equal(t, "shard-test.sh: the shard count must be 1 or more, got "+count+"\n", string(output))
	}
}
