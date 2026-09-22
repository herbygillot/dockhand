// Package testsupport holds helpers that tests in many packages share. Only
// tests import it.
//
// WriteExecutable writes a program that a test runs in place of an external
// tool, such as a stand-in tart, git, or portindex, so that the program starts
// even when a parallel test forks while it is being written.
package testsupport
