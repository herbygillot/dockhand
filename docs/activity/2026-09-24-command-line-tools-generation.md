# 2026-09-24: setup installs one Command Line Tools generation per release

Step 1 of the roadmap's Next; decision 13 of the [contracts
direction](../reviews/2026-09-23-contracts-direction.md). Setup took
`softwareupdate --list | sort | tail -1`, the newest tools offered, which
gave both Tahoe images the macOS 27 tools (clang 2100.3.34.2) while
MacPorts' 26 builders run 26.6.

- `macos.Release.Tools` records the generation, the major version, per
  release: 14 for Monterey and Ventura, 16 for Sonoma and Sequoia, 26
  for Tahoe, 27 for Golden Gate. It follows MacPorts' GitHub CI pin where
  CI covers the release (Xcode 16.2 on 14, 26.4 on 26) and the arm64
  buildbots elsewhere, read by hand on 2026-09-23; the facts table (step
  4) will hold it.
- `macos.EnsureCommandLineTools` lists the offers with the marker that
  makes Software Update offer the tools, and `CommandLineToolsLabel`
  picks the newest of the release's generation, comparing versions
  numerically and reading both label forms ("… for Xcode 27.0-27.0",
  "… for Xcode-14.2"). With none of the generation offered it refuses,
  naming what is offered, and never falls back to the newest. The marker
  is removed however the install ends.
- Tools already in a guest are held to the generation too, read from the
  `com.apple.pkg.CLTools_Executables` receipt
  (`macos.CommandLineToolsVersion`): a guest with another generation is
  refused rather than used.
- Validation reads the installed tools on every image. A build whose
  tools are not the release's generation is refused before adoption;
  `setup --check` reports an existing image's mismatch with "rerun with
  --rebuild". Setup's summary and JSON name the tools
  (`command_line_tools`).

Against the real images, `setup --check --os monterey` passes (Command
Line Tools 14.2), and `setup --check --os tahoe` reports
"dockhand-base-tahoe has Command Line Tools 27.0; Tahoe uses generation
26; rerun with --rebuild". The Tahoe images are not rebuilt in place:
dockhand's base, Xcode, and golden images are not modified outside a
person's own setup run, and step 2 builds images into dockhand's own
Tart home, where the Tahoe ones are made on the 26 generation.
