package cli

import (
	"github.com/herbygillot/dockhand/internal/app"
	"github.com/spf13/cobra"
)

// NewRoot builds the command tree with the real services, for the tests
// that walk it; commands run through Run.
func NewRoot(config app.Config) (*cobra.Command, error) {
	root, _, err := newRoot(config, app.Build)
	return root, err
}
