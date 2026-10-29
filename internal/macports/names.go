package macports

import (
	"cmp"
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

// NamedAfter reports whether a port is named after a project's name: the
// port's name, case and its port group's prefix (py-, p5-, rb-, php-) set
// aside, is the name. goplus isn't named after its repository, gop; xgo
// would be after xgo (the person's rule for a port rename, 2026-10-29).
func NamedAfter(port, name string) bool {
	return name != "" && strings.TrimPrefix(strings.ToLower(port), groupPrefix(port)) == strings.ToLower(name)
}

// RenamedAfter is a port's name once its project is renamed to name: its
// port group's prefix kept, and the name in lower case, as py-foo after
// foo became bar is py-bar.
func RenamedAfter(port, name string) string {
	return groupPrefix(port) + strings.ToLower(name)
}

// groupPrefix is the prefix a port group names its ports by, py-, p5-,
// rb-, or php-, in lower case; empty where the port has none.
func groupPrefix(port string) string {
	for _, prefix := range []string{"py-", "p5-", "rb-", "php-"} {
		if strings.HasPrefix(strings.ToLower(port), prefix) {
			return prefix
		}
	}
	return ""
}

func validateVariants(variants map[string]bool) error {
	for name := range variants {
		if !ValidName(name) || strings.HasPrefix(name, "+") || strings.HasPrefix(name, "-") {
			return fmt.Errorf("macports: invalid variant %q", name)
		}
	}
	return nil
}

// ComparePortNames orders port names as a person reads them, a run of
// digits by its number: terraform-1.2 before terraform-1.10, where byte
// order put 1.10 first (field testing, 2026-10-02). Names that differ
// only in leading zeros fall back to byte order, so the order is total.
func ComparePortNames(a, b string) int {
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if isDigit(a[i]) && isDigit(b[j]) {
			x, y := i, j
			for x < len(a) && isDigit(a[x]) {
				x++
			}
			for y < len(b) && isDigit(b[y]) {
				y++
			}
			m, n := strings.TrimLeft(a[i:x], "0"), strings.TrimLeft(b[j:y], "0")
			if order := cmp.Compare(len(m), len(n)); order != 0 {
				return order
			}
			if order := strings.Compare(m, n); order != 0 {
				return order
			}
			i, j = x, y
			continue
		}
		if a[i] != b[j] {
			return cmp.Compare(a[i], b[j])
		}
		i, j = i+1, j+1
	}
	if order := cmp.Compare(len(a)-i, len(b)-j); order != 0 {
		return order
	}
	return strings.Compare(a, b)
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
