// dockhand is a command-line tool for MacPorts maintainers.
// From upstream release to upstream PR.
package main

import (
	"os"

	"github.com/herbygillot/dockhand/internal/cmd"
)

// Version is a var, not a const: the linker's -X can only overwrite a
// variable, and it fails silently on anything else.
var Version = "0.0.0-dev"

func main() {
	os.Exit(cmd.Execute(Version))
}
