// Package installation observes and installs MacPorts on an explicit command
// target.
//
// It selects the installer for a requested Base version and macOS release and
// reports installation, platform, active-port, and Tcl-package facts. Provisioning
// and verification callers apply their own readiness and build requirements to
// these observations.
package installation
