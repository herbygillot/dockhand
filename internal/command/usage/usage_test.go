package usage

import (
	"bytes"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	guide "github.com/herbygillot/dockhand/docs/usage"
	"github.com/herbygillot/dockhand/internal/config"
	"github.com/herbygillot/dockhand/internal/term"
)

// Every topic usage lists prints its page from the binary, and every page
// docs/usage keeps is a topic's, so none is in the repository alone.
func TestEveryTopicPrintsItsPage(t *testing.T) {
	t.Parallel()
	pages, err := fs.Glob(guide.Pages, "*.md")
	require.NoError(t, err)
	var listed []string
	for _, topic := range Topics {
		listed = append(listed, topic.Page)
		var out bytes.Buffer
		cmd := Command(term.Begin(term.Streams{Out: &out}, nil))
		if topic.Name != "" {
			cmd.SetArgs([]string{topic.Name})
		} else {
			cmd.SetArgs([]string{})
		}
		require.NoError(t, cmd.Execute(), topic.Name)
		expected, err := fs.ReadFile(guide.Pages, topic.Page)
		require.NoError(t, err)
		require.Equal(t, string(expected), out.String(), "Markdown and shell examples print unchanged")
		require.True(t, strings.HasPrefix(out.String(), "# "), "%s prints its page", topic.Page)
		if topic.Name != "" {
			require.Contains(t, cmd.Long, "  "+topic.Name+" ", "usage's help lists it")
		}
	}
	require.ElementsMatch(t, pages, listed)
}

func TestAnUnknownTopicNamesTheTopics(t *testing.T) {
	t.Parallel()
	_, err := Page("hand-edit")
	require.EqualError(t, err, "no usage topic hand-edit: the topics are branches, hand-edits, checks, publishing, recovery, batches, unattended, new-port, review, agents, and configuration")
}

// Every TOML example is a configuration dockhand reads, its keys all
// known, and every shell example parses (Codex's checks of its pages,
// 2026-10-28).
func TestThePagesExamplesRead(t *testing.T) {
	t.Parallel()
	pages, err := fs.Glob(guide.Pages, "*.md")
	require.NoError(t, err)
	for _, page := range pages {
		data, err := fs.ReadFile(guide.Pages, page)
		require.NoError(t, err)
		for i, block := range fenced(string(data), "toml") {
			path := filepath.Join(t.TempDir(), "config.toml")
			require.NoError(t, os.WriteFile(path, []byte(block), 0o644))
			_, err := config.Load(path)
			require.NoError(t, err, "%s's TOML example %d:\n%s", page, i+1, block)
		}
		for i, block := range fenced(string(data), "sh") {
			out, err := exec.Command("/bin/bash", "-n", "-c", block).CombinedOutput()
			require.NoError(t, err, "%s's shell example %d: %s\n%s", page, i+1, out, block)
		}
	}
}

// fenced are a Markdown page's blocks fenced as the language.
func fenced(text, language string) []string {
	var blocks []string
	var block []string
	in := false
	for _, line := range strings.Split(text, "\n") {
		switch {
		case !in && strings.TrimSpace(line) == "```"+language:
			in, block = true, nil
		case in && strings.TrimSpace(line) == "```":
			in = false
			blocks = append(blocks, strings.Join(block, "\n")+"\n")
		case in:
			block = append(block, line)
		}
	}
	return blocks
}
