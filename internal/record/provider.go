package record

import (
	"encoding/json"
	"time"
)

// ProviderPool identifies a local resource namespace shared by repositories.
type ProviderPool struct {
	ID        string
	Scope     string
	Directory string
	Capacity  int
}

type ExecutionState string

const (
	ExecutionReserved ExecutionState = "reserved"
	ExecutionAdmitted ExecutionState = "admitted"
	ExecutionClosed   ExecutionState = "closed"
	ExecutionReleased ExecutionState = "released"
)

// ProviderExecution records provider-owned effects, independently of the
// workflow's adoption of those effects into attempts and resources.
type ProviderExecution struct {
	ID           RequestID
	RepositoryID RepositoryID
	AttemptID    AttemptID
	Resource     string
	Payload      json.RawMessage
	State        ExecutionState
	Occupied     bool
	Result       json.RawMessage
	CreatedAt    time.Time
}
