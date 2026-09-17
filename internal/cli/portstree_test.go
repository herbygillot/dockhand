package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/stretchr/testify/require"
)

// A checkout that is not a ports tree, such as dockhand's own repository, is
// refused by every command that would register it or fetch into it, before
// the state database is created.
func TestCommandsRefuseCheckoutsThatAreNotPortsTrees(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644))
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"add", "-A"}, {"-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgSign=false", "-c", "core.hooksPath=" + os.DevNull, "commit", "-q", "-m", "not a ports tree"}} {
		command := exec.CommandContext(t.Context(), "git", args...)
		command.Dir = root
		output, err := command.CombinedOutput()
		require.NoError(t, err, "%s", output)
	}
	db := filepath.Join(t.TempDir(), "state", "state.db")
	config := app.Config{Repository: root, DBPath: db}
	for _, args := range [][]string{
		{"status"}, {"status", "--json"}, {"bump", "jq", "--no-verify"}, {"bump", "jq", "--diff"}, {"bump-revision", "jq", "--reason", "x"},
		{"verify", "jq", "--branch", "main", "--provider", "tart"}, {"publish", "--branch", "main"}, {"wait", "--branch", "main"}, {"refresh", "--branch", "main"}, {"gc"}, {"outdated", "jq"}, {"assess", "jq"},
	} {
		var out bytes.Buffer
		err := Run(t.Context(), args, Streams{Out: &out, Err: &out}, config)
		require.Error(t, err, "%v", args)
		require.Contains(t, err.Error(), "no <category>/<port>/Portfile", "%v", args)
		require.Contains(t, err.Error(), root, "%v", args)
	}
	require.NoDirExists(t, filepath.Dir(db), "a refused checkout must not create the state database")
}
