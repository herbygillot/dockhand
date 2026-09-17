package workflow

import (
	"github.com/herbygillot/dockhand/internal/record"
)

func initialPhase(action record.Action) record.JobPhase {
	switch action {
	case record.Bump, record.BumpRevision, record.RefreshChecksums, record.Amend, record.Rebase:
		return record.PhasePreparation
	case record.Verify:
		return record.PhaseVerification
	case record.Publish:
		return record.PhasePublication
	default:
		return ""
	}
}
