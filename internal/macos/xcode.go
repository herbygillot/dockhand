package macos

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type xcodeArchive struct {
	Path       string
	Version    string
	Preference int
}

// SelectXcode chooses the archive of an Xcode version for a release's
// Xcode image: the archive at path, or the one of that version in the
// folder at path, Apple silicon's before the universal one. The version is
// the one MacPorts' arm64 builder for the release runs (Release.Xcode)
// unless the configuration names another, so an Xcode image matches where
// MacPorts builds its packages, whatever newer Xcode the folder holds. It
// must run on the release, below the release's upper bound. Betas and
// release candidates are never chosen.
func SelectXcode(path string, release Release, version string) (string, string, error) {
	if version == "" {
		return "", "", fmt.Errorf("macos: no Xcode is set for %s: MacPorts has no arm64 builder for it that the facts table knows; name one in providers.tart.xcode", release.Name)
	}
	if _, ok := numericVersion(version); !ok {
		return "", "", fmt.Errorf("macos: Xcode %q for %s is not a version such as 26.6", version, release.Name)
	}
	if bound := xcodeUpperBound(release.Darwin); bound != "" && compareNumericVersion(version, bound) >= 0 {
		return "", "", fmt.Errorf("macos: Xcode %s doesn't run on %s; Xcode must be below %s", version, release.Name, bound)
	}
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
	var selected xcodeArchive
	for _, candidate := range candidates {
		if SameXcode(candidate.Version, version) && (selected.Path == "" || candidate.Preference > selected.Preference) {
			selected = candidate
		}
	}
	if selected.Path == "" {
		missing := &MissingXcode{Release: release, Version: version, Path: path}
		if info.IsDir() {
			missing.Folder = path
		}
		return "", "", missing
	}
	return selected.Path, selected.Version, nil
}

// MissingXcode is an Xcode a release's image needs whose archive isn't
// where setup was pointed.
type MissingXcode struct {
	Release Release
	Version string
	// Path is where setup looked, and Folder the same when it is a folder
	// an archive can be downloaded into.
	Path, Folder string
}

func (e *MissingXcode) Error() string {
	return fmt.Sprintf("macos: %s's Xcode is %s, and %s has no archive of it (Xcode_%s.xip); download Xcode %s from https://developer.apple.com/download/all/",
		e.Release.Name, e.Version, e.Path, e.Version, e.Version)
}

// CheckXcodeSignature checks an Xcode archive is Apple's, as pkgutil
// --check-signature says: "signed Apple Software".
func CheckXcodeSignature(ctx context.Context, path string) error {
	out, err := exec.CommandContext(ctx, "/usr/sbin/pkgutil", "--check-signature", path).CombinedOutput()
	if err != nil || !strings.Contains(string(out), "Status: signed Apple Software") {
		return fmt.Errorf("macos: %s isn't signed by Apple: %s", path, strings.TrimSpace(string(out)))
	}
	return nil
}

// parseXcodeArchive reads an Xcode archive's version from its name, as
// Apple names it, Xcode_16.4.xip, or as xcodes does, Xcode-16.4.0+16F6.xip.
func parseXcodeArchive(path string) (xcodeArchive, bool) {
	name := filepath.Base(path)
	if value, ok := strings.CutPrefix(name, "Xcode-"); ok {
		value, ok = strings.CutSuffix(value, ".xip")
		// A prerelease's version isn't numeric, and is never taken.
		value, _, _ = strings.Cut(value, "+")
		if _, numeric := numericVersion(value); !ok || !numeric {
			return xcodeArchive{}, false
		}
		return xcodeArchive{Path: path, Version: value, Preference: 1}, true
	}
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
