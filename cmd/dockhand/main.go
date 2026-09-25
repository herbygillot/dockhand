package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/herbygillot/dockhand/internal/command"
	"github.com/herbygillot/dockhand/internal/scratch"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err := command.Run(ctx, os.Args[1:], command.Streams{In: os.Stdin, Out: os.Stdout, Err: os.Stderr})
	// The process's transient directories go with it, on an interrupt too:
	// the cancelled context has unwound every command by now.
	if closeErr := scratch.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
