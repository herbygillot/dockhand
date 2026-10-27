# A modelled context's SDK and tools on MacPorts base's master

2026-10-27, after rc6's tag (roadmap 4697148f): the weekly job against
base's master (22f546e2) failed `TestAModelledContextTakesItsToolsFromTheTable`,
reproduced here with base master in /opt/macports-master: a modelled
macOS 12 context read the Mac's CommandLineTools MacOSX.sdk and the
Mac's clang version.

Base's master runs port1.0's SDK and developer tool lookups in its own
interpreter, `portlib::configure`, aliased into each worker
(macports.tcl's `worker_init`), where 2.12 runs them in the worker. There,
their `file exists`, `glob`, and `xcrun` passed the dispatcher, and their
`file_exists_cache` and `sdkroot_cache` were the process's.

- **install_lookups** (toolchain.tcl), in a modelled context's worker on a
  Base whose `portconfigure::_get_sdkroot` and the rest are aliases, copies
  `file_exists`, `find_close_sdk`, `get_sdkroot`, `find_developer_tool`,
  `get_system_compiler_version`, and `configure_get_compiler` into the
  worker, with the values of Base's they read, the context's
  (`macports::override_vars`), and empty caches, and points each alias at
  its copy. The dispatcher then answers their questions from the context's
  row, as on 2.12, and their caches are the worker's. `get_tool_path` and
  `get_compiler_version` stay Base's, aliased, as 2.12 has them, answered
  from the caches the table fills: copying `get_compiler_version` showed
  it running `clang -v` against Xcode's developer directory, which 2.12
  also runs unobserved there.
- On 2.12, and in the Mac's own context, nothing changes.

The evaluator and macports tests pass against base master (with the
preview adapter) and against 2.12. `TestCompilerDependentFetchInputComesFromTheTable`
still skips its Darwin 9 half on master: the host read 2.12 sees there is
`get_clang_compilers` reading the tree's compiler list, which master also
runs in its parent, and which isn't a toolchain question; the skip and
`PreviewAdapter`'s comment now say so.
