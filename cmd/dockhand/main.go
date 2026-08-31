// dockhand is a command-line tool for MacPorts maintainers.
// From upstream release to upstream PR.
package main

import (
	"os"

	"github.com/herbygillot/dockhand/internal/cmd"
)

const version = "0.0.0-dev"

func main() {
	os.Exit(cmd.Execute(version))
}
