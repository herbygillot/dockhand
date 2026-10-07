// Package failpoint stops dockhand at a named step, as a kill there would,
// for the acceptance harness's kill rows (the project's
// plan/acceptance-harness.md, H5). It works only in a build with the
// acceptance tag: a normal build's Hit does nothing, holds no switch, and
// names no variable, which a test proves of the release binary.
//
// In an acceptance build, DOCKHAND_FAILPOINT=<step>:kill kills the process
// with SIGKILL when it reaches the step. The steps are:
//   - update.prepared, after an update has fetched and prepared its edit,
//     before writing it;
//   - tidy.prepared and rebase.prepared, after the checkpoint is recorded,
//     before the branch moves;
//   - submit.pushed, after the push, before the pull request opens;
//   - submit.created, once GitHub has opened the pull request, before
//     dockhand records it;
//   - check.running, once a check's provider run is recorded running.
//
// DOCKHAND_FAILPOINT=<step>:fault and <step>:error make a step fail once in
// the process, as a fault in dockhand's own handling would, or as an error
// nothing classifies would. The steps are:
//   - tart.results, as the Tart provider reads a guest's results.
package failpoint
