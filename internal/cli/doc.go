// Package cli defines Dockhand's Cobra commands and their user-facing behavior.
//
// It parses flags and selectors, invokes application services, and renders human
// or JSON results, progress, and logs. Commands select whether to attach through
// admission or completion; proc drives the cycles and workflow owns durable
// transitions. Run returns errors for the entry point to map through ExitCode.
package cli
