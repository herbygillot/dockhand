// Package config reads and writes dockhand's configuration file,
// ~/.dockhand/config.toml (decision 15; docs/design-v3.md §12).
//
// A setting's value comes from a flag, then the environment, then this
// file, then its default. The file holds only keys dockhand uses: an
// unknown key or a malformed value is refused by name, never ignored.
// Reading the file creates nothing.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/herbygillot/dockhand/internal/atomicfile"
)

// PathVariable names another configuration file, for tests and experiments.
const PathVariable = "DOCKHAND_CONFIG"

// File is the configuration file's contents.
type File struct {
	// Worktrees is the directory managed branches' worktrees live in, with
	// a leading ~ expanded.
	Worktrees string `toml:"worktrees"`
	// Maintainer is you as a Portfile's maintainers line names you, such
	// as "{@ada example.org:ada} openmaintainer".
	Maintainer string `toml:"maintainer"`
	Check      Check  `toml:"check"`
	Submit     Submit `toml:"submit"`
	Providers  struct {
		Command *CommandProvider `toml:"command"`
	} `toml:"providers"`
	Cleanup Cleanup `toml:"cleanup"`
}

// Submit holds submit's defaults.
type Submit struct {
	// RerequestReview is what submit does after pushing to a pull request
	// whose reviewers requested changes: ask (the default), always, or
	// never ask them to review again.
	RerequestReview string `toml:"rerequest_review"`
}

// Cleanup is decision 36's automatic cleanup, which serve runs at most
// once a day.
type Cleanup struct {
	// Automatic turns it off when false; it is on when unset.
	Automatic *bool `toml:"automatic"`
	// After is how long something goes unused before it is removed, such
	// as "7d" or "36h"; 7 days when unset.
	After string `toml:"after"`
}

// DefaultCleanupAfter is how long cleanup waits when after is unset.
const DefaultCleanupAfter = 7 * 24 * time.Hour

// On reports whether automatic cleanup runs.
func (c Cleanup) On() bool { return c.Automatic == nil || *c.Automatic }

// Age is after as a duration.
func (c Cleanup) Age() time.Duration {
	age, err := parseAge(c.After)
	if err != nil || c.After == "" {
		return DefaultCleanupAfter
	}
	return age
}

