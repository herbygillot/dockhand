package workflow

import (
	"github.com/herbygillot/dockhand/internal/record"
)

func initialPhase(action record.Action) record.JobPhase {
	switch {
	case action.Prepares():
		return record.PhasePreparation
	case action == record.Verify:
		return record.PhaseVerification
	case action == record.Publish:
		return record.PhasePublication
	default:
		return ""
	}
}
