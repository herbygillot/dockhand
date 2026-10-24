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

// Settings are the global selections. Each comes from its flag, then its
// environment variable, then its default.
type Settings struct {
	Tree     string
	Database string
	Git      string
	// File is the configuration file, as read when the engine opened.
	File config.File
	// Opened is the engine a command opened, and openedWith what it opened
	// with, for the cleanup that follows the command's work.
	Opened     *engine.Engine
	OpenedWith engine.Options
	// Streams are the command's, for a question opening the engine asks.
	Streams term.Streams
	// NamesBranch is a command naming a branch, by -b, -p, or --pr or by
	// its name, which run outside any checkout finds the registered one.
	NamesBranch bool
	// OverAllowance is --over-allowance: a look goes on past what's left
	// of the hour's GitHub allowance (allowLook).
	OverAllowance bool
	// KeepModeline is --keep-modeline: the Portfiles the command's edits
	// write keep their first line (engine.Engine.KeepModeline).
	KeepModeline bool
	// Command runs a command line in this one's invocation, its
	// environment, directory, and tree, database, and Git, for one typed at
	// status --watch's prompt.
	Command func(context.Context, []string, term.Streams) error
	// Deps are the stand-ins for what compose makes, which a test gives;
	// the real ones where they're empty.
	Deps compose.Dependencies
	// Host is the Mac's, or a test's stand-in.
	Host *host.Host
	// Env and dir are the command line's environment and working
	// directory (invocation), which compose reads.
	Env []string
	Dir string
}

// Getenv is the variable's value in the command line's environment.
func (s *Settings) Getenv(name string) string { return s.ComposeOptions().Getenv(name) }

// Signin is how the command line reaches the person's GitHub login, with
// the host's keeping of it (compose.Options.Signin).
func (s *Settings) Signin() github.Signin { return s.ComposeOptions().Signin(s.Host.AuthStore) }

// Places are the command line's home and working directory.
func (s *Settings) Places() config.Places { return s.ComposeOptions().Places() }

// OverAllowanceFlag registers --over-allowance on a command that looks up
// ports' newest releases.
func OverAllowanceFlag(cmd *cobra.Command, s *Settings) {
	cmd.Flags().BoolVar(&s.OverAllowance, "over-allowance", false, "look up the ports though the look's estimate passes what's left of the hour's GitHub allowance")
}

// KeepModelineFlag registers --keep-modeline on a command whose edits
// write a Portfile.
func KeepModelineFlag(cmd *cobra.Command, s *Settings) {
	cmd.Flags().BoolVar(&s.KeepModeline, "keep-modeline", false, "leave each Portfile's first line as it is, where dockhand otherwise gives it MacPorts' modeline")
}

func (s *Settings) Flags(root *cobra.Command) {
	root.PersistentFlags().StringVarP(&s.Tree, "tree", "t", "", "the ports checkout (default $MACPORTS_TREE, else the current directory)")
	root.PersistentFlags().StringVar(&s.Database, "db", "", "the database (default $DOCKHAND_DB, else ~/.dockhand/dockhand.db)")
	root.PersistentFlags().StringVar(&s.Git, "git", "", "the git executable (default $GIT_BIN, else git on PATH)")
}

// PullRequestsVariable names the sandbox pull requests go to instead of
// MacPorts' repository (engine.Options.PullRequests).
const PullRequestsVariable = compose.PullRequestsVariable

// ComposeOptions are the command line's selections, as compose takes them.
func (s *Settings) ComposeOptions() compose.Options {
	return compose.Options{Tree: s.Tree, Database: s.Database, Git: s.Git, NamesBranch: s.NamesBranch, Env: s.Env, Dir: s.Dir}
}

// Options resolves the engine's options, reading the configuration file
// (compose.Settings).
func (s *Settings) Options(ctx context.Context) (engine.Options, config.File, string, error) {
	return compose.Settings(ctx, s.ComposeOptions())
}

// Open opens the engine through compose, asking the person, on a
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
	if refused := new(engine.ReleaseUnderMigrationError); errors.As(err, &refused) && s.Streams.Terminal() {
		// A development or rc build opening a release's database: the
		// person decides, since the release can't open it after (the
		// exiv2 session's notes, 2026-10-24).
		migrate, askErr := s.Streams.AskYes(fmt.Sprintf("%v.\n? Migrate it anyway? Its schema %d form is kept at %s. [y/N] ", refused.ReleaseUnderMigrationError, refused.From, refused.Copy), false)
		if askErr != nil {
			return nil, askErr
		}
		if migrate {
			options.MigrateRelease = true
			assembly, err = compose.Open(ctx, options, s.Dependencies())
		}
	}
	if err != nil {
		return nil, err
	}
	s.File, s.Opened, s.OpenedWith = assembly.File, assembly.Engine, assembly.Options
	assembly.Engine.KeepModeline = s.KeepModeline
	return assembly.Engine, nil
}

// Dependencies are the stand-ins for compose: those given, with the
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

// Drain starts a background drain of the queue, outliving the command,
// with the options the command was opened with; its pid.
func (s *Settings) Drain() (int, error) { return s.Host.StartDrain(s.OpenedWith) }

// JsonAnnotation marks a command that can report its result as JSON.
const JsonAnnotation = "dockhand.json"

// SupportsJSON marks cmd as able to report its result with --json.
func SupportsJSON(cmd *cobra.Command) {
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations[JsonAnnotation] = "yes"
}

// Invocation is a command line: its arguments, and the environment and
// working directory it reads, as os.Environ and os.Getwd give them, in
// place of the process's.
type Invocation struct {
	Args, Env []string
	Dir       string
}
