# A check says how long each part took, and setup names go2port and cargo2port

2026-10-27, the docs dig's I38, its last two parts (roadmap's "A check
says how long each part took, and setup says whether go2port and cargo2port
are there").

- **Times in the report** (view.WriteResults, which check's report and
  `status -v` share): each target built says how long it took, as
  "(4m 12s)" in its cell, and a line under the results says how long the
  check ran in each release: "Took 13m 0s on macOS 14 arm64; 9m 41s on
  macOS 26 arm64." The entry read that `--json` held them; it held the run's
  created and finished times alone, and no target's start was recorded,
  so a target's time is read from its result's recorded time, less the
  one its run recorded before it, or the run's start
  (evidence.TargetTook), since a run builds and records its targets in
  order; a release's is each of the check's own runs there, start to end,
  every attempt counted (evidence.Took). A reused result, another check's,
  or anything under ten seconds says none (made ten, from one, when a scripted test check crossed a second and its report's exact lines changed). prose.Took words them.
- **setup's Vendoring rows** (engine.SetupReport.Vendoring, read with
  depblock.Tools.Resolve as the edits find them): "✓ go2port at …, for Go
  ports' go.vendors", or "! cargo2port is not on PATH; Rust ports'
  cargo.crates need it, as create and update write them: sudo port install
  cargo2port". `setup --json` gives them as `vendoring`.
- Tests: TestTheReportSaysHowLongEachPartTook (a two-release check),
  TestSetupSaysWhetherTheVendoringToolsAreThere, TestTookIsShortEnoughForACell.
