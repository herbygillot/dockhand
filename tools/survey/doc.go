// Command survey assesses every port of a ports tree, or a selection of
// them, and compares two surveys.
//
// It is v2's `dockhand assess --all`, kept as a developer measurement tool
// now that v3's command line has no assess: the oracle's phases are each
// proven by a whole-tree survey compared with the one before
// (docs/oracle.md). It writes the same journal, one JSON line per port,
// so its runs compare directly with the baselines kept in
// ~/.dockhand/surveys, and it records how the run was made and how long
// it took beside the journal. It is independent of the dockhand CLI and
// of any database. The assessment itself (Service) and its journal were
// v2's assess package, and live here now that only the survey uses them.
package main
