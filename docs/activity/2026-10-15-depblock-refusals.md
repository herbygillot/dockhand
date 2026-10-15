# depblock: two false refusals, and an uncertain tag's hint (v0.4.0 part 1, c)

Field testing at c97bca63: rust-glancer (#35397) and certstrap (#35398)
went up by hand, refused by update; with Later's tart-softnet, merged into
the same item.

- **One rule for a cargo build that resolves Git sources online**
  (`depblock.ResolvesOnline`), which `gitPolicy` and the cargo blocks'
  check both read, where each kept a copy reading only an empty
  `cargo.offline_cmd` as online. The arguments `cargo build` runs with
  hold neither `--offline` nor `--frozen`: build.pre_args where the build
  is cargo's own (now read, with build.cmd), else cargo.offline_cmd.
  Checked against MacPorts' evaluation: rust-glancer's cargo.offline_cmd
  is `--locked`, its build.pre_args `--release --locked -v -j18`;
  tart-softnet keeps cargo.offline_cmd `--frozen` and deletes it from
  build.pre_args. Both now resolve online, rust-glancer's Git-pinned
  `edition` and tart-softnet's dhcproto crates left to Cargo with the
  note, where they were refused, and before the update was tried, from
  the current version's check. The refusal's own advice names the rule.
- **A Go module declared without its major version suffix is the same
  row** (`sameMajor`): certstrap's Portfile names go-md2man and
  blackfriday, which an older go2port wrote for go-md2man/v2 and
  blackfriday/v2; at the same lock and checksums they're not overrides.
- **An uncertain tag's refusal names the livecheck that settles it**:
  after "If X is a release: dockhand update port version", "If it isn't,
  as an old tag spelled oddly: a livecheck.regex in the Portfile that
  leaves X out keeps it out of later looks", as certgraph's stray tag on
  an older commit wanted.

Tests: TestACargoBuildResolvesOnlineWithoutOfflineFlags,
TestAGoModuleWithoutItsMajorSuffixIsTheSameRow, and the uncertain refusal
in author_test.
