// Package host controls concrete Tart VMs and their guest-agent transport.
//
// Machine supplies guarded clone and deletion operations, launchd-backed or
// foreground VM startup, readiness checks, and guest command execution. Callers
// choose the VM identity, process lifetime, and guest workload. Verification
// requests, image recipes, and durable workflow state remain with those callers.
package host
