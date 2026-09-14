package workflow

import "github.com/herbygillot/dockhand/v2/internal/record"

// Milestone describes attachment, independently of the accepted destination.
type Milestone string

const (
	Admission  Milestone = "admission"
	Completion Milestone = "completion"
)

// Reached evaluates only recorded progress; it never contacts a provider.
func Reached(status Status, milestone Milestone) bool {
	if len(status.Jobs) == 0 || milestone != Admission && milestone != Completion {
		return false
	}
	for _, entry := range status.Jobs {
		if entry.Job.State.Terminal() {
			continue
		}
		if milestone == Admission && (entry.Job.AdmittedAt != nil || entry.Job.ReusedAttempt != "" || entry.Job.Phase == record.PhasePublication && entry.Job.State == record.JobActive) {
			continue
		}
		return false
	}
	return true
}
