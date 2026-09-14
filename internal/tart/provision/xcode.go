package provision

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/tart"
)

type xcodeArchive struct {
	Path       string
	Version    string
	Preference int
}

func selectXcode(path string, release tart.MacOSRelease) (string, string, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return "", "", err
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return "", "", fmt.Errorf("setup: Xcode archive path: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", "", fmt.Errorf("setup: Xcode archive path: %w", err)
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
		return "", "", fmt.Errorf("setup: Xcode path must be a regular .xip file or directory")
	}
	if len(candidates) == 0 {
		return "", "", fmt.Errorf("setup: no release Xcode archives found in %s", path)
	}
	bound := xcodeUpperBound(release.Darwin)
	var selected xcodeArchive
	for _, candidate := range candidates {
		if bound != "" && compareNumericVersion(candidate.Version, bound) >= 0 {
			continue
		}
		comparison := compareNumericVersion(candidate.Version, selected.Version)
		if selected.Path == "" || comparison > 0 || comparison == 0 && candidate.Preference > selected.Preference {
			selected = candidate
		}
	}
	if selected.Path == "" {
		return "", "", fmt.Errorf("setup: no Xcode archive can run on %s; Xcode must be below %s", release.Name, bound)
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

func xcodeUpperBound(darwin int) string {
	return map[int]string{21: "14.3", 22: "15.3", 23: "16.3", 24: "26.4"}[darwin]
}
