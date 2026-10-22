// Package cmdtest is the command line's tests' shared harness: a world of
// a MacPorts checkout, an upstream, and a fork (NewWorld), each test's
// stand-ins for compose and the host (Stands, Hosted), command lines run
// through them (Dockhand, RunFor, JSONOf), and the fakes a test gives the
// engine. A test binary hands Main the root's RunWith; cmdtest never
// imports the root, so a verb's external tests and the root's own run the
// same command line (part 4, item 2).
package cmdtest
