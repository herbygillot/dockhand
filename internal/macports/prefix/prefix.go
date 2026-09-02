// Package prefix knows a MacPorts installation as an instance: the
// installed system rooted at a directory, as distinct from the ports
// tree and from MacPorts base the software. Several can coexist on one
// machine — the conventional /opt/local next to a test prefix — and an
// evaluator, a build, or a probe always runs against exactly one of
// them. Machine-side probes belong here: what is installed and where,
// and eventually what the installation says about itself (configured
// sources, version).
package prefix

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/tool"
)

// ErrNotInstalled reports that no MacPorts installation could be found.
var ErrNotInstalled = errors.New("prefix: no MacPorts installation found")

// Prefix is a MacPorts installation rooted at a directory. New and Find
// return validated installations; a directly-cast Prefix is just a path
// with the layout methods, which is all path construction needs.
type Prefix string

// PortTclsh returns the path of the installation's Tcl shell.
func (p Prefix) PortTclsh() string {
	return filepath.Join(string(p), "bin", string(tool.PortTclsh))
}

// Port returns the path of the installation's port client.
func (p Prefix) Port() string {
	return filepath.Join(string(p), "bin", macports.CommandName)
}

// Portindex returns the path of the installation's tree indexer.
func (p Prefix) Portindex() string {
	return filepath.Join(string(p), "bin", macports.IndexCommandName)
}

// SourcesConf returns the path of the file listing the ports trees this
// installation reads. Putting a tree ahead of the others there is how a
// caller makes an edited port win over the installation's own copy.
func (p Prefix) SourcesConf() string {
	return filepath.Join(string(p), macports.SourcesConfPath)
}

// New returns the installation rooted at dir, validating that one
// actually lives there — any directory works, not just the conventional
// prefix. A stated prefix is never fallen back from.
func New(dir string) (Prefix, error) {
	p := Prefix(dir)
	if _, err := os.Stat(p.PortTclsh()); err != nil {
		return "", fmt.Errorf("%w (no %s under %s)", ErrNotInstalled, tool.PortTclsh, dir)
	}
	return p, nil
}

// Find discovers an installation: port-tclsh on PATH, whose location
// implies the prefix, else the conventional default prefix. The
// lookup runs through the run's finder, so what doctor reported and
// what the run resolves are one answer.
func Find(tools *tool.Finder) (Prefix, error) {
	return find(tools, macports.DefaultPrefix)
}

func find(tools *tool.Finder, defaultPrefix string) (Prefix, error) {
	if path, err := tools.Find(tool.PortTclsh); err == nil {
		return Prefix(filepath.Dir(filepath.Dir(path))), nil
	}
	if p, err := New(defaultPrefix); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("%w (no %s on PATH or under %s)", ErrNotInstalled, tool.PortTclsh, defaultPrefix)
}

// runVersion is indirected for hermetic tests. A failure is handed
// back as os/exec reported it — "exit status 1", a start failure —
// rather than as the port client's stderr, because that is the text
// Version has always wrapped and the callers log.
var runVersion = func(ctx context.Context, path string, args ...string) (string, error) {
	out, _, err := tool.Output(ctx, path, tool.Opts{Args: args})
	var f *tool.Failure
	if errors.As(err, &f) {
		err = f.Err
	}
	return string(out), err
}

// Version reports the MacPorts version this installation runs, as the
// port client states it. Callers that cannot determine a version are
// expected to proceed on a best-effort default rather than fail: the
// version selects among behaviors, it does not gate them.
func (p Prefix) Version(ctx context.Context) (string, error) {
	out, err := runVersion(ctx, p.Port(), "version")
	if err != nil {
		return "", fmt.Errorf("prefix: %s version: %w", p.Port(), err)
	}
	// "Version: 2.12.6"
	_, v, ok := strings.Cut(strings.TrimSpace(out), ":")
	if !ok {
		return "", fmt.Errorf("prefix: %s version: unexpected output %q", p.Port(), strings.TrimSpace(out))
	}
	return strings.TrimSpace(v), nil
}
