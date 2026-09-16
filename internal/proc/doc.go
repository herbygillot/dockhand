// Package proc drives workflow cycles within the current Dockhand process.
//
// Manager runs a persistent loop or attaches to explicit jobs until an admission
// or completion milestone. It owns loop pacing, context cancellation, and observer
// callbacks. Workflow and state coordinate claims across processes; stopping an
// attachment leaves accepted work recorded and does not request its cancellation.
package proc
