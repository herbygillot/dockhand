package procid

import (
	"os"
	"strings"
)

// BootSession names the machine's current boot, which changes at each
// restart: a record of a process from another boot names nothing running
// now.
func BootSession() (string, error) {
	data, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	return strings.TrimSpace(string(data)), err
}
