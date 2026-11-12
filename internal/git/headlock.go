package git

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/filelock"
	"github.com/herbygillot/dockhand/internal/procid"
)

// HeadHeldError is a head another process is submitting to: who, as that
// process recorded itself in the lock (docs/designs/publication-record.md,
// "One submission at a time").
type HeadHeldError struct {
	Head  string
	PID   int
	Start string
}

func (e *HeadHeldError) Error() string {
	if e.PID == 0 {
		return fmt.Sprintf("another dockhand is submitting to %s; nothing was done", e.Head)
	}
	return fmt.Sprintf("dockhand process %d is submitting to %s; nothing was done. If it hangs, stop it: kill %d", e.PID, e.Head, e.PID)
}

// WithHeadLock holds the lock of a fork's branch a submission writes to,
// head as owner/repo:branch, around fn, and refuses a second caller with
// nothing done, naming the process that holds it. The lock is never taken
// from a live process: it is the operating system's, released when that
// process and any git or gh it started have exited, since the lock's file
// is handed to each of them (filelock.Carry). The key is one spelling of
// the head, the owner and repository in lower case, so two local branches
// aimed at one remote branch share it; the file is never removed.
func (r *Repository) WithHeadLock(ctx context.Context, head string, fn func(context.Context) error) (err error) {
	repository, branch, ok := strings.Cut(head, ":")
	if !ok || !strings.Contains(repository, "/") || !ValidBranchName(branch) || !filepath.IsAbs(r.CommonDir) {
		return fmt.Errorf("git: a head lock requires a repository and owner/repo:branch")
	}
	key := strings.ToLower(repository) + ":" + branch
	path := filelock.Path(filepath.Join(r.CommonDir, "dockhand", "head-locks"), key)
	file, err := filelock.TryAcquire(path)
	if errors.Is(err, filelock.ErrBusy) {
		return headHeld(path, head)
	}
	if err != nil {
		return err
	}
	// A normal return releases it for every holder of the file; a holder
	// killed releases nothing, and a child it started keeps it.
	defer func() { err = errors.Join(err, filelock.Release(file)) }()
	// The holder says who it is, for a refusal to name.
	if self, selfErr := procid.Self(); selfErr == nil {
		if err := file.Truncate(0); err != nil {
			return err
		}
		if _, err := file.WriteAt([]byte(fmt.Sprintf("%d %s\n", self.PID, self.Start)), 0); err != nil {
			return err
		}
	}
	return fn(filelock.Carry(ctx, file))
}

// headHeld reads who holds a head's lock, as it recorded itself.
func headHeld(path, head string) error {
	held := &HeadHeldError{Head: head}
	file, err := os.Open(path)
	if err != nil {
		return held
	}
	defer file.Close()
	data, _ := io.ReadAll(io.LimitReader(file, 256))
	fields := strings.Fields(string(data))
	if len(fields) >= 1 {
		held.PID, _ = strconv.Atoi(fields[0])
	}
	if len(fields) >= 2 {
		held.Start = strings.Join(fields[1:], " ")
	}
	return held
}
