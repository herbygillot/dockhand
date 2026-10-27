# Item 9's findings where a person and a reviewer look

2026-10-27, Then's item 9's first follow-up (prioritization, 34690651 and
0a88387f).

- **The words are the engine's** (`InstallFinding.Lines`): the check's
  report, status's detail, and the description say a finding alike, view
  marking a line that asks a look.
- **Status's detail** says them under "Against", gathered in the status
  report beside the link findings (`StatusReport.Installed`), for the
  branches shown in detail alone.
- **The description** says them under Tested on, "Against the port's
  earlier build:", a moved library naming which of its dependents the
  branch's revision bumps rebuild: "…, and this branch rebuilds
  gpsbabel", or "each" (`bodyFacts.Installed`, prdescription
  `TestedOn.Installed`).
- **The earlier build's version** is named beside its check where it's
  older than master's, by MacPorts' vercmp: "against check-12 (0.27.7,
  older than master's 0.28.2)", items 9's and 10's ask.
- Tests: TestAMovedLibraryNamesTheDependentsTheBranchRebuilds,
  TestInstalledFindingsAreSaidUnderTestedOn.
