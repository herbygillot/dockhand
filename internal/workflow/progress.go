package workflow

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
		if jobTerminal(entry.Job.State) {
			continue
		}
		if milestone == Admission && entry.Job.AdmittedAt != nil {
			continue
		}
		return false
	}
	return true
}
