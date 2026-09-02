package cmd

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/runstate"
)

// Action is one invocation's behavior: what a subcommand does once its
// flags are parsed into it. Each subcommand builds an Action from the
// command line and executes it against the run; a test constructs one
// directly and skips the command line entirely.
//
// An Action is where orchestration lives — invoking one or more
// intents and the workflow around them — and it shares the
// runstate.Context with exactly one thing it calls: the lifecycle
// engine, which needs the run's streams, seams, and services
// throughout. Everything else takes narrow arguments: an evaluator, a
// fetcher, a portdir. The planned Engine refactor (S5 in the overhaul
// plan) constructs the engine from the Context once, and an Action
// becomes the last layer that sees one. Usage-shaped validation
// (mutually exclusive flags, malformed arguments) belongs in the
// builder at the cobra boundary, where UsageError lives; what remains
// in Execute is the behavior.
type Action interface {
	Execute(ctx context.Context, rs *runstate.Context) error
}

// runE adapts an Action builder to cobra: parse the invocation into an
// Action, then execute it against the run. The run's Context is not a
// parameter — the root command stored it on the command context when
// the global flags were parsed, so constructors stay pure grammar and
// the run state appears exactly where execution begins.
func runE(build func(cmd *cobra.Command, args []string) (Action, error)) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		a, err := build(cmd, args)
		if err != nil {
			return err
		}
		return a.Execute(cmd.Context(), runstate.From(cmd.Context()))
	}
}
