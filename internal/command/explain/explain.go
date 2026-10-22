package explain

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/command/settings"
	"github.com/herbygillot/dockhand/internal/macports/commitrules"
	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/term"
)

func Command(streams term.Streams) *cobra.Command {
	cmd := &cobra.Command{
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
				streams.Emit(map[string]any{"codes": commitrules.Codes()})
				for _, code := range commitrules.Codes() {
					explanation, _ := commitrules.Explain(code)
					fmt.Fprintf(out, "%-22s %s\n", code, prose.FirstSentence(explanation.Rule))
				}
				return nil
			}
			explanation, ok := commitrules.Explain(args[0])
			if !ok {
				return fmt.Errorf("no rule is called %q; dockhand explain lists them", args[0])
			}
			var sources []map[string]string
			for _, source := range explanation.Sources {
				sources = append(sources, map[string]string{"title": source.Title, "url": source.URL, "quote": source.Quote})
			}
			streams.Emit(map[string]any{"code": explanation.Code, "rule": explanation.Rule, "sources": sources})
			fmt.Fprintf(out, "%s\n\n%s\n", explanation.Code, prose.Wrap(explanation.Rule, 76))
			for _, source := range explanation.Sources {
				fmt.Fprintf(out, "\n%s\n%s\n", source.Title, source.URL)
				if source.Quote == "" {
					fmt.Fprintln(out, "  (not quoted here; the link has its text)")
					continue
				}
				for _, line := range strings.Split(prose.Wrap("“"+source.Quote+"”", 72), "\n") {
					fmt.Fprintf(out, "  %s\n", line)
				}
			}
			return nil
		},
	}
	settings.SupportsJSON(cmd)
	return cmd
}
