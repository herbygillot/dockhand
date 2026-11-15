package observe

// ActedOption is the complement of the follower's benign-sink predicate.
// It depends only on the option name; patchfiles remains acted on.
func ActedOption(name string) bool { return !sinkFor(name) }
