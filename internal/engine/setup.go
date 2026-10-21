package engine

import (
	"cmp"
	"context"
	"path/filepath"
)

// SetupReport is what setup finds of the checkout dockhand works on: the
// main checkout, MacPorts' repository and where master comes from, the
// Git it runs, where branch worktrees go, the MacPorts prefix whose
// port-tclsh it evaluates with (empty where there's none), and its
// records.
type SetupReport struct {
	Checkout  string        `json:"checkout"`
	Upstream  SetupUpstream `json:"upstream"`
	Git       SetupGit      `json:"git"`
	Worktrees string        `json:"worktrees"`
	MacPorts  string        `json:"macports"`
	Records   string        `json:"records"`
	// Unchosen is where the person hasn't said where worktrees go, by name
	// or in the configuration, so Worktrees is the default, which a
	// question may change (Choose).
	Unchosen bool `json:"-"`
	// Remember is the worktrees directory chosen that the configuration
	// doesn't keep yet, or empty.
	Remember string `json:"-"`
}

// Choose puts worktrees in directory, the person's answer where setup
// asked (Unchosen), for the configuration to keep.
func (r *SetupReport) Choose(directory string) {
	r.Worktrees, r.Remember, r.Unchosen = directory, directory, false
}

// SetupUpstream is MacPorts' repository, the checkout's remote for it
// where it has one, and where master is fetched from.
type SetupUpstream struct {
	Repository  string `json:"repository"`
	Remote      string `json:"remote,omitempty"`
	FetchedFrom string `json:"fetched_from"`
}

// SetupGit is the Git dockhand runs.
type SetupGit struct {
	Version string `json:"version"`
	Path    string `json:"path"`
}

// Setup opens the engine for setup and reports on it, refusing a Git too
// old before anything is recorded (GitVersion). Worktrees go in named, an
// absolute directory, where it's given, else the configured directory
// (Options.Worktrees), else the default (DefaultWorktrees). The report
// holds what was found where it fails partway.
func Setup(ctx context.Context, options Options, named string) (*Engine, SetupReport, error) {
	report := SetupReport{Records: options.Database}
	version, err := GitVersion(ctx, options)
	if err != nil {
		return nil, report, err
	}
	report.Git = SetupGit{Version: version.String(), Path: version.Path}
	e, err := Open(ctx, options)
	if err != nil {
		return nil, report, err
	}
	report.Checkout = e.Clone()
	upstream, err := e.UpstreamRemote(ctx)
	if err != nil {
		e.Close()
		return nil, report, err
	}
	report.Upstream = SetupUpstream{Repository: UpstreamRepository, FetchedFrom: e.Upstream()}
	if upstream != nil {
		report.Upstream.Remote = upstream.Name
	}
	chosen := cmp.Or(named, options.Worktrees)
	if chosen != options.Worktrees {
		report.Remember = chosen
	}
	report.Worktrees, report.Unchosen = cmp.Or(chosen, e.DefaultWorktrees()), chosen == ""
	if tclsh := e.Tclsh(); tclsh != "" {
		report.MacPorts = filepath.Dir(filepath.Dir(tclsh))
	}
	return e, report, nil
}
