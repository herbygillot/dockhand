// Package distributable judges whether MacPorts' builders may keep a binary
// archive of a port, by MacPorts' own checker: macports-infrastructure's
// jobs/distributable_lib.tcl, kept verbatim and pinned (Pin), with
// infoForPort answered from what dockhand reads, so its answer is the
// buildbot's (decisions.md, "Distributability is MacPorts' own judge").
package distributable

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// library is MacPorts' checker, verbatim; wrapper answers its infoForPort.
var (
	//go:embed distributable_lib.tcl
	library string
	//go:embed distributable.tcl
	wrapper string
)

// Pin is macports-infrastructure's commit whose jobs/distributable_lib.tcl
// is the copy here, which a weekly job compares with master's.
const Pin = "1e0eb89aa6f6992dfd57b1d3e1f5a13042cfe770"

// Script is the checker with dockhand's infoForPort, to load once into an
// interpreter that serves tclrpc calls.
func Script() string { return library + "\n" + wrapper }

// Info is what the checker reads of a port (infoForPort): the ports its
// depends_build and depends_lib name, its license as written, whether it
// installs libraries a dependent takes in (yes unless it says no), and the
// dependencies its license_noconflict exempts.
type Info struct {
	Dependencies []string
	License      string
	InstallsLibs bool
	NoConflict   []string
}

// Caller is an interpreter's call, which Script was loaded into.
type Caller interface {
	Call(ctx context.Context, op string, args ...string) (string, error)
}

// Verdict is the checker's answer on a port: whether it's distributable,
// and its sentence; Known is false where it couldn't judge it.
type Verdict struct {
	Known, OK bool
	Reason    string
}

// Judge asks the checker of port, given each port's Info by name, the
// port's own and its dependencies', transitively, as far as they're known.
func Judge(ctx context.Context, interpreter Caller, port string, infos map[string]Info) (Verdict, error) {
	var ports []string
	for name, info := range infos {
		installs := "no"
		if info.InstallsLibs {
			installs = "yes"
		}
		var fields []string
		for _, dependency := range info.Dependencies {
			fields = append(fields, syntax.Quote(dependency))
		}
		var noconflict []string
		for _, exempt := range info.NoConflict {
			noconflict = append(noconflict, syntax.Quote(exempt))
		}
		entry := strings.Join([]string{syntax.Quote(strings.Join(fields, " ")), syntax.Quote(info.License), installs, syntax.Quote(strings.Join(noconflict, " "))}, " ")
		ports = append(ports, syntax.Quote(name), syntax.Quote(entry))
	}
	reply, err := interpreter.Call(ctx, "distributable", port, strings.Join(ports, " "))
	if err != nil {
		return Verdict{}, err
	}
	fields, errs := syntax.ListValues(reply)
	if len(errs) > 0 || len(fields) != 2 {
		return Verdict{}, fmt.Errorf("distributable: invalid verdict %q", reply)
	}
	if fields[0] == "unknown" {
		return Verdict{Reason: fields[1]}, nil
	}
	return Verdict{Known: true, OK: fields[0] == "1", Reason: fields[1]}, nil
}
