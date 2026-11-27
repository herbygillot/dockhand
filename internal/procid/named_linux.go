package procid

import (
	"os"
	"strconv"
	"strings"
)

// Named lists the processes, other than this one, whose program is named
// name, from /proc. The kernel keeps a program's name to its first 15
// bytes, so name is compared to that much of it. A program that has ended
// but whose parent hasn't yet collected it (state Z) isn't listed.
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
		if ended(pid) {
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

// ended says whether the process has ended and waits for its parent to
// collect it: state Z, the field after the parenthesized name in
// /proc/<pid>/stat. A process gone already has ended too.
func ended(pid int) bool {
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return true
	}
	text := string(stat)
	end := strings.LastIndexByte(text, ')')
	fields := strings.Fields(text[end+1:])
	return end < 0 || len(fields) == 0 || fields[0] == "Z"
}
