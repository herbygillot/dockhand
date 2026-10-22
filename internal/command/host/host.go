package host

import (
	"context"
	"io"
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

// Host is what a command line reads of the Mac it runs on, beyond the
// engine compose assembles: its clock and timings, its login's keeping,
// its notifications and launchd, and Tart. A run is given one, the Mac's
// own (machineHost) unless a test stands in its own.
type Host struct {
	Timings Timings
	// Now is the clock serve's daily work reads.
	Now func() time.Time
	// Identity is who serve's GitHub login acts as now, read afresh each
	// time it's asked.
	Identity func(*github.Runtime) func(context.Context) (string, error)
	// Executable is the file a long-lived serve watches for an upgrade;
	// a drain ends by itself, and watches none.
	Executable func(drain bool) string
	// Notify shows a notification.
	Notify func(title, text string) error
	// Launchctl runs launchctl for serve's agent (macos.LaunchAgent), and
	// returns what it wrote, nil being launchctl's own.
	Launchctl func(ctx context.Context, args ...string) ([]byte, error)
	// os is the operating system serve --install targets.
	OS string
	// StartCleanup starts dockhand clean --automatic after a command, and
	// startDrain dockhand serve --drain where check -d finds no serve
	// leading, returning its process's ID.
	StartCleanup func(engine.Options) error
	StartDrain   func(engine.Options) (int, error)
	// LookTart finds Tart, for setup's report and the engine's provider.
	LookTart func(string) (string, error)
	// TartImages, when set, stands in for dockhand's Tart images.
	TartImages TartImages
	// TartSupported is whether this Mac takes Tart (tart.Supported).
	TartSupported func() bool
	// The login's parts: its device flow, where it's kept, the client
	// reading it, and the browser its page opens in.
	AuthFlow    credential.DeviceFlow
	AuthStore   github.LoginStore
	AuthAPI     func(github.Signin) *github.Client
	OpenBrowser func(ctx context.Context, address string) error
}

// Timings are how often the long-running commands look again.
type Timings struct {
	// ServePoll is how often serve looks for work and a standby for the
	// leader; serveRefresh how often it reads your pull requests from
	// GitHub; serveGrace how long its executable may be missing before
	// serve takes dockhand as uninstalled; serveCleanup how often it runs
	// automatic cleanup (decision 36); serveIdentity how often it reads
	// who its login acts as.
	ServePoll, ServeRefresh, ServeGrace, ServeCleanup, ServeIdentity time.Duration
	// WatchPoll is how often status --watch looks at the journal;
	// watchRedraw how often the live view is redrawn with nothing new, so
	// its ages stay true, and watchProgress how often at most for a
	// check's progress alone, which the list of checks shows.
	WatchPoll, WatchRedraw, WatchProgress time.Duration
	// FollowPoll is how often logs --follow reads the log again.
	FollowPoll time.Duration
}

// MachineHost is the Mac's own host.
func MachineHost() *Host {
	return &Host{
		Timings: Timings{
			ServePoll:     2 * time.Second,
			ServeRefresh:  engine.PullRequestRefresh,
			ServeGrace:    time.Minute,
			ServeCleanup:  24 * time.Hour,
			ServeIdentity: time.Minute,
			WatchPoll:     time.Second,
			WatchRedraw:   30 * time.Second,
			WatchProgress: 5 * time.Second,
			FollowPoll:    time.Second,
		},
		Now: time.Now,
		// From the engine's own runtime, so a renewal its store didn't
		// take is the watcher's too.
		Identity: func(runtime *github.Runtime) func(context.Context) (string, error) {
			watcher := runtime.Identity()
			return func(ctx context.Context) (string, error) {
				identity, err := watcher.Current(ctx)
				return identity.Account, err
			}
		},
		Executable: func(drain bool) string {
			if drain {
				return ""
			}
			executable, err := os.Executable()
			if err != nil {
				return ""
			}
			return executable
		},
		Notify: func(title, text string) error {
			if runtime.GOOS != "darwin" {
				return nil
			}
			return macos.Notify(context.Background(), title, text)
		},
		OS:            runtime.GOOS,
		StartCleanup:  startCleanup,
		StartDrain:    startDrain,
		LookTart:      exec.LookPath,
		TartSupported: tart.Supported,
		AuthFlow:      &github.DeviceFlow{},
		AuthStore:     keychain.Store{},
		AuthAPI:       github.SystemClient,
		OpenBrowser:   macos.Open,
	}
}

// TartImages is what setup and setup tart ask of dockhand's Tart images.
type TartImages interface {
	Status(ctx context.Context) (tart.Status, error)
	Setup(ctx context.Context, options tart.SetupOptions, progress io.Writer) (tart.SetupResult, error)
	Xcodes() (string, bool)
	DownloadXcode(ctx context.Context, missing *tart.MissingXcode, in io.Reader, out, errs io.Writer) (string, error)
}
