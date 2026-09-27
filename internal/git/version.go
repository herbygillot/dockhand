package git

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/subprocess"
)

// Version is a Git's version, as git version reports it.
type Version struct {
	Major, Minor, Patch int
	// Reported is all it said: "git version 2.54.0 (Apple Git-157)".
	Reported string
	// Path is the executable that said it.
	Path string
}

// MinimumVersion is the oldest Git dockhand works with: 2.40, whose
// merge-tree takes --merge-base, with which rebase replays a branch's
// commits (Replay).
var MinimumVersion = Version{Major: 2, Minor: 40}

// ExecutableVersion runs a Git executable, "git" on PATH when empty, and
// reads its version.
func ExecutableVersion(ctx context.Context, executable string) (Version, error) {
	if executable == "" {
		executable = "git"
	}
	path, err := exec.LookPath(executable)
	if err != nil {
		return Version{}, err
	}
	result, err := subprocess.Run(ctx, subprocess.Spec{Tool: "git", Command: "version", Path: path, Args: []string{"version"}})
	if err != nil {
		return Version{}, err
	}
	version, err := parseVersion(strings.TrimSpace(string(result.Output)))
	version.Path = path
	return version, err
}

// parseVersion reads "git version 2.54.0 (Apple Git-157)". A part with a
// suffix, 2.40.0.rc1 or 2.39.5.windows.1, counts by its leading digits.
func parseVersion(reported string) (Version, error) {
	version := Version{Reported: reported}
	words := strings.Fields(reported)
	if len(words) < 3 || words[0] != "git" || words[1] != "version" {
		return version, fmt.Errorf("git: unreadable version %q", reported)
	}
	parts := strings.Split(words[2], ".")
	numbers := []*int{&version.Major, &version.Minor, &version.Patch}
	for i := 0; i < len(numbers) && i < len(parts); i++ {
		digits := strings.TrimRightFunc(parts[i], func(r rune) bool { return r < '0' || r > '9' })
		n, err := strconv.Atoi(digits)
		if err != nil {
			if i < 2 {
				return version, fmt.Errorf("git: unreadable version %q", reported)
			}
			break
		}
		*numbers[i] = n
	}
	return version, nil
}

// AtLeast reports whether the version is minimum or newer.
func (v Version) AtLeast(minimum Version) bool {
	if v.Major != minimum.Major {
		return v.Major > minimum.Major
	}
	if v.Minor != minimum.Minor {
		return v.Minor > minimum.Minor
	}
	return v.Patch >= minimum.Patch
}

// String is the version's number: 2.54.0, or 2.40 for a minimum.
func (v Version) String() string {
	if v.Patch == 0 && v.Reported == "" {
		return fmt.Sprintf("%d.%d", v.Major, v.Minor)
	}
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}
