package record

// WorkflowEvidence records a particular remote run attempt, without inferring port phases.
type WorkflowEvidence struct {
	Repository string
	Branch     string
	Commit     ObjectID
	Path       string
	RunID      int64
	RunAttempt int
	URL        string
	Conclusion string
	Jobs       []WorkflowJob
}

type WorkflowJob struct {
	ID         int64
	Name       string
	URL        string
	Status     string
	Conclusion string
	Labels     []string
}
