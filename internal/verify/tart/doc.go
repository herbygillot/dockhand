// Package tart verifies ports in isolated local Tart virtual machines.
//
// Provider binds image identity and capabilities to accepted build inputs,
// prepares source before reserving shared capacity, and records recoverable
// execution identities. It stages and observes guest builds, preserves diagnostic
// logs, and handles cancellation, release, and artifact retention.
//
// Shared VM control lives in tart/host, image provisioning in tart/provision,
// and source packaging in verify/staging. Workflow coordinates attempts and
// claims around provider calls; provider state coordinates concrete executions
// across processes and repository registrations.
package tart
