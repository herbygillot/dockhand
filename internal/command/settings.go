package command

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/config"
	"github.com/herbygillot/dockhand/internal/engine"
)

// settings are the global selections. Each comes from its flag, then its
// environment variable, then the configuration file, then its default.
type settings struct {
	tree     string
	database string
	git      string
}

func (s *settings) flags(root *cobra.Command) {
	root.PersistentFlags().StringVarP(&s.tree, "tree", "t", "", "the ports checkout (default $MACPORTS_TREE, else the current directory)")
	root.PersistentFlags().StringVar(&s.database, "db", "", "the database (default $DOCKHAND_DB, else ~/.dockhand/dockhand.db)")
	root.PersistentFlags().StringVar(&s.git, "git", "", "the git executable (default $GIT_BIN, else git on PATH)")
}

func firstOf(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// options resolves the engine's options, reading the configuration file.
func (s *settings) options() (engine.Options, config.File, string, error) {
	configPath, err := config.Path()
	if err != nil {
		return engine.Options{}, config.File{}, "", err
	}
	file, err := config.Load(configPath)
	if err != nil {
		return engine.Options{}, config.File{}, "", err
	}
	database := firstOf(s.database, os.Getenv("DOCKHAND_DB"))
	if database == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return engine.Options{}, config.File{}, "", err
		}
		database = filepath.Join(home, ".dockhand", "dockhand.db")
	}
	return engine.Options{
		Tree:      firstOf(s.tree, os.Getenv("MACPORTS_TREE"), "."),
		Git:       firstOf(s.git, os.Getenv("GIT_BIN")),
		Database:  database,
		Worktrees: file.Worktrees,
		// DOCKHAND_UPSTREAM fetches master from a mirror or, in tests, a
		// local repository.
		Upstream: os.Getenv("DOCKHAND_UPSTREAM"),
		Tclsh:    portTclsh(),
	}, file, configPath, nil
}

func (s *settings) open(ctx context.Context) (*engine.Engine, error) {
	options, _, _, err := s.options()
	if err != nil {
		return nil, err
	}
	e, err := engine.Open(ctx, options)
	if err == nil && testPreparer != nil {
		e.Preparer = testPreparer(e)
	}
	return e, err
}

// tilde abbreviates the home directory in a path shown to a person.
func tilde(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(path, home+string(filepath.Separator)); ok {
		return "~/" + rest
	}
	return path
}

func homeDir() (string, error) { return os.UserHomeDir() }
