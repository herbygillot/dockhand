package tool

// IsTerminal reports whether a descriptor is a terminal, by asking the
// kernel for its terminal attributes — the question a shell has to
// settle before requesting a TTY, because tart exec -t on anything
// else dies on the terminal-size ioctl. It is deliberately not
// os.ModeCharDevice: /dev/null is a character device too, and a stdin
// redirected from it must not pass. The ioctl is per platform (see
// the sibling files); a platform without one answers no.
func IsTerminal(fd uintptr) bool { return isTerminal(fd) }