// parseAge reads a duration that may count days, such as "7d".
func parseAge(value string) (time.Duration, error) {
	if days, ok := strings.CutSuffix(value, "d"); ok {
		n, err := strconv.Atoi(days)
		if err != nil || n <= 0 {
			return 0, fmt.Errorf("%q is not a number of days, such as 7d", value)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	age, err := time.ParseDuration(value)
	if err != nil || age <= 0 {
		return 0, fmt.Errorf("%q is not a duration, such as 7d or 36h", value)
	}
	return age, nil
}

// Check holds check's defaults.
type Check struct {
	// On are the providers every check must pass on, such as
	// "tart:tahoe" or "command"; all of them, never one of them.
	On []string `toml:"on"`
	// Tests is declared, required, or skip.
	Tests string `toml:"tests"`
	// Baseline runs a baseline of what failed after a check fails.
	Baseline bool `toml:"baseline"`
}

// CommandProvider is a person's own build script (Design v3 §7): it is
// given a request file and writes a result file.
type CommandProvider struct {
	// Run is the command, given the request file's path as its argument.
	Run string `toml:"run"`
	// Name labels its results: "reported by <name>".
	Name string `toml:"name"`
}

// Path is the configuration file to use: $DOCKHAND_CONFIG, or
// ~/.dockhand/config.toml.
func Path() (string, error) {
	if path := os.Getenv(PathVariable); path != "" {
		return filepath.Abs(path)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".dockhand", "config.toml"), nil
}

// Load reads the file at path. A missing file is an empty configuration.
func Load(path string) (File, error) {
	var f File
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return f, nil
	}
	if err != nil {
		return f, err
	}
	return parse(path, string(data))
}

func parse(path, text string) (File, error) {
	var f File
	meta, err := toml.Decode(text, &f)
	if err != nil {
		return File{}, fmt.Errorf("%s: %w", path, err)
	}
	if unknown := meta.Undecoded(); len(unknown) > 0 {
		// A table is reported beside the keys in it; name only the keys.
		var keys []string
		for i, key := range unknown {
			name := key.String()
			if i+1 < len(unknown) && strings.HasPrefix(unknown[i+1].String(), name+".") {
				continue
			}
			keys = append(keys, name)
		}
		return File{}, fmt.Errorf("%s: unknown setting %s", path, strings.Join(keys, ", "))
	}
	if f.Worktrees, err = expandHome(f.Worktrees); err != nil {
		return File{}, fmt.Errorf("%s: worktrees: %w", path, err)
	}
	switch f.Check.Tests {
	case "", "declared", "required", "skip":
	default:
		return File{}, fmt.Errorf("%s: check.tests: %q is not declared, required, or skip", path, f.Check.Tests)
	}
	switch f.Submit.RerequestReview {
	case "", "ask", "always", "never":
	default:
		return File{}, fmt.Errorf("%s: submit.rerequest_review: %q is not ask, always, or never", path, f.Submit.RerequestReview)
	}
	if err := checkMaintainer(f.Maintainer); err != nil {
		return File{}, fmt.Errorf("%s: maintainer: %w", path, err)
	}
	if f.Cleanup.After != "" {
		if _, err := parseAge(f.Cleanup.After); err != nil {
			return File{}, fmt.Errorf("%s: cleanup.after: %w", path, err)
		}
	}
	if command := f.Providers.Command; command != nil {
		if strings.TrimSpace(command.Run) == "" {
			return File{}, fmt.Errorf("%s: providers.command.run: the command to run is required", path)
		}
		if command.Name == "" {
			command.Name = "command"
		}
	}
	return f, nil
}

// Maintainers are the identities the maintainer line names, the way the
// port index's maintainers field carries them: @ada, example.org:ada. The
// class words openmaintainer and nomaintainer name no one.
func (f File) Maintainers() []string {
	var identities []string
	for _, field := range strings.Fields(f.Maintainer) {
		field = strings.Trim(field, "{}")
		if field == "" || field == "openmaintainer" || field == "nomaintainer" {
			continue
		}
		identities = append(identities, field)
	}
	return identities
}

// checkMaintainer checks a maintainers line the way MacPorts writes one:
// entries separated by spaces, each a braced group of a GitHub handle and
// an obfuscated address, such as {@ada example.org:ada}, or a bare
// address, handle, openmaintainer, or nomaintainer.
func checkMaintainer(line string) error {
	if line == "" {
		return nil
	}
	depth, entries := 0, 0
	for _, field := range strings.Fields(line) {
		opening := strings.HasPrefix(field, "{")
		closing := strings.HasSuffix(field, "}")
		switch {
		case opening && depth > 0:
			return fmt.Errorf("%q opens a group inside another", line)
		case opening:
			depth++
		case depth == 0 && strings.ContainsAny(field, "{}"):
			return fmt.Errorf("%q has a stray brace", line)
		}
		if closing {
			if depth == 0 {
				return fmt.Errorf("%q closes a group it never opened", line)
			}
			depth--
		}
		if depth == 0 {
			entries++
		}
	}
	if depth != 0 {
		return fmt.Errorf("%q leaves a group open", line)
	}
	if entries == 0 {
		return fmt.Errorf("%q names no one", line)
	}
	return nil
}

func expandHome(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, strings.TrimPrefix(path, "~"))
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("%q is not an absolute path", path)
	}
	return filepath.Clean(path), nil
}

var worktreesLine = regexp.MustCompile(`(?m)^[ \t]*worktrees[ \t]*=.*$`)

// SetWorktrees records the worktrees directory in the file at path,
// creating it when absent and leaving every other line as it was.
func SetWorktrees(path, directory string) error {
	if !filepath.IsAbs(directory) {
		return fmt.Errorf("worktrees: %q is not an absolute path", directory)
	}
	line := "worktrees = " + strconv.Quote(directory)
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	text := string(data)
	// A top-level key must come before the first table, so an existing line
	// counts only there; otherwise the key goes at the top.
	top := text
	if i := regexp.MustCompile(`(?m)^[ \t]*\[`).FindStringIndex(text); i != nil {
		top = text[:i[0]]
	}
	if loc := worktreesLine.FindStringIndex(top); loc != nil {
		text = text[:loc[0]] + line + text[loc[1]:]
	} else {
		text = line + "\n" + text
	}
	if _, err := parse(path, text); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return atomicfile.Write(path, []byte(text), 0o600)
}
