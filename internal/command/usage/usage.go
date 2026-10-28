// Package usage prints the guide in the binary, a page a topic, for one
// who has dockhand and no checkout of it (the person's "usage",
// 2026-10-28).
package usage

import (
	"fmt"
	"io/fs"
	"strings"

	"github.com/spf13/cobra"

	guide "github.com/herbygillot/dockhand/docs/usage"
	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/term"
)

// A Topic is a page of the guide, by the word that names it.
type Topic struct {
	Name, Page, About string
}

// Topics are the guide's pages, the first printed with no topic, in the
// order docs/usage.md lists them.
var Topics = []Topic{
	{"", "setup.md", "first setup and a release update through to its pull request"},
	{"branches", "branches.md", "find, start, adopt, rebase, restore, and retire work"},
	{"hand-edits", "hand-edits.md", "edit Portfiles, refresh checksums, and request rebuilds"},
	{"checks", "checks.md", "choose files, targets, tests, and build environments"},
	{"publishing", "publishing.md", "shape commits, assess evidence, and submit or update a PR"},
	{"recovery", "recovery.md", "diagnose holds, failed checks, interruptions, and stale evidence"},
	{"batches", "batches.md", "select many ports and manage partially completed work"},
	{"unattended", "unattended.md", "bump, background checks, serve, and unattended holds"},
	{"new-port", "new-port.md", "create and finish a Portfile for a new project"},
	{"review", "review.md", "inspect, adopt, check, and review another pull request"},
	{"agents", "agents.md", "explicit context, JSON, exit codes, scratch state, and automation"},
	{"configuration", "configuration.md", "profiles, holds, service settings, paths, and retention"},
}

// Command is dockhand usage [topic].
func Command(streams term.Streams) *cobra.Command {
	var names []string
	var listed []string
	for _, topic := range Topics[1:] {
		names = append(names, topic.Name)
		listed = append(listed, fmt.Sprintf("  %-14s %s", topic.Name, topic.About))
	}
	return &cobra.Command{
		Use:   "usage [topic]",
		Short: "Print the guide: setup and the path to a pull request, or a topic",
		Long: `Prints dockhand's guide, the pages docs/usage keeps, from the binary.
With no topic, it's the one-time setup and the straight path from an
update to its pull request. Each topic develops a workflow or a set of
choices across commands:

` + strings.Join(listed, "\n") + `

dockhand <command> --help is the reference for any one command.`,
		Args:      cobra.MaximumNArgs(1),
		ValidArgs: names,
		RunE: func(_ *cobra.Command, args []string) error {
			name := ""
			if len(args) > 0 {
				name = args[0]
			}
			text, err := Page(name)
			if err != nil {
				return err
			}
			// Preserve Markdown and copyable shell examples verbatim.
			streams.Unwrapped()
			_, err = fmt.Fprint(streams.Out, text)
			return err
		},
	}
}

// Page is a topic's page, or the first with none.
func Page(name string) (string, error) {
	for _, topic := range Topics {
		if topic.Name == name {
			data, err := fs.ReadFile(guide.Pages, topic.Page)
			return string(data), err
		}
	}
	var names []string
	for _, topic := range Topics[1:] {
		names = append(names, topic.Name)
	}
	return "", fmt.Errorf("no usage topic %s: the topics are %s", name, prose.And(names))
}
