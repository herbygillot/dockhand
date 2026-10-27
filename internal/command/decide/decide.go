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
		Short: "Answer a concern that holds a branch, with your reason",
		Long: `Answers one concern that holds the branch's submission, named by the
rule that raised it, such as license-changed, as status and submit's
preview show it, for the port -p names, the one named after the rule,
or the branch's one port.

--because "<reason>" takes it as acceptable: a submission nobody looks
over, serve's, ship's, or bump's, no longer waits on it, status and
submit still say it, and the pull request's description gives your reason
under Reviewed. --waiting "<what for>" leaves it for later: submit refuses
the branch until you decide it --because, or its files change.

A decision rests on the port's own files, its Portfile and files/: it
lapses when they change, as with the port's next version, and a change to
another port of the branch leaves it standing. Deciding again replaces
it.`,
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
			fmt.Fprintf(streams.Out, "%s: %s\n", branch.ShortName(), engine.DecisionWords(decision))
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
