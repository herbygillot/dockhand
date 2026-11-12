package procid

import "golang.org/x/sys/unix"

// BootSession names the Mac's current boot, which changes at each restart
// (kern.bootsessionuuid): a record of a process from another boot names
// nothing running now.
func BootSession() (string, error) { return unix.Sysctl("kern.bootsessionuuid") }
