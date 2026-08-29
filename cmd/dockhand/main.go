// Command dockhand is a port maintenance utility for MacPorts. This is
// a thin entry point; the command tree and exit-code table live in
// internal/cmd.
package main

import (
	"os"

	"github.com/herbygillot/dockhand/internal/cmd"
)

const version = "0.0.0-dev"

func main() {
	os.Exit(cmd.Execute(version))
}
