package author

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/command/branchflags"
	"github.com/herbygillot/dockhand/internal/command/settings"
	"github.com/herbygillot/dockhand/internal/command/view"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/term"
)

// RenameCommand renames a port as MacPorts does, an obsolete stub left at
// its old name (the person's "I agree we add a `rename` action to
// Dockhand.", 2026-10-29).
func RenameCommand(s *settings.Settings, streams term.Streams) *cobra.Command {
	var where branchflags.BranchChoice
	var category string
	var plan bool
	cmd := &cobra.Command{
		Use:   "rename <port> <new name>",
		Short: "Rename a port, leaving an obsolete stub at its old name",
		Long: `Renames a port as MacPorts does: its Portfile and files/ move to the new
name's directory, in its category unless --category names another, with the
new name set where the Portfile sets its name (its name line, or the setup its
name comes from, such as github.setup or go.setup), and its old Portfile
becomes a stub: PortGroup obsolete 1.0, replaced_by the new name, at its
version with its revision raised. It's one edit, so tidy commits it as
"<new>: rename from <old>", and check and submit take the pair as one change.

It does the mechanical part alone. What the new name implies inside the
Portfile, a Go module's path, a binary's name, notes, is yours, and the lines
still naming the old name are said, as are the ports that depend on it. A new
release is dockhand update <new name> after.

The branch is --branch, else the one checked out here; --new starts one.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return renamePort(cmd, s, streams, where, engine.PortRenameRequest{Port: args[0], Name: args[1], Category: category, Plan: plan})
		},
	}
	where.Register(cmd)
	cmd.Flags().StringVar(&category, "category", "", "the category of the new name's directory, where it isn't the port's own")
	cmd.Flags().BoolVar(&plan, "plan", false, "show what the rename would write, and change nothing")
	settings.SupportsJSON(cmd)
	return cmd
}

// MoveCommand moves a port to another category's directory, keeping its
// name: field testing ran git mv and edited categories by hand, which
// tidy took as a hand edit (2026-10-29).
func MoveCommand(s *settings.Settings, streams term.Streams) *cobra.Command {
	var where branchflags.BranchChoice
	var plan bool
	cmd := &cobra.Command{
		Use:   "move <port> <category>",
		Short: "Move a port to another category",
		Long: `Moves a port's Portfile and files/ to the category's directory, keeping its
name, and names the category first on its categories line where the old one
was. It's one edit, so tidy commits it as "<port>: move to <category>", and
check and submit take it as a move.

The branch is --branch, else the one checked out here; --new starts one.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return renamePort(cmd, s, streams, where, engine.PortRenameRequest{Port: args[0], Category: args[1], Plan: plan})
		},
	}
	where.Register(cmd)
	cmd.Flags().BoolVar(&plan, "plan", false, "show what the move would write, and change nothing")
	settings.SupportsJSON(cmd)
	return cmd
}

// renameJSON is a rename's or move's result.
type renameJSON struct {
	Branch            view.BranchRefJSON `json:"branch"`
	Started           bool               `json:"started"`
	Port              string             `json:"port"`
	Name              string             `json:"name"`
	From              string             `json:"from"`
	To                string             `json:"to"`
	Stub              string             `json:"stub,omitempty"`
	Left              []string           `json:"left"`
	Dependents        []string           `json:"dependents"`
	DependentsProblem string             `json:"dependents_problem,omitempty"`
	Applied           bool               `json:"applied"`
}

func renamePort(cmd *cobra.Command, s *settings.Settings, streams term.Streams, where branchflags.BranchChoice, request engine.PortRenameRequest) error {
	ctx := cmd.Context()
	e, err := s.Open(ctx)
	if err != nil {
		return err
	}
	defer e.Close()
	chosen, err := branchflags.ChooseBranch(ctx, e, streams, where, request.Port, engine.WorkEdit)
	if err != nil {
		return err
	}
	request.Branch = chosen.Branch
	out := streams.Out
	if chosen.Started {
		fmt.Fprintf(out, "Started %s from master %s (fetched just now)\n", chosen.Branch.Name, chosen.Branch.Base.Short())
	}
	renamed, err := e.RenamePort(ctx, request)
	if err != nil {
		return err
	}
	result := renameJSON{Branch: view.BranchRef(chosen.Branch), Started: chosen.Started, Port: renamed.Port, Name: renamed.Name, From: renamed.From, To: renamed.To,
		Left: view.NonNil(renamed.Left), Dependents: view.NonNil(renamed.Dependents), DependentsProblem: renamed.DependentsProblem, Applied: renamed.Applied}
	verb := map[bool]string{true: "would rename", false: "renamed"}[request.Plan]
	what := fmt.Sprintf("%s %s to %s: %s → %s", verb, renamed.Port, renamed.Name, renamed.From, renamed.To)
	if renamed.Stub == nil {
		verb = map[bool]string{true: "would move", false: "moved"}[request.Plan]
		what = fmt.Sprintf("%s %s: %s → %s", verb, renamed.Port, renamed.From, renamed.To)
	} else {
		result.Stub = fmt.Sprintf("%s_%d", renamed.Stub.Version, renamed.Stub.Revision)
		what += fmt.Sprintf(", with an obsolete stub at %s, replaced_by %s, version %s", renamed.From, renamed.Name, result.Stub)
	}
	fmt.Fprintf(out, "%s · %s\n", chosen.Branch.ShortName(), what)
	if renamed.Category {
		fmt.Fprintf(out, "    Its categories line names %s first.\n", strings.SplitN(renamed.To, "/", 2)[0])
	}
	if len(renamed.Left) > 0 {
		fmt.Fprintf(out, "    Still naming %s in %s/Portfile, yours to change:\n", renamed.Port, renamed.To)
		for _, line := range renamed.Left {
			fmt.Fprintf(out, "      %s\n", line)
		}
	}
	switch {
	case len(renamed.Dependents) > 0:
		fmt.Fprintf(out, "    %s on %s, by the port index; their lines are yours: %s\n", map[bool]string{true: "1 port depends", false: prose.Plural(len(renamed.Dependents), "port") + " depend"}[len(renamed.Dependents) == 1], renamed.Port, strings.Join(renamed.Dependents, ", "))
	case renamed.DependentsProblem != "":
		fmt.Fprintf(out, "    The ports depending on %s couldn't be read: %s\n", renamed.Port, renamed.DependentsProblem)
	}
	streams.Emit(result)
	if request.Plan {
		fmt.Fprintln(out, "Plan, nothing changed.")
		return nil
	}
	next := []string{"dockhand check"}
	if renamed.Stub != nil {
		next = []string{"dockhand update " + renamed.Name + " (for a new release)", "dockhand check"}
	}
	fmt.Fprintln(out, "Next: "+branchflags.NextIn(ctx, e, chosen.Branch, next...))
	return nil
}
