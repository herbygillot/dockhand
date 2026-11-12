package tart

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/buildenv"
	tartvm "github.com/herbygillot/dockhand/internal/tart"
)

// stopWait bounds how long Stop waits for an attempt's launch lock once it
// has stopped the clone: the clone's tart run holds it until the VM's
// process exits.
const stopWait = time.Minute

// Stop ends what an attempt launched in Tart (buildenv.Stopper), the removal
// Tart already has (RemoveLeftover), given the clone the attempt noted, in
// two parts: the stop first, which needs no lock, since the clone is the
// check's own and its cancel is recorded, then the delete once it holds
// the attempt's launch lock, which the clone's tart run holds for as long
// as the VM lives, so no clone can still be made and none still runs. A
// clone gone by then is ended. A clone a finished attempt kept for shell
// is no unfinished work: it is left, and said. A stop or a delete that
// fails is not known, with Tart's words, and the next ask tries again (the
// lifecycle design's "Tart").
func (p *Provider) Stop(ctx context.Context, request buildenv.StopRequest) ([]string, error) {
	vm := request.Execution.ProviderRef
	if request.Execution.Launch != "" {
		var note launchNote
		if err := json.Unmarshal([]byte(request.Execution.Launch), &note); err != nil {
			return nil, fmt.Errorf("attempt %d's note of what it launched can't be read: %w", request.Execution.Attempt, err)
		}
		vm = note.Clone
	}
	if vm == "" {
		// Nothing was named, so nothing was cloned; holding the lock, none
		// can be.
		release, err := request.Lock.Hold(ctx, stopWait)
		if err != nil {
			return nil, err
		}
		release()
		return nil, nil
	}
	if !strings.HasPrefix(vm, clonePrefixAll) {
		return nil, fmt.Errorf("%s is not a check's clone, so dockhand doesn't stop it", vm)
	}
	home, _ := p.home()
	if _, kept := tartvm.KeptFrom(home, vm); kept && home != "" {
		return []string{fmt.Sprintf("the clone %s, kept for dockhand shell, which dockhand clean removes", vm)}, nil
	}
	m, err := p.vms()
	if err != nil {
		return nil, err
	}
	if err := m.Stop(ctx, vm); err != nil {
		return nil, fmt.Errorf("tart couldn't stop %s: %w", vm, err)
	}
	release, err := request.Lock.Hold(ctx, stopWait)
	if err != nil {
		return nil, fmt.Errorf("%s was asked to stop, and %w", vm, err)
	}
	defer release()
	if err := m.Delete(ctx, vm); err != nil {
		return nil, fmt.Errorf("tart couldn't delete %s: %w", vm, err)
	}
	if home != "" {
		if err := tartvm.ForgetClone(home, vm); err != nil {
			return nil, fmt.Errorf("%s was deleted, and its marker couldn't be removed: %w", vm, err)
		}
	}
	return nil, nil
}

var _ buildenv.Stopper = (*Provider)(nil)
