// Package subprocess runs one external command with the conventions every
// Dockhand tool runner shares: a bounded wait after cancellation, captured
// output, the context error joined into the cause, and one error shape that
// names the tool, its command, and what it wrote to stderr. Each caller keeps
// its own environment policy; this package never adds to or filters it.
//
// A command run to completion, for its output or its exit, goes through Run.
// os/exec is used directly only by one that holds the terminal, as an
// editor or xcodes does; runs as long as its caller, as the Tcl interpreter,
// a guest, and a person's build do; streams what it reads and writes, as a
// decompressor does; or outlives the process, as the background cleanup
// does.
package subprocess
