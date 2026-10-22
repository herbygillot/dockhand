package view

// The marks, each with one meaning in every command's text (v0.5.0 part
// 5, item 2, from the command UX review's §10): how much a reader should
// care, never whether something holds automation, which a line says in a
// word, "held". "·" is a separator only, and a line for your information
// carries no mark.
const (
	// Broken is what failed or can't be done: "✗ fails at the base".
	Broken = "✗"
	// Look is what wants your look: "! held: upstream's COPYING changed".
	Look = "!"
	// Done is what's done, or ready: "✓ passed".
	Done = "✓"
	// Working is what's under way: "⋯ checking".
	Working = "⋯"
)

// Unmarked is the space a mark takes, for a line in a marked list that
// carries none.
const Unmarked = " "
