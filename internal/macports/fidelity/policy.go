package fidelity

// ChangePolicy is the version of the rules a change record is made under:
// which fields SubportChanges compares, and how it normalizes them. A
// record made under earlier rules no longer stands. Policy 3 says what a
// revision changed where this Mac's evaluation can't see (Unseen). Policy
// 4 is the same rules, raised because update primed records under 3
// without Unseen, which no later read made again (the architecture
// re-synthesis, L2c): those are made again now. Policy 5 compares
// macosx_deployment_target too, which a raised one holds for a look.
// Policy 6 compares a renamed subport with its old name's port
// (macports.PortTransition), where it read as added beside that port
// removed. Policy 7 says which of _resources' PortGroup files differ
// between the sides and the ports each moved (ResourceChanges), and
// compares a port's PortGroups by their files, not their blobs.
const ChangePolicy = 7
