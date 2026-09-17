package macports

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var errNotPortsTree = errors.New("not a MacPorts ports tree")

// ValidatePortsTree checks that a directory looks like a ports tree before any
// expensive work, such as index generation, is spent on it. A ports tree holds
// at least one <category>/<port>/Portfile; the check stops at the first match.
// The root may be a materialized snapshot; described names the checkout the
// user selected so the refusal points at their configuration.
func ValidatePortsTree(root, described string) error {
	categories, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, category := range categories {
		name := category.Name()
		if !category.IsDir() || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
			continue
		}
		ports, err := os.ReadDir(filepath.Join(root, name))
		if err != nil {
			continue
		}
		for _, port := range ports {
			if !port.IsDir() || strings.HasPrefix(port.Name(), ".") {
				continue
			}
			if info, err := os.Stat(filepath.Join(root, name, port.Name(), "Portfile")); err == nil && info.Mode().IsRegular() {
				return nil
			}
		}
	}
	if described == "" {
		described = root
	}
	return fmt.Errorf("%w: %s has no <category>/<port>/Portfile; pass --tree /path/to/macports-ports or set MACPORTS_TREE", errNotPortsTree, described)
}
