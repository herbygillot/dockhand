package procid

import (
	"bytes"
	"strconv"

	"golang.org/x/sys/unix"
)

// Named lists the processes on this Mac, other than this one, whose program
// is named name, from the system's own process table (kern.proc.all). The
// kernel keeps a program's name to its first 16 bytes, so name is compared
// to that much of it.
func Named(name string) ([]Process, error) {
	infos, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return nil, err
	}
	want := []byte(name)
	if len(want) > 16 {
		want = want[:16]
	}
	self := unix.Getpid()
	var named []Process
	for _, info := range infos {
		comm := info.Proc.P_comm[:]
		if end := bytes.IndexByte(comm, 0); end >= 0 {
			comm = comm[:end]
		}
		pid := int(info.Proc.P_pid)
		if pid == self || !bytes.Equal(comm, want) {
			continue
		}
		start := info.Proc.P_starttime
		named = append(named, Process{PID: pid, Start: "darwin:" + strconv.FormatInt(int64(start.Sec), 10) + "." + strconv.FormatInt(int64(start.Usec), 10)})
	}
	return named, nil
}
