package coord

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// processStart reads the process's start time in clock ticks since boot,
// qualified by the boot's ID so a reboot cannot make an old record match.
func processStart(pid int) (string, error) {
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return "", err
	}
	// The command name, field 2, is parenthesized and may hold spaces, so
	// fields are counted from its closing parenthesis.
	text := string(stat)
	end := strings.LastIndexByte(text, ')')
	if end < 0 {
		return "", fmt.Errorf("unreadable /proc/%d/stat", pid)
	}
	fields := strings.Fields(text[end+1:])
	// After the name come state (field 3) onward; starttime is field 22.
	if len(fields) < 20 {
		return "", fmt.Errorf("short /proc/%d/stat", pid)
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", err
	}
	return "linux:" + strings.TrimSpace(string(boot)) + ":" + fields[19], nil
}
