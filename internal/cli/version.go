package cli

import "github.com/herbygillot/dockhand/internal/version"

// buildVersion is what --version prints.
func buildVersion() string { return version.Current().String() }
