package fidelity

// ChangePolicy is the version of the rules a change record is made under:
// which fields SubportChanges compares, and how it normalizes them. A
// record made under earlier rules no longer stands. Policy 3 says what a
// revision changed where this Mac's evaluation can't see (Unseen).
const ChangePolicy = 3
