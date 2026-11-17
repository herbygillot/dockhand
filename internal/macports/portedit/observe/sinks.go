package observe

import "strings"

// sinkFor reports whether a command is a benign sink for a host answer's
// value: patchfiles is not, since dockhand fetches, checksums and
// rewrites the patch list (architecture's ruling on case n).
func sinkFor(name string) bool {
	return benignSink(name) && !strings.HasPrefix(name, "patchfiles")
}

// ActedOption names what the experiment compares among ReadOptions.
func ActedOption(name string) bool { return !sinkFor(name) }
