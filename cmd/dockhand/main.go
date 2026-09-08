// dockhand is a command-line tool for MacPorts maintainers.
// From upstream release to submitted port.
package main

import (
	"os"

	"github.com/herbygillot/dockhand/internal/cli"
)

// Version is a var, not a const: the linker's -X can only overwrite a
// variable, and it fails silently on anything else.
var Version = "0.0.0-dev"

func main() {
	os.Exit(cli.Execute(Version))
}
