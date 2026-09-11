package model

import "time"

type ResourceState string

const (
	ResourceActive           ResourceState = "active"
	ResourceRetained         ResourceState = "retained"
	ResourceReleaseRequested ResourceState = "release-requested"
	ResourceReleased         ResourceState = "released"
	ResourceUncertain        ResourceState = "uncertain"
)

type ResourceHandle struct {
	Provider string
	ID       string
}

type Resource struct {
	ID          ResourceID
	AttemptID   AttemptID
	Handle      ResourceHandle
	State       ResourceState
	Claim       *Claim
	RetainUntil *time.Time
	ReleasedAt  *time.Time
	LastError   string
}
