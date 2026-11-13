package main

import (
	"context"
	"fmt"
	"os"
	"runtime/pprof"

	"github.com/herbygillot/dockhand/internal/buildenv/starter"
	"github.com/herbygillot/dockhand/internal/command"
	"github.com/herbygillot/dockhand/internal/command/settings"
	"github.com/herbygillot/dockhand/internal/scratch"
	"github.com/herbygillot/dockhand/internal/term"
)

func main() {
	// dockhand as the starter of a local or a script build runs nothing
	// else (starter.Command).
	starter.RunIfAsked()
	ctx, stop := term.UntilStopped(context.Background())
	defer stop()
	stopProfile := startProfile(os.Getenv("DOCKHAND_CPUPROFILE"))
	err := command.Run(ctx, os.Args[1:], term.Streams{In: os.Stdin, Out: os.Stdout, Err: os.Stderr})
	stopProfile()
	if file := os.Getenv(settings.HeapProfileVariable); file != "" {
		if heapErr := settings.WriteHeapProfile(file); heapErr != nil {
			fmt.Fprintf(os.Stderr, "%s: could not write heap profile: %v\n", settings.HeapProfileVariable, heapErr)
		}
	}
	// The process's transient directories go with it, on an interrupt too:
	// the cancelled context has unwound every command by now.
	if closeErr := scratch.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		code := term.ExitCode(err)
		if ctx.Err() != nil && code == 1 {
			code = 130
		}
		if err.Error() != "" {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(code)
	}
}

// startProfile writes the process's CPU profile, as runtime/pprof makes
// one, to the file DOCKHAND_CPUPROFILE names, for a measurement such as an
// idle serve's (the roadmap's "An idle serve costs little"); the function
// it returns finishes it, before any exit. Without a file, it does nothing.
func startProfile(file string) func() {
	if file == "" {
		return func() {}
	}
	out, err := os.Create(file)
	if err == nil {
		if err = pprof.StartCPUProfile(out); err != nil {
			out.Close()
		}
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "DOCKHAND_CPUPROFILE: could not start CPU profiling: %v\n", err)
		return func() {}
	}
	return func() {
		pprof.StopCPUProfile()
		out.Close()
	}
}
