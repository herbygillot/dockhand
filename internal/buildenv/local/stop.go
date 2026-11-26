package local

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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
// prefix left, said with what puts it back. A build run as root is
// stopped the same way, its starter stopping the sudo and, through the
// root stop, its guest (the design's rule 4), only where this Mac's sudo
// has passed the probe of a root build's stop (rootStopProved), and the
// guest's group is seen gone too; elsewhere it isn't signalled, and its
// stop isn't known, with what the probe found and the one command to stop
// it by hand.
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
	// The directory and its record are this account's alone, or nothing
	// in them is read or signalled.
	if _, statErr := os.Lstat(dir); dir != "" && statErr == nil && !yourDirectory(dir) {
		return nil, fmt.Errorf("%s isn't this account's alone, so nothing it names was stopped", dir)
	}
	data, err := readYours(filepath.Join(dir, "owner.json"))
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
		proved, why := l.rootStopProved(l.sudoCommand(), made.InSession)
		switch {
		case !made.Root:
			why = "its prefix's registry isn't this user's to write"
		case legacy || !made.RootStop:
			why = "it was started before dockhand handed a root build its stop"
		case proved:
			// A proved sudo may since have stopped answering sudo -n, its
			// rule taken away: the starter's root stop is refused, it stays
			// with the lock, and the stop isn't known, with the command to
			// stop the build by hand, as on a Mac never proved.
			if err := starter.Stop(ctx, dir, request.Lock, stopBound); err != nil {
				return nil, l.rootStopUnseen(dir, made, err)
			}
			if err := guestGroupGone(ctx, dir); err != nil {
				return nil, l.rootStopUnseen(dir, made, err)
			}
			return l.putBackStopped(ctx, dir, made), nil
		}
		return nil, l.rootStopWords(dir, made, why)
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

// guestGroupGone sees a root build's guest's group gone once its starter
// has ended and the lock is free: what the guest started, where it leads a
// group of its own, that let go of its standard input. Its PID is the one
// its first save recorded; a group that still answers, or answers that it
// may not be signalled, isn't known to be gone.
func guestGroupGone(ctx context.Context, dir string) error {
	results, _ := (&guest.Follower{Root: dir}).Read(ctx, hostGuest{})
	pid, err := strconv.Atoi(results.Environment["pid"])
	if err != nil || pid <= 1 {
		return nil
	}
	err = syscall.Kill(-pid, 0)
	for deadline := time.Now().Add(groupWait); err == nil || errors.Is(err, syscall.EPERM); {
		if time.Now().After(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
		err = syscall.Kill(-pid, 0)
	}
	if err == nil || errors.Is(err, syscall.EPERM) {
		return fmt.Errorf("the root build's guest, PID %d, leaves process group %d with members after its stop; dockhand doesn't signal them, since they can't be told from another group that took the number", pid, pid)
	}
	return nil
}

// groupWait is how long a stop asks again of a group that still answers.
const groupWait = 2 * time.Second

// rootStopWords say why a root build's stop isn't known, why, with the
// one command to stop it by hand and the one to run after.
func (l *Local) rootStopWords(dir string, made owner, why string) error {
	return fmt.Errorf("%s builds in %s as root, and dockhand doesn't stop this root build: %s; %s", made.Check, made.Prefix, why, byHandWords(dir, made))
}

// rootStopUnseen says a root build dockhand asked to stop wasn't seen to
// end, why, and the one command to stop it by hand.
func (l *Local) rootStopUnseen(dir string, made owner, err error) error {
	return fmt.Errorf("%s builds in %s as root, and it was asked to stop but wasn't seen to end: %w; %s", made.Check, made.Prefix, err, byHandWords(dir, made))
}

// byHandWords are the one command that stops a root build by hand, and the
// one to run after: the starter's group, where it recorded itself, else
// the guest program's PID, as its results carry it.
func byHandWords(dir string, made owner) string {
	group := "<the guest program's PID>"
	if record, ok, _ := starter.ReadRecord(dir); ok {
		group = "-" + strconv.Itoa(record.PID)
	} else if results, err := (&guest.Follower{Root: dir}).Read(context.Background(), hostGuest{}); err == nil && results.Environment["pid"] != "" {
		group = results.Environment["pid"]
	}
	return fmt.Sprintf("stop it by hand, sudo kill -TERM %s, then dockhand cancel %s asks again", group, made.Check)
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
	// A prefix changed as root is put back as root, under sudo -n, never a
	// prompt: where sudo would ask, the put-back fails and says how to do
	// it by hand (the lifecycle design's "Local and Script").
	var sudo []string
	if made.Root {
		sudo = append(slices.Clone(l.sudoCommand()), "-n")
	}
	if err := l.restore(restoring, dir, made.Prefix, made.Check, sudo); err != nil {
		return []string{fmt.Sprintf("%s, which %s changed and couldn't put back: %v; dockhand undo %s, or the next check in it, tries again. %s", made.Prefix, made.Check, err, made.Check, l.byHand(dir, made.Check, snapshot))}
	}
	_ = os.RemoveAll(dir)
	return nil
}

var _ buildenv.Stopper = (*Local)(nil)
