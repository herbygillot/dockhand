package installation

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/record"
)

type Facts struct {
	Prefix      string
	Version     string
	Platform    record.Platform
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
	fields := strings.Fields(string(version))
	if len(fields) >= 2 && fields[0] == "Version:" {
		result.Version = fields[1]
	} else if version != nil {
		result.Problems = append(result.Problems, "MacPorts returned an unrecognized version: "+strings.TrimSpace(string(version)))
	}
	active, err := run("checking active ports", port, "-q", "installed", "active")
	if err != nil {
		return result, err
	}
	if value := strings.TrimSpace(string(active)); value != "" {
		result.ActivePorts = strings.Split(value, "\n")
	}
	// Pass the program through stdin rather than interpolating the prefix into Tcl.
	tcl := "package require macports\nmportinit\nputs \"$::macports::os_platform $::macports::os_major $::macports::build_arch\"\n"
	out, err := command(ctx, strings.NewReader(tcl), filepath.Join(prefix, "bin", "port-tclsh"))
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
		fields = strings.Fields(string(out))
		if len(fields) == 3 {
			result.Platform = record.Platform{OS: fields[0], Version: fields[1], Architecture: fields[2]}
		} else {
			result.Problems = append(result.Problems, "MacPorts returned an unrecognized platform: "+strings.TrimSpace(string(out)))
		}
	}
	return result, nil
}

func CheckTclPackages(ctx context.Context, command macos.Command, prefix string) error {
	_, err := command(ctx, strings.NewReader("package require json\npackage require json::write\n"), filepath.Join(prefix, "bin", "port-tclsh"))
	return err
}
