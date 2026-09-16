package record

import "time"

type RepositoryID string

type Repository struct {
	ID        RepositoryID
	CommonDir string
	CreatedAt time.Time
}

type RequestKind string

const (
	JobRequest    RequestKind = "job"
	CancelRequest RequestKind = "cancel"
)

type AcceptedRequest struct {
	// JoinedJob durably associates an equivalent request with existing work.
	JoinedJob   JobID `json:",omitempty"`
	ID          RequestID
	Kind        RequestKind
	Payload     []byte
	AcceptedAt  time.Time
	CompletedAt *time.Time
}

type Submission struct {
	ID         RequestID
	AttemptID  AttemptID
	Sequence   int
	Provider   string
	RunID      string
	CreatedAt  time.Time
	AdmittedAt *time.Time
	ClosedAt   *time.Time
}
