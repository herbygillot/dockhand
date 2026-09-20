# The pull request's environment, as a table

The "Tested on" section listed the build environment as a run of one-line paragraphs: macOS and build and architecture on one, the developer tools on another, MacPorts on a third, then provider, provider version and image joined by semicolons on a fourth. Those are facts with values, and a reader comparing two pull requests reads down a column rather than through prose.

They are a two-column table now, one row per fact, with an empty header because each row names itself and a column heading would only repeat that. The provider keeps its own line and the facts that belong to it, its version, its image, and the identity of the environment it built in, hang off it as bullets; the GitHub path already bulleted its matrix jobs and now bullets the run alongside them. Two redundancies went: an Xcode row no longer repeats the word Xcode, the way MacPorts already dropped its "Version: " prefix, and a guest that recorded no macOS gets no row at all rather than a row reading "not recorded" three times. The `dockhand` link in the opening line is bold.

The table gained a row the section never had: which dockhand ran the verification. It is recorded on the evidence when the observation is judged, not read when a body is written, because the build that publishes need not be the one that verified; a job resumed after an upgrade would otherwise name the wrong one. Evidence from before this carries none and its row is simply absent, which is the same rule every other row follows.

A workflow observation establishes no runner tool versions, and says so, so its table carries only that row. Evidence with nothing to table writes no table.
