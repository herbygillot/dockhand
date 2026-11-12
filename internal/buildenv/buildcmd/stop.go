package buildcmd

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/buildenv/starter"
)

// Stop ends a script's build (buildenv.Stopper): its starter, found by the
// record in the job's directory the attempt noted, whatever reference the
// script's result later gave, is signalled, and the stop sees its group
// gone and its launch lock free (starter.Stop). What a script started
// elsewhere, or moved out of its group, is the script's own (the lifecycle
// design's "Script"). An attempt from before dockhand noted its directory
// recorded nothing that names its shell: not known.
func (p *Provider) Stop(ctx context.Context, request buildenv.StopRequest) ([]string, error) {
	if request.Execution.Launch == "" {
		if request.Execution.ProviderRef == "" {
			release, err := request.Lock.Hold(ctx, p.stopBound())
			if err != nil {
				return nil, err
			}
			release()
			return nil, nil
		}
		return nil, fmt.Errorf("attempt %d was started before dockhand recorded where, so nothing names its shell to stop", request.Execution.Attempt)
	}
	var note launchNote
	if err := json.Unmarshal([]byte(request.Execution.Launch), &note); err != nil || note.Directory == "" {
		return nil, fmt.Errorf("attempt %d's note of what it launched can't be read: %v", request.Execution.Attempt, err)
	}
	return nil, starter.Stop(ctx, note.Directory, request.Lock, p.stopBound())
}

// stopBound is how long a stop waits for the launch lock once the starter is
// asked to stop: its grace, and a margin for the group's SIGKILL.
func (p *Provider) stopBound() time.Duration {
	grace := p.Grace
	if grace <= 0 {
		grace = 30 * time.Second
	}
	return grace + 15*time.Second
}

var _ buildenv.Stopper = (*Provider)(nil)
