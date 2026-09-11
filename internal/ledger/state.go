package ledger

import (
	"github.com/herbygillot/dockhand/v2/internal/model"
)

const (
	StateRef = "refs/dockhand/state"
	NotesRef = "refs/notes/dockhand/verify"
	Schema   = 1
)

type State struct {
	Changes      map[model.ChangeID]model.Change
	Revisions    map[model.RevisionID]model.Revision
	Jobs         map[model.JobID]model.Job
	Requests     map[model.RequestID]model.JobID
	Controls     map[model.RequestID]model.ControlRequest
	Plans        map[model.JobID]model.VerificationPlan
	Attempts     map[model.AttemptID]model.Attempt
	Resources    map[model.ResourceID]model.Resource
	Publications map[model.PublicationID]model.PublicationAction
	PullRequests map[model.PullRequestID]model.PullRequest
}

func NewState() State {
	return State{
		Changes:      make(map[model.ChangeID]model.Change),
		Revisions:    make(map[model.RevisionID]model.Revision),
		Jobs:         make(map[model.JobID]model.Job),
		Requests:     make(map[model.RequestID]model.JobID),
		Controls:     make(map[model.RequestID]model.ControlRequest),
		Plans:        make(map[model.JobID]model.VerificationPlan),
		Attempts:     make(map[model.AttemptID]model.Attempt),
		Resources:    make(map[model.ResourceID]model.Resource),
		Publications: make(map[model.PublicationID]model.PublicationAction),
		PullRequests: make(map[model.PullRequestID]model.PullRequest),
	}
}

type Snapshot struct {
	Version model.ObjectID
	State   State
}
