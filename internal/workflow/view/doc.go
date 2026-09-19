// Package view is the contribution-centric projection of the engine's
// recorded snapshot and the phrasebook for it: the rows a person reads in
// status, its JSON, and the live table, and the words an action's summary
// uses for the same job. It reads records only; it knows neither the engine
// nor the store, so what a change, phase, state, or next step is called
// lives in one place that nothing operational depends on.
package view
