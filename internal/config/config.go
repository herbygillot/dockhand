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
	return f, nil
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
