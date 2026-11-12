package procid

import (
	"os"
	"strconv"
	"strings"
)

// Named lists the processes, other than this one, whose program is named
// name, from /proc. The kernel keeps a program's name to its first 15
// bytes, so name is compared to that much of it.
func Named(name string) ([]Process, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	if len(name) > 15 {
		name = name[:15]
	}
	self := os.Getpid()
	var named []Process
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid == self {
			continue
		}
		comm, err := os.ReadFile("/proc/" + entry.Name() + "/comm")
		if err != nil || strings.TrimSuffix(string(comm), "\n") != name {
			continue
		}
		start, err := Start(pid)
		if err != nil {
			continue
		}
		named = append(named, Process{PID: pid, Start: start})
	}
	return named, nil
}
