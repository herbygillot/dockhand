// Package policy answers read-only questions about recorded workflow state:
// which verification evidence applies to a build question, whether evidence
// covers a publication's whole cohort, and whether a publication action still
// matches its job. It reads through state.Reader and never claims, writes, or
// calls a provider, so both intake and drivers can ask the same questions.
package policy
