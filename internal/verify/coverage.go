package verify

import "github.com/herbygillot/dockhand/internal/record"

// Coverage describes discovered roots and direct dependents for one frozen source.
// Problems are discovery gaps that may conceal additional targets.
type Coverage struct {
	Source   record.Source
	Platform record.Platform
	Targets  []CoverageTarget
	Problems []string
}

// CoverageTarget retains a verification question even if evaluation failed.
// IndexedDependencies are estimates from default-variant metadata, not a guest's
// resolved dependencies. CoverageProblems retain missing or unread index data.
type CoverageTarget struct {
	Target              record.Target
	Root                bool
	Reasons             []string
	Evaluation          *TargetEvaluation
	Problem             string
	IndexedDependencies []string
	CoverageProblems    []string
}

// TargetEvaluation supplies the source-bound build requirements obtained by
// discovery. Native evaluator structures stay within the discovery adapter.
type TargetEvaluation struct {
	Source     record.Source
	Platform   record.Platform
	Target     record.Target
	NeedsXcode bool
}
