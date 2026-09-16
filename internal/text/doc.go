// Package text provides byte spans and edits for source-preserving transformations.
//
// Spans refer to the original source buffer. Apply checks edit bounds and overlap
// before producing replacement bytes, and Position maps byte offsets to line and
// column diagnostics. Language-specific parsing and edit intent belong to callers.
package text
