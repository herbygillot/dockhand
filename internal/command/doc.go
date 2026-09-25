// Package command is dockhand's command line, rebuilt for Design v3
// (docs/design-v3.md).
//
// It parses arguments and renders results. The engine does the work, so a
// command binds a person's intent and reports what happened, and nothing
// here decides workflow policy. Until the v3 core lands (roadmap step 5),
// the command line holds only the root command.
package command
