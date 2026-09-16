package record

import "time"

// ResourceState describes an owned environment's retention and cleanup status.
// This lifecycle continues independently of job completion.
type ResourceState string

const (
	// ResourceActive identifies a resource assigned to an admitted attempt.
	ResourceActive ResourceState = "active"
	// ResourceRetained keeps an environment available for diagnosis.
	ResourceRetained ResourceState = "retained"
	// ResourceReleaseRequested records an obligation to release the resource.
	ResourceReleaseRequested ResourceState = "release-requested"
	// ResourceReleased records provider-confirmed release or absence.
	ResourceReleased ResourceState = "released"
	// ResourceUncertain records unresolved provisioning or an unconfirmed release.
	ResourceUncertain ResourceState = "uncertain"
)

// ResourceHandle identifies one resource lifetime in a provider namespace.
// The provider must preserve this identity for recovery and must not recycle it
// for another attempt's resource.
type ResourceHandle struct {
	Provider string
	ID       string
}

// Resource retains ownership, diagnostic retention, and release obligations
// for an attempt's environment. A terminal job does not establish that its
// resources have been released.
type Resource struct {
	ID           ResourceID
	AttemptID    AttemptID
	SubmissionID RequestID
	Handle       ResourceHandle
	State        ResourceState
	// Lease grants ownership of a cleanup action separately from attempt work;
	// its retry time covers release and released-diagnostic pruning.
	Lease
	// RetainUntil makes a retained resource eligible for cleanup at this time.
	// Nil retains it indefinitely. Expiry does not authorize releasing an active run.
	RetainUntil *time.Time
	// ReleasedAt records when release was confirmed, if it has been confirmed.
	ReleasedAt *time.Time
	// ArtifactsPrunedAt records confirmed removal of released diagnostic files.
	// Evidence and historical file references remain in the attempt.
	ArtifactsPrunedAt *time.Time
	LastError         string
}
