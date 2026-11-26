package local

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/herbygillot/dockhand/internal/buildenv/starter"
)

// yours reports whether a file's information is of a file this account
// alone could have written: owned by it, or, where rootToo, by root, which
// writes a root build's results, and writable by no one else. A link is
// not: Lstat's information of one is never a regular file or a directory.
func yours(info fs.FileInfo, rootToo bool) bool {
	if info.Mode().Perm()&0o022 != 0 {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}
	uid := int(stat.Uid)
	return uid == os.Getuid() || rootToo && uid == 0
}

// yourDirectory reports whether path is a directory, not a link to one,
// that this account owns and no one else can write.
func yourDirectory(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.IsDir() && yours(info, false)
}

// yourFile reports whether path is a regular file, not a link to one,
// written by this account or by root, and writable by no one else.
func yourFile(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular() && yours(info, true)
}

// foreign reports whether path is there and isn't yourFile: a check's
// directory holding one is passed over, where one not there yet is read
// as missing.
func foreign(path string) bool {
	_, err := os.Lstat(path)
	return err == nil && !yourFile(path)
}

// readYours reads a file in a check's directory where it is yourFile, and
// refuses it otherwise.
func readYours(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || !yours(info, true) {
		return nil, &fs.PathError{Op: "read", Path: path, Err: fs.ErrPermission}
	}
	return readNoFollow(path)
}

// checkDirectories are the checks' directories in temp a check left or
// builds in, each one this account's alone: a directory there of another
// owner or mode, or a link in its place, is passed over, and nothing in it
// is read or run, since its put-back runs its program as root.
func (l *Local) checkDirectories() []string {
	// Each file read in one, by the name any check's directory keeps it
	// under, is held to the same test.
	held := []string{"owner.json", "results.json", "input.json", "guest.tcl", "before.txt", starter.RecordName}
	entries, err := os.ReadDir(l.temp())
	if err != nil {
		return nil
	}
	var dirs []string
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), localDirectory) {
			continue
		}
		dir := filepath.Join(l.temp(), entry.Name())
		if !yourDirectory(dir) || slices.ContainsFunc(held, func(name string) bool { return foreign(filepath.Join(dir, name)) }) {
			continue
		}
		dirs = append(dirs, dir)
	}
	return dirs
}
