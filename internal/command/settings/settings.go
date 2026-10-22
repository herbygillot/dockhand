package settings

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/command/host"
	"github.com/herbygillot/dockhand/internal/compose"
	"github.com/herbygillot/dockhand/internal/config"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/term"
)

// settings are the global selections. Each comes from its flag, then its
// environment variable, then its default.
type Settings struct {
	Tree     string
	Database string
	Git      string
	// file is the configuration file, as read when the engine opened.
	File config.File
	// opened is the engine a command opened, and openedWith what it opened
	// with, for the cleanup that follows the command's work.
	Opened     *engine.Engine
	OpenedWith engine.Options
	// streams are the command's, for a question opening the engine asks.
	Streams term.Streams
	// namesBranch is a command naming a branch, by -b, -p, or --pr or by
	// its name, which run outside any checkout finds the registered one.
	NamesBranch bool
	// overAllowance is --over-allowance: a look goes on past what's left
	// of the hour's GitHub allowance (allowLook).
	OverAllowance bool
	// command runs a command line as the process does (Run), for one typed
	// at status --watch's prompt.
	Command func(context.Context, []string, term.Streams) error
	// deps are the stand-ins for what compose makes, which a test gives;
	// the real ones where they're empty.
	Deps compose.Dependencies
	// host is the Mac's, or a test's stand-in.
	Host *host.Host
	// env and dir are the command line's environment and working
	// directory (invocation), which compose reads.
	Env []string
	Dir string
}

// getenv is the variable's value in the command line's environment.
func (s *Settings) Getenv(name string) string { return s.ComposeOptions().Getenv(name) }

// signin is how the command line reaches the person's GitHub login, with
// the host's keeping of it (compose.Options.Signin).
func (s *Settings) Signin() github.Signin { return s.ComposeOptions().Signin(s.Host.AuthStore) }

// places are the command line's home and working directory.
func (s *Settings) Places() config.Places { return s.ComposeOptions().Places() }

// overAllowanceFlag registers --over-allowance on a command that looks up
// ports' newest releases.
func OverAllowanceFlag(cmd *cobra.Command, s *Settings) {
	cmd.Flags().BoolVar(&s.OverAllowance, "over-allowance", false, "look up the ports though the look's estimate passes what's left of the hour's GitHub allowance")
}

func (s *Settings) Flags(root *cobra.Command) {
	root.PersistentFlags().StringVarP(&s.Tree, "tree", "t", "", "the ports checkout (default $MACPORTS_TREE, else the current directory)")
	root.PersistentFlags().StringVar(&s.Database, "db", "", "the database (default $DOCKHAND_DB, else ~/.dockhand/dockhand.db)")
	root.PersistentFlags().StringVar(&s.Git, "git", "", "the git executable (default $GIT_BIN, else git on PATH)")
}

// pullRequestsVariable names the sandbox pull requests go to instead of
// MacPorts' repository (engine.Options.PullRequests).
const PullRequestsVariable = compose.PullRequestsVariable

// composeOptions are the command line's selections, as compose takes them.
func (s *Settings) ComposeOptions() compose.Options {
	return compose.Options{Tree: s.Tree, Database: s.Database, Git: s.Git, NamesBranch: s.NamesBranch, Env: s.Env, Dir: s.Dir}
}

// options resolves the engine's options, reading the configuration file
// (compose.Settings).
func (s *Settings) Options(ctx context.Context) (engine.Options, config.File, string, error) {
	return compose.Settings(ctx, s.ComposeOptions())
}

// open opens the engine through compose, asking the person, on a
// terminal, to migrate a database a process on the older schema is
// working with.
func (s *Settings) Open(ctx context.Context) (*engine.Engine, error) {
	options := s.ComposeOptions()
	assembly, err := compose.Open(ctx, options, s.Dependencies())
	if refused := new(engine.LiveUnderMigrationError); errors.As(err, &refused) && s.Streams.Terminal() {
		// A person decides: their checks can be resumed by this build
		// once they stop, as dockhand wait resumes a stopped check.
		migrate, askErr := s.Streams.AskYes(fmt.Sprintf("%v.\n? Migrate anyway? What they're doing stops, and this dockhand can resume a stopped check. [y/N] ", refused), false)
		if askErr != nil {
			return nil, askErr
		}
		if migrate {
			options.MigrateUnderLive = true
			assembly, err = compose.Open(ctx, options, s.Dependencies())
		}
	}
	if err != nil {
		return nil, err
	}
	s.File, s.Opened, s.OpenedWith = assembly.File, assembly.Engine, assembly.Options
	return assembly.Engine, nil
}

// dependencies are the stand-ins for compose: those given, with the
// login's parts the command shares with setup github, and Tart as the
// host finds it.
func (s *Settings) Dependencies() compose.Dependencies {
	d := s.Deps
	if d.GitHubClient == nil {
		d.GitHubClient = s.Host.AuthAPI
	}
	if d.Store == nil {
		d.Store = s.Host.AuthStore
	}
	if d.LookTart == nil {
		d.LookTart = s.Host.LookTart
	}
	return d
}

// drain starts a background drain of the queue, outliving the command,
// with the options the command was opened with; its pid.
func (s *Settings) Drain() (int, error) { return s.Host.StartDrain(s.OpenedWith) }

// jsonAnnotation marks a command that can report its result as JSON.
const JsonAnnotation = "dockhand.json"

// supportsJSON marks cmd as able to report its result with --json.
func SupportsJSON(cmd *cobra.Command) {
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations[JsonAnnotation] = "yes"
}
