package installation

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
)

type Facts struct {
	Prefix      string
	Version     string
	Platform    model.Platform
	ActivePorts []string
	Problems    []string
}

// Inspect reads installation facts. Command exits become diagnostics; transport
// failures and cancellation remain errors. It does not compile or install anything.
func Inspect(ctx context.Context, command macos.Command, prefix string) (Facts, error) {
	result := Facts{Prefix: prefix}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if !filepath.IsAbs(prefix) {
		return result, fmt.Errorf("macports: absolute installation prefix required")
	}
	run := func(label string, args ...string) ([]byte, error) {
		out, err := command(ctx, nil, args...)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err == nil {
			return out, nil
		}
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			return nil, err
		}
		result.Problems = append(result.Problems, label+": "+err.Error())
		return nil, nil
	}
	port := filepath.Join(prefix, "bin", "port")
	version, err := run("MacPorts is unavailable", port, "version")
	if err != nil {
		return result, err
	}
	if parsed, ok := ParseVersion(version); ok {
		result.Version = parsed
	} else if version != nil {
		result.Problems = append(result.Problems, "MacPorts returned an unrecognized version: "+strings.TrimSpace(string(version)))
	}
	active, err := run("checking active ports", port, "-q", "installed", "active")
	if err != nil {
		return result, err
	}
	result.ActivePorts = activePorts(active)
	// Pass the program through stdin rather than interpolating the prefix into
	// Tcl. Its answer is the line it marks: mportinit warns, on stderr, once
	// the port definitions are more than two weeks old, and a guest's
	// commands read stderr with stdout (rc1's full stage).
	tcl := "package require macports\nmportinit\nputs \"" + platformMark + " $::macports::os_platform $::macports::os_major $::macports::build_arch\"\n"
	out, err := command(ctx, strings.NewReader(tcl), filepath.Join(prefix, "bin", macports.TclShell))
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			return result, err
		}
		result.Problems = append(result.Problems, "MacPorts platform evaluation failed: "+err.Error())
	} else {
		fields := markedFields(out, platformMark)
		if len(fields) == 3 {
			result.Platform = model.Platform{OS: fields[0], Version: fields[1], Architecture: fields[2]}
		} else {
			result.Problems = append(result.Problems, "MacPorts returned an unrecognized platform: "+strings.TrimSpace(string(out)))
		}
	}
	return result, nil
}

func CheckTclPackages(ctx context.Context, command macos.Command, prefix string) error {
	_, err := command(ctx, strings.NewReader("package require json\npackage require json::write\n"), filepath.Join(prefix, "bin", macports.TclShell))
	return err
}

// ParseVersion reads the version from the output of port version, from its
// "Version:" line wherever it is: port prints a warning before it once the
// port definitions are more than two weeks old, which refused every Tart
// image set up from a sound one (rc1's full stage).
func ParseVersion(out []byte) (string, bool) {
	for line := range strings.Lines(string(out)) {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "Version:" {
			return fields[1], true
		}
	}
	return "", false
}

// platformMark marks the platform probe's answer among what MacPorts
// says as it starts.
const platformMark = "dockhand-platform:"

// markedFields are the fields of out's line that starts with mark, less
// the mark; nil where none does.
func markedFields(out []byte, mark string) []string {
	for line := range strings.Lines(string(out)) {
		if fields := strings.Fields(line); len(fields) > 0 && fields[0] == mark {
			return fields[1:]
		}
	}
	return nil
}

// activePorts are the active ports port -q installed active lists, a
// line each ending "(active)", anything else MacPorts says beside them,
// as its stale-definitions warning, left out.
func activePorts(out []byte) []string {
	var ports []string
	for line := range strings.Lines(string(out)) {
		if line = strings.TrimSpace(line); strings.HasSuffix(line, "(active)") {
			ports = append(ports, line)
		}
	}
	return ports
}
