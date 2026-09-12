package ledger

import (
	"github.com/herbygillot/dockhand/v2/internal/record"
)

const (
	StateRef = "refs/dockhand/state"
	NotesRef = "refs/notes/dockhand/verify"
	Schema   = 1
)

type State struct {
	Changes      map[record.ChangeID]record.Change
	Revisions    map[record.RevisionID]record.Revision
	Jobs         map[record.JobID]record.Job
	Requests     map[record.RequestID]record.JobID
	Controls     map[record.RequestID]record.ControlRequest
	Plans        map[record.JobID]record.VerificationPlan
	Attempts     map[record.AttemptID]record.Attempt
	Resources    map[record.ResourceID]record.Resource
	Publications map[record.PublicationID]record.PublicationAction
	PullRequests map[record.PullRequestID]record.PullRequest
}

func NewState() State {
	return State{
		Changes:      make(map[record.ChangeID]record.Change),
		Revisions:    make(map[record.RevisionID]record.Revision),
		Jobs:         make(map[record.JobID]record.Job),
		Requests:     make(map[record.RequestID]record.JobID),
		Controls:     make(map[record.RequestID]record.ControlRequest),
		Plans:        make(map[record.JobID]record.VerificationPlan),
		Attempts:     make(map[record.AttemptID]record.Attempt),
		Resources:    make(map[record.ResourceID]record.Resource),
		Publications: make(map[record.PublicationID]record.PublicationAction),
		PullRequests: make(map[record.PullRequestID]record.PullRequest),
	}
}

type Snapshot struct {
	Version record.ObjectID
	State   State
}
