package fidelity

// ChangePolicy is the version of the rules a change record is made under:
// which fields SubportChanges compares, and how it normalizes them. A
// record made under earlier rules no longer stands. Policy 3 says what a
// revision changed where this Mac's evaluation can't see (Unseen). Policy
// 4 is the same rules, raised because update primed records under 3
// without Unseen, which no later read made again (the architecture
// re-synthesis, L2c): those are made again now.
const ChangePolicy = 4
