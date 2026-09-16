// Package publish plans and reconciles the remote publication of verified
// contributions.
//
// Service resolves destinations, checks contribution scope and supplied
// verification evidence, prepares pull-request content, and observes and writes
// pull requests. Observations are checked against the accepted source and remote
// identity. Workflow coordinates guarded Git pushes, complete dependent coverage,
// claims, and durable checkpoints around these external effects; forge adapters
// implement the remote protocol.
package publish
