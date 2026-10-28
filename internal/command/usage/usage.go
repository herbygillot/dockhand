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
	{"", "setup.md", "the one-time setup, and the straight path from an update to its pull request"},
	{"hand-edits", "hand-edits.md", "edits dockhand doesn't make, and trying a branch's port with MacPorts"},
	{"unattended", "unattended.md", "bump, serve, and what holds a submission nobody looks over"},
	{"checks", "checks.md", "what a check builds and where, its results, logs, and baselines"},
	{"review", "review.md", "someone else's pull request"},
	{"batches", "batches.md", "many ports at once"},
	{"new-port", "new-port.md", "a port MacPorts doesn't have"},
	{"agents", "agents.md", "--json, exit codes, a scratch setup, and what acts beyond this Mac"},
}

// Command is dockhand usage [topic].
func Command(streams term.Streams) *cobra.Command {
	var names []string
	var listed []string
	for _, topic := range Topics[1:] {
		names = append(names, topic.Name)
		listed = append(listed, fmt.Sprintf("  %-12s %s", topic.Name, topic.About))
	}
	return &cobra.Command{
		Use:   "usage [topic]",
		Short: "Print the guide: setup and the path to a pull request, or a topic",
		Long: `Prints dockhand's guide, the pages docs/usage keeps, from the binary.
With no topic, it's the one-time setup and the straight path from an
update to its pull request. A topic is a workflow across commands:

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
