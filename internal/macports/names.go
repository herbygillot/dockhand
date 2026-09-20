package macports

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ValidName reports whether a port, subport, or variant name is a single safe
// token. "." and ".." are refused: they are path segments, never port names,
// and admitting them turns a mistyped directory into a lookup that fails much
// later saying no such contribution exists rather than that this is not a name.
func ValidName(value string) bool {
	if value == "." || value == ".." {
		return false
	}
	return value != "" && utf8.ValidString(value) && !strings.ContainsAny(value, "/\\") && strings.IndexFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) < 0
}
func validateVariants(variants map[string]bool) error {
	for name := range variants {
		if !ValidName(name) || strings.HasPrefix(name, "+") || strings.HasPrefix(name, "-") {
			return fmt.Errorf("macports: invalid variant %q", name)
		}
	}
	return nil
}
