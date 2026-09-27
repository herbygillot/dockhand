package macos

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type xcodeArchive struct {
	Path       string
	Version    string
	Preference int
}

// SelectXcode chooses the Xcode archive a release's Xcode image installs:
// the archive at path, or the newest release archive in the folder at path
// that fits the release. An Xcode fits when it runs there, below the
// release's upper bound, and is no older than the release's own tools
// generation (Tools, from the facts table): an older Xcode lacks the
// release's SDK, so Golden Gate, generation 27, never gets Xcode 26.6.
// Newer generations fit, as Sequoia's Xcode 26 does over its tools' 16.
// Betas and release candidates are never chosen.
func SelectXcode(path string, release Release) (string, string, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return "", "", err
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return "", "", fmt.Errorf("macos: Xcode archive path: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", "", fmt.Errorf("macos: Xcode archive path: %w", err)
	}
	var candidates []xcodeArchive
	if info.IsDir() {
		entries, err := os.ReadDir(path)
		if err != nil {
			return "", "", err
		}
		for _, entry := range entries {
			if entry.Type().IsRegular() {
				if candidate, ok := parseXcodeArchive(filepath.Join(path, entry.Name())); ok {
					candidates = append(candidates, candidate)
				}
			}
		}
	} else if info.Mode().IsRegular() {
		if candidate, ok := parseXcodeArchive(path); ok {
			candidates = append(candidates, candidate)
		}
	} else {
		return "", "", fmt.Errorf("macos: Xcode path must be a regular .xip file or directory")
	}
	if len(candidates) == 0 {
		return "", "", fmt.Errorf("macos: no release Xcode archives found in %s", path)
	}
	bound := xcodeUpperBound(release.Darwin)
	floor := ""
	if release.Tools > 0 {
		floor = strconv.Itoa(release.Tools)
	}
	var selected xcodeArchive
	for _, candidate := range candidates {
		if bound != "" && compareNumericVersion(candidate.Version, bound) >= 0 {
			continue
		}
		if floor != "" && compareNumericVersion(candidate.Version, floor) < 0 {
			continue
		}
		comparison := compareNumericVersion(candidate.Version, selected.Version)
		if selected.Path == "" || comparison > 0 || comparison == 0 && candidate.Preference > selected.Preference {
			selected = candidate
		}
	}
	if selected.Path == "" {
		var limits []string
		if floor != "" {
			limits = append(limits, "at least "+floor)
		}
		if bound != "" {
			limits = append(limits, "below "+bound)
		}
		return "", "", fmt.Errorf("macos: no Xcode archive fits %s; Xcode must be %s", release.Name, strings.Join(limits, " and "))
	}
	return selected.Path, selected.Version, nil
}

func parseXcodeArchive(path string) (xcodeArchive, bool) {
	name := filepath.Base(path)
	value, ok := strings.CutPrefix(name, "Xcode_")
	if !ok {
		return xcodeArchive{}, false
	}
	value, ok = strings.CutSuffix(value, ".xip")
	if !ok {
		return xcodeArchive{}, false
	}
	preference := 1
	lower := strings.ToLower(value)
	for _, suffix := range []struct {
		value      string
		preference int
	}{{"_apple_silicon", 3}, {"_universal", 2}} {
		if strings.HasSuffix(lower, suffix.value) {
			value = value[:len(value)-len(suffix.value)]
			preference = suffix.preference
			break
		}
	}
	if _, ok := numericVersion(value); !ok {
		return xcodeArchive{}, false
	}
	return xcodeArchive{Path: path, Version: value, Preference: preference}, true
}

func numericVersion(value string) ([]int, bool) {
	parts := strings.Split(value, ".")
	if len(parts) == 0 {
		return nil, false
	}
	result := make([]int, len(parts))
	for i, part := range parts {
		if part == "" {
			return nil, false
		}
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 {
			return nil, false
		}
		result[i] = number
	}
	return result, true
}

func compareNumericVersion(left, right string) int {
	a, _ := numericVersion(left)
	b, _ := numericVersion(right)
	for i := 0; i < max(len(a), len(b)); i++ {
		var x, y int
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x < y {
			return -1
		}
		if x > y {
			return 1
		}
	}
	return 0
}

// SameXcode reports whether two Xcode versions are one: Xcode_27.xip's 27
// is the 27.0 the installed Xcode reports. A version that isn't numeric
// matches only itself.
func SameXcode(a, b string) bool {
	_, okA := numericVersion(a)
	_, okB := numericVersion(b)
	if !okA || !okB {
		return a == b
	}
	return compareNumericVersion(a, b) == 0
}

func xcodeUpperBound(darwin int) string {
	return map[int]string{21: "14.3", 22: "15.3", 23: "16.3", 24: "26.4"}[darwin]
}
