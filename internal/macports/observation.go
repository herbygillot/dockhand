package macports

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/model"
)

// ObservationRequest explicitly selects modeled metadata. It never changes the
// native platform used by Reader.Evaluate or establishes build evidence.
type ObservationRequest struct {
	Platform model.Platform
	// DeveloperTools, when stated, model the context with those tools from
	// the facts table, the Mac's own platform too: the Command Line Tools
	// alone, or Xcode.
	DeveloperTools model.DeveloperTools
	Declarations   bool
	// SelectedOnly omits sibling metadata; it is not suitable for final fidelity.
	SelectedOnly bool
	// Operands selects scalar or option values needed to resolve platform boundaries.
	Operands []string
}

// Declaration records an executed option command and its Tcl source frames.
// Consumers must resolve these frames to unique syntax before editing.
type Declaration struct {
	Command string
	Values  []string
	Frames  []SourceFrame
}
type SourceFrame struct {
	File    string
	Line    int
	Command string
}

// Distfile is a native MacPorts fetch plan; it does not mean an archive was fetched.
type Distfile struct {
	Name string
	URLs []string
}

type OperandObservation struct {
	Name   string
	Value  string
	Frames []SourceFrame
}

// LedgerEntry counts the calls a worker's dispatcher passed for one
// command, and file subcommand, by where the answer came from: pure path
// arithmetic, the captured tree, Base's own library, a process, a relative
// path, a directory enumeration, the host (docs/oracle.md, phase 1), or
// the fresh installation (phase 4). Subject is what a question of the
// installation was about: the port a registry question names, or the
// program a lookup or exec looked for.
type LedgerEntry struct {
	Command, Subcommand, Source string
	Subject                     string `json:",omitempty"`
	Count                       int
}

// PathRead counts the calls that read one path, by command, file
// subcommand, what the path is (the captured tree, Base's library, Tcl's
// library, the host, or relative where it couldn't be normalized), and who
// answered: the host, the fresh or installed installation, the facts
// table, or the host in a modelled context (host-in-model). An evaluator
// records them only when asked to (eval.Evaluator.Recording).
type PathRead struct {
	Command, Subcommand, Kind, Source, Path string
	Count                                   int
}

// CommandRun counts the runs of one command line, cut at 200 characters,
// by who answered: the host ran it, or the fresh installation or the facts
// table answered for it.
type CommandRun struct {
	Line, Source string
	Count        int
}

// Hook is a phase body a Portfile or PortGroup declared, as Base made it
// a procedure, and the file and line it was declared at.
type Hook struct {
	Procedure, Origin, Body string
}

type PortObservation struct {
	// HostAccess records external evaluation inputs even in the native context.
	HostAccess bool
	// Ledger is every call the dispatcher passed while the port was
	// evaluated, counted; in shadow mode it judges nothing.
	Ledger            []LedgerEntry
	Operands          []OperandObservation
	ModeledHostAccess bool
	Declarations      []Declaration
	Distfiles         []Distfile
	Problems          []string
	// Reads, Execs, and Hooks are what the evaluation read, ran, and
	// declared, where the evaluator records them; empty otherwise.
	Reads []PathRead
	Execs []CommandRun
	Hooks []Hook
}

// FetchPlan is the port's fetch plan as MacPorts made it: each archive,
// with the locations MacPorts would fetch it from, its mirror groups
// expanded. Without one, the error says why, with the observation's
// problems.
func (o PortObservation) FetchPlan() ([]Distfile, error) {
	switch {
	case len(o.Distfiles) > 0:
		return o.Distfiles, nil
	case len(o.Problems) > 0:
		return nil, fmt.Errorf("MacPorts' fetch plan names no archives: %s", strings.Join(o.Problems, "; "))
	}
	return nil, errors.New("MacPorts' fetch plan names no archives")
}

type Observation struct {
	Snapshot Snapshot
	Modeled  bool
	Ports    map[string]PortObservation
}

// Observer adds request-scoped source and fetch observations to native metadata.
type Observer interface {
	Observe(context.Context, Context, ObservationRequest) (Observation, error)
}
