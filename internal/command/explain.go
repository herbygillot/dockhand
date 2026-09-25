package command

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/macports/commitrules"
)

func explainCommand(streams Streams) *cobra.Command {
	return &cobra.Command{
		Use:   "explain [code]",
		Short: "Say what a commit rule asks, and where MacPorts asks it",
		Long: `Explains a finding's code, the word in brackets at the end of a tidy or
submit finding, such as [follow-up]: what dockhand checks, and the MacPorts
documents behind it, quoted where dockhand carries their words. With no
code, it lists them.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			out := streams.Out
			if len(args) == 0 {
				for _, code := range commitrules.Codes() {
					explanation, _ := commitrules.Explain(code)
					fmt.Fprintf(out, "%-22s %s\n", code, firstSentence(explanation.Rule))
				}
				return nil
			}
			explanation, ok := commitrules.Explain(args[0])
			if !ok {
				return fmt.Errorf("no rule is called %q; dockhand explain lists them", args[0])
			}
			fmt.Fprintf(out, "%s\n\n%s\n", explanation.Code, wrap(explanation.Rule, 76))
			for _, source := range explanation.Sources {
				fmt.Fprintf(out, "\n%s\n%s\n", source.Title, source.URL)
				if source.Quote == "" {
					fmt.Fprintln(out, "  (not quoted here; the link has its text)")
					continue
				}
				for _, line := range strings.Split(wrap("“"+source.Quote+"”", 72), "\n") {
					fmt.Fprintf(out, "  %s\n", line)
				}
			}
			return nil
		},
	}
}

// firstSentence is a rule's first sentence, for the list.
func firstSentence(rule string) string {
	if i := strings.Index(rule, ". "); i >= 0 {
		return rule[:i+1]
	}
	return rule
}

// wrap breaks text into lines of at most width runes, between words.
func wrap(text string, width int) string {
	var lines []string
	line := ""
	for _, word := range strings.Fields(text) {
		if line != "" && len([]rune(line))+1+len([]rune(word)) > width {
			lines = append(lines, line)
			line = ""
		}
		if line != "" {
			line += " "
		}
		line += word
	}
	return strings.Join(append(lines, line), "\n")
}
