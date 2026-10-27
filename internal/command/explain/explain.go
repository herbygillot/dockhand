package explain

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/command/branchflags"
	"github.com/herbygillot/dockhand/internal/command/settings"
	"github.com/herbygillot/dockhand/internal/macports/commitrules"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/term"
)

func Command(s *settings.Settings, streams term.Streams) *cobra.Command {
	var where branchflags.BranchFlags
	cmd := &cobra.Command{
		Use:   "explain [code | check]",
		Short: "Explain a finding, check, or branch status",
		Long: `Explains a finding code, a check, or a branch's status.

A finding code is the word in brackets at the end of a tidy or submit
finding, such as [follow-up]. Explaining it describes the rule Dockhand
checks and the MacPorts documentation behind it, quoted where Dockhand
carries its words. With nothing named, explain lists the codes.

A check ID, such as check-42, explains the check's result or progress:
where it failed, the last lines of the failing step, test failures the
test policy allowed, and whether a baseline check could tell more; or
what it is doing, or where it passed.

-b, -p, or --pr selects a branch and explains its status: what needs
attention, what is holding it, and the next steps; or what it is doing or
waiting for.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			out := streams.Out
			named := len(args) > 0 && model.IsRunName(args[0])
			if where.Explicit() || named {
				if where.Explicit() && len(args) > 0 {
					return fmt.Errorf("name one thing to explain: %s, or a branch with -b, -p, or --pr", args[0])
				}
				e, err := s.Open(ctx)
				if err != nil {
					return err
				}
				defer e.Close()
				if named {
					run, err := e.RunNamed(ctx, args[0])
					if err != nil {
						return err
					}
					return explainCheck(ctx, e, out, run)
				}
				branch, err := where.Resolve(ctx, e, streams)
				if err != nil {
					return err
				}
				return explainBranch(ctx, e, out, branch)
			}
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
				return fmt.Errorf("no rule is called %q; dockhand explain lists them, and explains a check named as check-42 too", args[0])
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
	where.Register(cmd, s, "explain")
	settings.SupportsJSON(cmd)
	return cmd
}
