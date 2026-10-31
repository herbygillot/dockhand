package eval

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// Installation is what a port is evaluated against (installation.tcl).
type Installation string

const (
	// FreshInstallation is MacPorts Base alone in its prefix and an empty
	// registry (docs/oracle.md, phase 4); every evaluation dockhand makes
	// is against it.
	FreshInstallation Installation = "fresh"
	// HostInstallation is the Mac's own, in shadow: the dispatcher still
	// refuses writes, the network, and programs not known to only report,
	// and passes every read to the host, counted. A survey of a guest
	// evaluates against it.
	HostInstallation Installation = "host"
	// InstalledPorts is the fresh installation with a registry that holds
	// Evaluator.Installed's ports, each installed and active, which a
	// survey compares against the fresh one.
	InstalledPorts Installation = "installed"
)

func (i Installation) resolve() (Installation, error) {
	switch i {
	case "":
		return FreshInstallation, nil
	case FreshInstallation, HostInstallation, InstalledPorts:
		return i, nil
	}
	return "", fmt.Errorf("eval: unknown installation %q", string(i))
}

// InstalledPort is a port the installed registry holds: its version,
// revision, epoch, and its variants as Base writes them, "+a+b".
type InstalledPort struct {
	Version  string
	Revision int
	Epoch    int
	Variants string
}

// encodeInstalled is the table as installation.tcl's
// set_installed_table takes it, by name.
func encodeInstalled(table map[string]InstalledPort) string {
	names := make([]string, 0, len(table))
	for name := range table {
		names = append(names, name)
	}
	sort.Strings(names)
	words := make([]string, 0, 2*len(names))
	for _, name := range names {
		port := table[name]
		words = append(words, syntax.Quote(name), syntax.Quote(strings.Join([]string{syntax.Quote(port.Version), strconv.Itoa(port.Revision), strconv.Itoa(port.Epoch), syntax.Quote(port.Variants)}, " ")))
	}
	return strings.Join(words, " ")
}

// Bootstrap is what mportinit read in a recording evaluator's session:
// each call, with its path or command line and answer, and each
// environment variable; the settings it ended with and the file each came
// from; and the registry and private aliases Base gives a port's worker.
type Bootstrap struct {
	Reads    []BootstrapRead
	Settings []BootstrapSetting
	Aliases  []WorkerAlias
}

// BootstrapRead is one read: a command ("file exists", open, glob, exec,
// source, env), its path, command line, or variable, and what it answered,
// cut at 200 characters, where the answer says something.
type BootstrapRead struct{ Command, Path, Value string }

// BootstrapSetting is a setting of Base's and the configuration file that
// set it, or default.
type BootstrapSetting struct{ Name, Value, Origin string }

// WorkerAlias is a command Base aliases into a port's worker and the
// parent's command it runs.
type WorkerAlias struct{ Name, Target string }

func decodeBootstrap(reply string) (Bootstrap, error) {
	var out Bootstrap
	parts, errs := syntax.ListValues(reply)
	if len(errs) > 0 || len(parts) != 3 {
		return out, fmt.Errorf("macports: invalid bootstrap record")
	}
	rows := func(value string, width int, each func([]string)) error {
		list, errs := syntax.ListValues(value)
		if len(errs) > 0 {
			return fmt.Errorf("macports: invalid bootstrap record")
		}
		for _, row := range list {
			fields, errs := syntax.ListValues(row)
			if len(errs) > 0 || len(fields) != width {
				return fmt.Errorf("macports: invalid bootstrap record entry")
			}
			each(fields)
		}
		return nil
	}
	if err := rows(parts[0], 3, func(f []string) { out.Reads = append(out.Reads, BootstrapRead{Command: f[0], Path: f[1], Value: f[2]}) }); err != nil {
		return out, err
	}
	if err := rows(parts[1], 3, func(f []string) {
		out.Settings = append(out.Settings, BootstrapSetting{Name: f[0], Value: f[1], Origin: f[2]})
	}); err != nil {
		return out, err
	}
	err := rows(parts[2], 2, func(f []string) { out.Aliases = append(out.Aliases, WorkerAlias{Name: f[0], Target: f[1]}) })
	return out, err
}
