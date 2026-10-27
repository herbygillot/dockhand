package testsupport

import "time"

// Settle bounds a test's wait for what a process or another goroutine
// does. It's a bound, not a pace: a wait ends as soon as its condition
// holds, and on a loaded machine, such as CI's runner beside the other
// tests, that can take far longer than it does alone; five seconds failed
// there for serve's words (2026-10-02), and ten for a local check's
// install beside the full suite (rc6's gate). A wait that asserts
// something happens within a time keeps its own bound and says so.
const Settle = 2 * time.Minute
