// Package host is the Mac the command line runs on, or a test's stand-in
// for it: its clock, identity, notifications, launchd, the background
// cleanup and drain it starts, its Tart and its login's stores, and the
// timings serve and dockhand console keep (Timings). MachineHost is the
// real one, and the one place that imports Tart, the Keychain, and macOS
// (part 4, item 2).
package host
