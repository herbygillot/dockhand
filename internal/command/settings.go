package command

import (
	"context"
	"errors"
	"fmt"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/buildenv/ghactions"
	"github.com/herbygillot/dockhand/internal/compose"
	"github.com/herbygillot/dockhand/internal/config"
	"github.com/herbygillot/dockhand/internal/engine"
)

// settings are the global selections. Each comes from its flag, then its
// environment variable, then its default.
type settings struct {
	tree     string
	database string
	git      string
	// file is the configuration file, as read when the engine opened.
	file config.File
	// opened is the engine a command opened, and openedWith what it opened
	// with, for the cleanup that follows the command's work.
	opened     *engine.Engine
	openedWith engine.Options
	// streams are the command's, for a question opening the engine asks.
	streams Streams
	// namesBranch is a command naming a branch, by -b, -p, or --pr or by
	// its name, which run outside any checkout finds the registered one.
	namesBranch bool
	// overAllowance is --over-allowance: a look goes on past what's left
	// of the hour's GitHub allowance (allowLook).
	overAllowance bool
}

// overAllowanceFlag registers --over-allowance on a command that looks up
// ports' newest releases.
func overAllowanceFlag(cmd *cobra.Command, s *settings) {
	cmd.Flags().BoolVar(&s.overAllowance, "over-allowance", false, "look up the ports though the look's estimate passes what's left of the hour's GitHub allowance")
}

func (s *settings) flags(root *cobra.Command) {
	root.PersistentFlags().StringVarP(&s.tree, "tree", "t", "", "the ports checkout (default $MACPORTS_TREE, else the current directory)")
	root.PersistentFlags().StringVar(&s.database, "db", "", "the database (default $DOCKHAND_DB, else ~/.dockhand/dockhand.db)")
	root.PersistentFlags().StringVar(&s.git, "git", "", "the git executable (default $GIT_BIN, else git on PATH)")
}

// pullRequestsVariable names the sandbox pull requests go to instead of
// MacPorts' repository (engine.Options.PullRequests).
const pullRequestsVariable = compose.PullRequestsVariable

// composeOptions are the command line's selections, as compose takes them.
func (s *settings) composeOptions() compose.Options {
	return compose.Options{Tree: s.tree, Database: s.database, Git: s.git, NamesBranch: s.namesBranch}
}

// options resolves the engine's options, reading the configuration file
// (compose.Settings).
func (s *settings) options(ctx context.Context) (engine.Options, config.File, string, error) {
	return compose.Settings(ctx, s.composeOptions())
}

// open opens the engine through compose, asking the person, on a
// terminal, to migrate a database a process on the older schema is
// working with.
func (s *settings) open(ctx context.Context) (*engine.Engine, error) {
	options := s.composeOptions()
	assembly, err := compose.Open(ctx, options, s.dependencies())
	if refused := new(engine.LiveUnderMigrationError); errors.As(err, &refused) && s.streams.terminal() {
		// A person decides: their checks can be resumed by this build
		// once they stop, as dockhand wait resumes a stopped check.
		migrate, askErr := s.streams.askYes(fmt.Sprintf("%v.\n? Migrate anyway? What they're doing stops, and this dockhand can resume a stopped check. [y/N] ", refused), false)
		if askErr != nil {
			return nil, askErr
		}
		if migrate {
			options.MigrateUnderLive = true
			assembly, err = compose.Open(ctx, options, s.dependencies())
		}
	}
	if err != nil {
		return nil, err
	}
	s.file, s.opened, s.openedWith = assembly.File, assembly.Engine, assembly.Options
	return assembly.Engine, nil
}

// dependencies are the stand-ins the tests set, for compose.
func (s *settings) dependencies() compose.Dependencies {
	return compose.Dependencies{Preparer: testPreparer, Forge: testForge, ArchiveFetcher: testArchiveFetcher, PortReader: testPortReader,
		DependentReader: testDependentReader, OutdatedReader: testOutdatedReader, ProjectReader: testProjectReader, HTTPS: testHTTPS,
		GitHubCLI: gitHubCLI, Actions: testActions, Leftovers: testLeftovers, LookTart: lookTart, GitHubClient: authAPI, Store: authStore}
}

// testHTTPS, when set, stands in for asking URLs over HTTPS.
var testHTTPS engine.HTTPSProbe

// testArchiveFetcher, when set, stands in for fetching archives.
var testArchiveFetcher func(*engine.Engine) engine.ArchiveFetcher

// testActions, when set, stands in for GitHub Actions.
var testActions ghactions.API

// testLeftovers, when set, is a provider with environments left behind,
// registered under its name in place of whichever the configuration made.
var testLeftovers buildenv.LeftoverProvider

// lookTart finds Tart; the tests find none, so they don't depend on the
// machine they run on.
var lookTart = exec.LookPath

// testPortReader, when set, stands in for MacPorts' evaluator in plans.
var testPortReader engine.PortReader

// testDependentReader, when set, stands in for the port index in impact.
var testDependentReader engine.DependentReader

// testOutdatedReader, when set, stands in for upstream discovery.
var testOutdatedReader engine.OutdatedReader
