package check

import (
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
)

// variantsFlag reads check's --variants: "each", or variants as MacPorts'
// command line takes them (macports.ParseVariants); none where empty.
func variantsFlag(value string) (map[string]bool, bool, error) {
	switch strings.TrimSpace(value) {
	case "":
		return nil, false, nil
	case "each":
		return nil, true, nil
	}
	variants, err := macports.ParseVariants(value)
	if err != nil {
		return nil, false, fmt.Errorf("--variants: %w", err)
	}
	return variants, false, nil
}
