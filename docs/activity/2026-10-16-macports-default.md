# An image's MacPorts, and one note of it

Field testing, 2026-10-16: `setup tart --macports-version` defaulted to
2.12.6 (`macports.DefaultBaseVersion`, through `tart.DefaultMacPorts` and
the provisioner's fallback), and nothing read this Mac's MacPorts, so the
stale-image hint's own `setup tart <name> --rebuild` made the image again
with 2.12.6, and the warning never cleared.

- An image made now, new or `--rebuild`, installs this Mac's MacPorts where
  it's newer than 2.12.6 (`Provider.newMacPorts`, on `HostMacPorts`); 2.12.6
  stays the floor, and `--macports-version` still names another. An image
  that exists is checked against the floor, as before, so `setup tart` on
  a stale one says it's stale rather than refusing it.
- check's plan note (`engine.macPortsNotes`) says the environments the
  check builds on, those with something in their build order, in one line
  for each pair of versions, with each one's rebuild: it said a line an
  environment, minmax's unbuilt ones too, six for a lazygit check.
- setup's report says its stale images the same way, in one clause.
- Both name an Xcode image's Xcode, from the release's pin, where they said
  `--xcode <Xcode>`: `--xcode <folder> (Xcode 26.0's .xip, or a folder
  xcodes downloads it into)`, since `--xcode` takes a .xip or a folder.
