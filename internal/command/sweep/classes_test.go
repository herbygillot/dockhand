package sweep

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Every subcommand the git package issues is classed a write or a read,
// so a new one is swept, or said not to need it, the day it's written
// (docs/designs/fault-sweep.md, "Git").
func TestEveryGitSubcommandIsClassed(t *testing.T) {
	t.Parallel()
	call := regexp.MustCompile(`\b(?:output|run|runStatus|runInput|outputInput|runEnv|outside)\(ctx,(?: [a-zA-Z.]+,)*? "([a-z-]+)"`)
	files, err := filepath.Glob(filepath.Join("..", "..", "git", "*.go"))
	require.NoError(t, err)
	var unclassed []string
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		data, err := os.ReadFile(file)
		require.NoError(t, err)
		for _, match := range call.FindAllStringSubmatch(string(data), -1) {
			name := match[1]
			if strings.HasPrefix(name, "-") {
				continue // an option before the subcommand, as push's -c
			}
			if !slices.Contains(gitWrites, name) && !slices.Contains(gitReads, name) && !slices.Contains(unclassed, name) {
				unclassed = append(unclassed, name)
			}
		}
	}
	require.Empty(t, unclassed, "git subcommands classed neither a write nor a read")
	require.True(t, gitWrite([]string{"symbolic-ref", "HEAD", "refs/heads/x"}))
	require.False(t, gitWrite([]string{"symbolic-ref", "--quiet", "HEAD"}))
	require.False(t, gitWrite([]string{"worktree", "list", "--porcelain"}))
	require.False(t, gitWrite([]string{"config", "--get", "remote.origin.url"}))
	require.True(t, gitWrite([]string{"-c", "push.followTags=false", "push", "fork", "x"}))
}
