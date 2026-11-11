// Package decide is dockhand decide: a person's decision on one concern
// that holds a branch, with their reason (the person's voice, Then's item
// 13).
package decide

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/command/branchflags"
	"github.com/herbygillot/dockhand/internal/command/settings"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/term"
)

func Command(s *settings.Settings, streams term.Streams) *cobra.Command {
	var where branchflags.BranchFlags
	var because, waiting string
	cmd := &cobra.Command{
		Use:   "decide <rule> [<port>]",
		Short: "Record your decision and reason for a submission concern",
		Long: `Answers a concern shown by status or submit's preview, using its rule
name, such as license-changed. Select the port with -p, name it after the
rule, or omit it when the branch changes only one port.

--because "<reason>" accepts the concern so unattended serve, ship, or bump
can proceed. Status and submit still show it, and the pull request includes
your reason under Reviewed. --waiting "<what for>" blocks submission until
you replace it with --because or the port's files change.

A decision applies to the port's current Portfile and files/. It expires
when those files change; edits to another port do not expire it. A decision
about a moving source also applies to the specific commits reviewed.
Deciding again replaces the previous decision.`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if (because == "") == (waiting == "") {
				return fmt.Errorf("say why: --because \"<reason>\", or --waiting \"<what for>\"")
			}
			ctx := cmd.Context()
			e, err := s.Open(ctx)
			if err != nil {
				return err
			}
			defer e.Close()
			branch, err := where.Resolve(ctx, e, streams)
			if err != nil {
				return err
			}
			port := where.Port
			if len(args) == 2 {
				port = args[1]
			}
			request := engine.DecideRequest{Branch: branch, Rule: args[0], Port: port, Answer: model.Accepted, Words: because}
			if waiting != "" {
				request.Answer, request.Words = model.Waiting, waiting
			}
			decision, err := e.Decide(ctx, request)
			if err != nil {
				return err
			}
			streams.Emit(map[string]any{"branch": branch.ShortName(), "subject": decision.Subject, "answer": string(decision.Answer), "words": decision.Words})
			fmt.Fprintf(streams.Prose(), "%s: %s\n", branch.ShortName(), engine.DecisionWords(decision))
			return nil
		},
	}
	where.Register(cmd, s, "decide on")
	cmd.Flags().StringVar(&because, "because", "", "take the concern as acceptable, for this reason")
	cmd.Flags().StringVar(&waiting, "waiting", "", "leave the concern for later, awaiting this")
	cmd.MarkFlagsMutuallyExclusive("because", "waiting")
	settings.SupportsJSON(cmd)
	return cmd
}
