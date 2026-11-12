package local

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/buildenv/guest"
	"github.com/herbygillot/dockhand/internal/buildenv/starter"
	"github.com/herbygillot/dockhand/internal/filelock"
	"github.com/herbygillot/dockhand/internal/model"
)

// stopBound is how long a stop waits for the attempt's launch lock once the
// starter is asked to stop: its grace, and a margin for its group's kill.
const stopBound = stopGrace + 15*time.Second

// Stop ends a local check's build (buildenv.Stopper), found by the check's
// directory the attempt noted (the lifecycle design's "Local and Script"):
// its starter is signalled, the stop sees its group gone and the launch
// lock free (starter.Stop), and then the prefix is put back by the
// restore a check already runs, under the prefix's lock. Where another
// check holds that lock, or the put-back fails, it is ended with the
// prefix left, said with what puts it back. A build run as root isn't
// signalled: whether this Mac's sudo passes a stop on, and hands the lock
// on, is the design's test still to run, so it isn't known, with the one
// command to stop it by hand.
func (l *Local) Stop(ctx context.Context, request buildenv.StopRequest) ([]string, error) {
	dir := request.Execution.ProviderRef
	legacy := request.Execution.Launch == "" || request.Execution.Launch == model.LaunchUnrecorded
	if !legacy {
		var note launchNote
		if err := json.Unmarshal([]byte(request.Execution.Launch), &note); err != nil || note.Directory == "" {
			return nil, fmt.Errorf("attempt %d's note of what it launched can't be read: %v", request.Execution.Attempt, err)
		}
		dir = note.Directory
	}
	var made owner
	data, err := os.ReadFile(filepath.Join(dir, "owner.json"))
	switch {
	case dir == "" || errors.Is(err, os.ErrNotExist):
		// Nothing was started there, or its directory is gone with what
		// it changed put back; holding the lock, nothing can be started.
		release, err := request.Lock.Hold(ctx, stopBound)
		if err != nil {
			return nil, err
		}
		release()
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("%s's record can't be read: %w", dir, err)
	}
	if err := json.Unmarshal(data, &made); err != nil {
		return nil, fmt.Errorf("%s's record can't be read: %w", dir, err)
	}
	if made.Root || syscall.Access(filepath.Join(made.Prefix, "var", "macports", "registry"), 2) != nil {
		return nil, l.rootStopWords(dir, made)
	}
	if legacy {
		// Started before the starter: its guest program's PID, where its
		// results carry one, is all that names it.
		results, _ := (&guest.Follower{Root: dir}).Read(ctx, hostGuest{})
		if program, _ := strconv.Atoi(results.Environment["pid"]); alive(program) {
			return nil, fmt.Errorf("%s's guest program, PID %d, started before dockhand recorded its starter, runs still; dockhand doesn't signal it", made.Check, program)
		}
	} else if err := starter.Stop(ctx, dir, request.Lock, stopBound); err != nil {
		return nil, err
	}
	return l.putBackStopped(ctx, dir, made), nil
}

// rootStopWords say why a root build's stop isn't known, with the one
// command to stop it by hand and the one to run after.
func (l *Local) rootStopWords(dir string, made owner) error {
	// The starter's group, where it recorded itself, else the guest
	// program's PID, as its results carry it.
	group := "<the guest program's PID>"
	if record, ok, _ := starter.ReadRecord(dir); ok {
		group = "-" + strconv.Itoa(record.PID)
	} else if results, err := (&guest.Follower{Root: dir}).Read(context.Background(), hostGuest{}); err == nil && results.Environment["pid"] != "" {
		group = results.Environment["pid"]
	}
	return fmt.Errorf("%s builds in %s as root, and dockhand doesn't stop a root build until this Mac's sudo has passed its test of a stop; stop it by hand, sudo kill -TERM %s, then dockhand cancel %s asks again", made.Check, made.Prefix, group, made.Check)
}

// putBackStopped puts back the prefix a stopped check left changed, as a
// check's own end does (settle), and says what it couldn't: nothing where
// the prefix is as it was and the directory is removed.
func (l *Local) putBackStopped(ctx context.Context, dir string, made owner) []string {
	results, err := (&guest.Follower{Root: dir}).Read(ctx, hostGuest{})
	snapshot, restored := results.Environment["snapshot"], results.Environment["restored"]
	switch {
	case err != nil:
		return []string{unreadableWords(made.Check, made.Prefix, dir, err)}
	case snapshot == "", restored == "yes":
		_ = os.RemoveAll(dir)
		return nil
	case restored == "kept":
		return []string{fmt.Sprintf("the branch's versions active in %s, kept; dockhand undo %s puts it back as it was", made.Prefix, made.Check)}
	case restored != "":
		return []string{fmt.Sprintf("%s, which %s changed and couldn't put back; dockhand undo %s, or the next check in it, tries again. %s", made.Prefix, made.Check, made.Check, l.byHand(dir, made.Check, snapshot))}
	}
	// The prefix's lock, briefly: where another check holds it, the
	// put-back isn't run beside it, nor assumed of it.
	canonical, err := filepath.EvalSymlinks(made.Prefix)
	if err != nil {
		canonical = filepath.Clean(made.Prefix)
	}
	quick, cancel := context.WithTimeout(ctx, time.Second)
	file, err := filelock.AcquireAcrossUsers(quick, filelock.Path(l.locks(), "prefix:"+canonical))
	cancel()
	if err != nil {
		return []string{fmt.Sprintf("%s, which %s changed, held now by %s; the next check in it puts it back before it builds, or dockhand undo %s", made.Prefix, made.Check, held(filelock.Path(l.locks(), "prefix:"+canonical)), made.Check)}
	}
	defer file.Close()
	restoring, cancelRestore := context.WithTimeout(ctx, 10*time.Minute)
	defer cancelRestore()
	if err := l.restore(restoring, dir, made.Prefix, made.Check, nil); err != nil {
		return []string{fmt.Sprintf("%s, which %s changed and couldn't put back: %v; dockhand undo %s, or the next check in it, tries again. %s", made.Prefix, made.Check, err, made.Check, l.byHand(dir, made.Check, snapshot))}
	}
	_ = os.RemoveAll(dir)
	return nil
}

var _ buildenv.Stopper = (*Local)(nil)
