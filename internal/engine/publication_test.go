package engine

import (
	"github.com/herbygillot/dockhand/internal/branchstate"
	"github.com/herbygillot/dockhand/internal/model"
)

// The publication rule and the acceptances are branchstate's; these words
// them as submit says them, for the tests that read them in the engine's
// words.
func publicationProblems(evidence Evidence, accepted []string, atBase map[string][]string, standing map[string]AcceptedFailure) []string {
	var problems []string
	for _, block := range branchstate.PublicationBlocks(evidence, accepted, atBase, standing) {
		problems = append(problems, block.Words(Vocabulary{}))
	}
	return problems
}

func acceptedReasons(evidence Evidence, accepted []string, atBase map[string][]string, standing map[string]AcceptedFailure) map[string]AcceptedFailure {
	return branchstate.AcceptedReasons(evidence, accepted, atBase, standing)
}

func standingAccepted(evidence Evidence, standing []model.Decision) map[string]AcceptedFailure {
	return branchstate.StandingAccepted(evidence, standing)
}

func acceptanceProblem(evidence Evidence, accept []string, atBase map[string][]string, standing bool) error {
	return branchstate.AcceptanceProblem(evidence, accept, atBase, standing)
}
