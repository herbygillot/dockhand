package macports

import (
	"context"

	"github.com/herbygillot/dockhand/internal/record"
)

// ObservationRequest explicitly selects modeled metadata. It never changes the
// native platform used by Reader.Evaluate or establishes build evidence.
type ObservationRequest struct {
	Platform     record.Platform
	Declarations bool
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

type PortObservation struct {
	// HostAccess records external evaluation inputs even in the native context.
	HostAccess        bool
	Operands          []OperandObservation
	ModeledHostAccess bool
	Declarations      []Declaration
	Distfiles         []Distfile
	Problems          []string
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
