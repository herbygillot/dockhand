package check

import (
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
)

// variantsFlag reads check's --variants: "each", or variants as MacPorts'
// command line takes them (macports.ParseVariants), either after the port
// they build, a bare word first, as 'curl +gnutls +sectrust' or 'curl
// each' (field testing's curl +sectrust notes, note 1); none where empty.
func variantsFlag(value string) (port string, variants map[string]bool, each bool, err error) {
	fields := strings.Fields(value)
	if len(fields) > 0 && !strings.HasPrefix(fields[0], "+") && !strings.HasPrefix(fields[0], "-") && fields[0] != "each" {
		port, fields = fields[0], fields[1:]
		if len(fields) == 0 {
			return "", nil, false, fmt.Errorf("--variants: %q names a port and no variants after it; a variant is +name or -name, as --variants '%s +name'", port, port)
		}
	}
	switch strings.Join(fields, " ") {
	case "":
		return "", nil, false, nil
	case "each":
		return port, nil, true, nil
	}
	variants, err = macports.ParseVariants(strings.Join(fields, " "))
	if err != nil {
		return "", nil, false, fmt.Errorf("--variants: %w", err)
	}
	return port, variants, false, nil
}
