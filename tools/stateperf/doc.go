// Command stateperf measures SQLite state and workflow-cycle costs with synthetic
// job histories.
//
// It creates temporary fixtures with shared or distinct source identities and
// emits JSON timing samples, including concurrent driver-process measurements.
// It is a developer measurement tool independent of the dockhand CLI.
package main
