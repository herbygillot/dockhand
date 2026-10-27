package engine

import (
	"cmp"
	"context"
	"path/filepath"

	"github.com/herbygillot/dockhand/internal/macports/depblock"
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
	// Vendoring are go2port and cargo2port, which create and an update's
	// Go and Rust vendoring run, each found or not, so a missing one is
	// said before serve's next look fails on it (the docs dig's I38).
	Vendoring []SetupTool `json:"vendoring"`
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
	report.Vendoring = vendoringTools(depblock.Tools{})
	return e, report, nil
}

// SetupTool is a helper dockhand runs, where it was found, or empty, and
// what it's for.
type SetupTool struct {
	Name string `json:"name"`
	Path string `json:"path,omitempty"`
	For  string `json:"for"`
}

// vendoringTools are go2port and cargo2port as the edits that run them
// find them (depblock.Tools.Resolve).
func vendoringTools(tools depblock.Tools) []SetupTool {
	var found []SetupTool
	for _, tool := range []struct{ kind, name, used string }{{depblock.Go, "go2port", "Go ports' go.vendors"}, {depblock.Cargo, "cargo2port", "Rust ports' cargo.crates"}} {
		path, _ := tools.Resolve(tool.kind)
		found = append(found, SetupTool{Name: tool.name, Path: path, For: tool.used})
	}
	return found
}
