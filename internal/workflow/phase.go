package workflow

import (
	"github.com/herbygillot/dockhand/internal/record"
)

func initialPhase(action record.Action) record.JobPhase {
	switch action {
	case record.Bump, record.BumpRevision, record.RefreshChecksums:
		return record.PhasePreparation
	case record.Verify:
		return record.PhaseVerification
	case record.Publish:
		return record.PhasePublication
	default:
		return ""
	}
}

func preparationAction(action record.Action) bool {
	return action == record.Bump || action == record.BumpRevision || action == record.RefreshChecksums
}
