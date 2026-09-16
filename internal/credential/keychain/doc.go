// Package keychain stores credentials in the current user's macOS Keychain.
//
// Store implements credential lookup, replacement, and removal through the
// security executable. Writes pass secrets through standard input rather than
// process arguments; service and account keys identify the stored entry.
package keychain
