// Package subprocess runs one external command with the conventions every
// Dockhand tool runner shares: a bounded wait after cancellation, captured
// output, the context error joined into the cause, and one error shape that
// names the tool, its command, and what it wrote to stderr. Each caller keeps
// its own environment policy; this package never adds to or filters it.
package subprocess
