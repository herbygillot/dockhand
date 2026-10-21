package command

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/herbygillot/dockhand/internal/buildenv/tart"
	"github.com/herbygillot/dockhand/internal/credential"
	"github.com/herbygillot/dockhand/internal/credential/keychain"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/macos"
)

// host is what a command line reads of the Mac it runs on, beyond the
// engine compose assembles: its clock and timings, its login's keeping,
// its notifications and launchd, and Tart. A run is given one, the Mac's
// own (machineHost) unless a test stands in its own.
type host struct {
	timings timings
	// now is the clock serve's daily work reads.
	now func() time.Time
	// identity is who serve's GitHub login acts as now, read afresh each
	// time it's asked.
	identity func(*github.Runtime) func(context.Context) (string, error)
	// executable is the file a long-lived serve watches for an upgrade;
	// a drain ends by itself, and watches none.
	executable func(drain bool) string
	// notify shows a notification.
	notify func(title, text string) error
	// launchctl runs launchctl for serve's agent (macos.LaunchAgent), and
	// returns what it wrote, nil being launchctl's own.
	launchctl func(ctx context.Context, args ...string) ([]byte, error)
	// os is the operating system serve --install targets.
	os string
	// startCleanup starts dockhand clean --automatic after a command.
	startCleanup func(engine.Options) error
	// lookTart finds Tart, for setup's report and the engine's provider.
	lookTart func(string) (string, error)
	// tartImages, when set, stands in for dockhand's Tart images.
	tartImages tartImages
	// tartSupported is whether this Mac takes Tart (tart.Supported).
	tartSupported func() bool
	// The login's parts: its device flow, where it's kept, the client
	// reading it, and the browser its page opens in.
	authFlow    credential.DeviceFlow
	authStore   github.LoginStore
	authAPI     func(credential.Store) *github.Client
	openBrowser func(ctx context.Context, address string) error
}

// timings are how often the long-running commands look again.
type timings struct {
	// servePoll is how often serve looks for work and a standby for the
	// leader; serveRefresh how often it reads your pull requests from
	// GitHub; serveGrace how long its executable may be missing before
	// serve takes dockhand as uninstalled; serveCleanup how often it runs
	// automatic cleanup (decision 36); serveIdentity how often it reads
	// who its login acts as.
	servePoll, serveRefresh, serveGrace, serveCleanup, serveIdentity time.Duration
	// watchPoll is how often status --watch looks at the journal;
	// watchRedraw how often the live view is redrawn with nothing new, so
	// its ages stay true, and watchProgress how often at most for a
	// check's progress alone, which the list of checks shows.
	watchPoll, watchRedraw, watchProgress time.Duration
	// followPoll is how often logs --follow reads the log again.
	followPoll time.Duration
}

// machineHost is the Mac's own host.
func machineHost() *host {
	return &host{
		timings: timings{
			servePoll:     2 * time.Second,
			serveRefresh:  engine.PullRequestRefresh,
			serveGrace:    time.Minute,
			serveCleanup:  24 * time.Hour,
			serveIdentity: time.Minute,
			watchPoll:     time.Second,
			watchRedraw:   30 * time.Second,
			watchProgress: 5 * time.Second,
			followPoll:    time.Second,
		},
		now: time.Now,
		// From the engine's own runtime, so a renewal its store didn't
		// take is the watcher's too.
		identity: func(runtime *github.Runtime) func(context.Context) (string, error) {
			watcher := runtime.Identity()
			return func(ctx context.Context) (string, error) {
				identity, err := watcher.Current(ctx)
				return identity.Account, err
			}
		},
		executable: func(drain bool) string {
			if drain {
				return ""
			}
			executable, err := os.Executable()
			if err != nil {
				return ""
			}
			return executable
		},
		notify: func(title, text string) error {
			if runtime.GOOS != "darwin" {
				return nil
			}
			return macos.Notify(context.Background(), title, text)
		},
		os:            runtime.GOOS,
		startCleanup:  startCleanup,
		lookTart:      exec.LookPath,
		tartSupported: tart.Supported,
		authFlow:      &github.DeviceFlow{},
		authStore:     keychain.Store{},
		authAPI:       github.SystemClient,
		openBrowser:   macos.Open,
	}
}
