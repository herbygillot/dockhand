// Package macos describes macOS releases and supplies developer-tool, storage,
// and launchd operations.
//
// It maps OS names and Darwin versions, inspects and installs developer tools,
// observes foreign package managers, and renders launchd property lists. Commands
// run through an explicit target function, and disk-image operations use explicit
// paths. Provisioning and verification callers decide which observed capabilities
// are required.
package macos
