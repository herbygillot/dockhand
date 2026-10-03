// Package prose words counts and sizes the same way wherever dockhand
// says them.
package prose

import (
	"fmt"
	"strings"
)

// Plural is a count of a noun: "1 port", "2 ports", "3 patches".
func Plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	for _, ending := range []string{"s", "x", "ch", "sh"} {
		if strings.HasSuffix(noun, ending) {
			return fmt.Sprintf("%d %ses", n, noun)
		}
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// Bytes is a size in SI units, as Finder and df -H count them: "820 MB",
// "1.4 GB", "12 KB", "512 bytes". Below ten of a unit it keeps a tenth,
// where the tenth isn't nought: "30 GB", "1 KB" of 1,024 bytes.
func Bytes(n int64) string {
	units := []string{"KB", "MB", "GB", "TB"}
	if n < 1000 && n > -1000 {
		return Plural(int(n), "byte")
	}
	value, unit := float64(n), ""
	for _, next := range units {
		value /= 1000
		unit = next
		if value < 1000 && value > -1000 {
			break
		}
	}
	if value >= 10 || value <= -10 {
		return fmt.Sprintf("%.0f %s", value, unit)
	}
	return strings.TrimSuffix(fmt.Sprintf("%.1f", value), ".0") + " " + unit
}

// Few is a list as a line can carry it: the whole list where it has at
// most shown items, and else its count and its first few, "18 ports:
// terraform-1.16, terraform-1.17, terraform-1.18 and 15 more", as review
// says a port's dependents (the command-line UX review's §10).
func Few(items []string, shown int, noun string) string {
	return FewJoined(items, shown, noun, ", ")
}

// FewJoined is Few with the items joined by sep, as an order's " → ".
func FewJoined(items []string, shown int, noun, sep string) string {
	if len(items) <= shown {
		return strings.Join(items, sep)
	}
	return fmt.Sprintf("%s: %s and %d more", Plural(len(items), noun), strings.Join(items[:shown], sep), len(items)-shown)
}
