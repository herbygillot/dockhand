package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime/pprof"
	"syscall"

	"github.com/herbygillot/dockhand/internal/command"
	"github.com/herbygillot/dockhand/internal/scratch"
	"github.com/herbygillot/dockhand/internal/term"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	stopProfile := startProfile(os.Getenv("DOCKHAND_CPUPROFILE"))
	err := command.Run(ctx, os.Args[1:], term.Streams{In: os.Stdin, Out: os.Stdout, Err: os.Stderr})
	stopProfile()
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
		fmt.Fprintf(os.Stderr, "DOCKHAND_CPUPROFILE: %v; no profile is written\n", err)
		return func() {}
	}
	return func() {
		pprof.StopCPUProfile()
		out.Close()
	}
}
