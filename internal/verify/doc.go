// Package verify defines verification plans, provider contracts, and evidence
// assessment.
//
// Planning freezes concrete target and build requirements, including isolated
// dependent coverage. Judgment converts provider observations into evidence, and
// applicability checks compare recorded results with requested inputs. Providers
// implement idempotent submission, recovery, observation, cancellation, and resource
// release; optional capabilities supply logs and artifact cleanup.
//
// Workflow selects stored attempts, owns claims and scheduling, and records the
// results. This package supplies the contracts and policy calculations used at
// that boundary.
package verify
