package main

import (
	"context"
	"fmt"
	"os"

	"github.com/herbygillot/dockhand/v2/internal/cli"
)

func main() {
	streams := cli.Streams{In: os.Stdin, Out: os.Stdout, Err: os.Stderr}
	if err := cli.Run(context.Background(), os.Args[1:], streams, nil); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
