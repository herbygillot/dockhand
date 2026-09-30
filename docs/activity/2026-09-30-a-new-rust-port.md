# 2026-09-30: batch 16, a new Rust port, and checksums of vendored sources

Batch 16 is the peer's `create` run on txt ([review](../reviews/2026-09-28-hugo-bump-exercise.md#create-for-txt)), widened at the person's word: HTTP URLs said, and MacPorts' license names as the Guide and the tree have them.

## Checksums of a port with vendored sources (finding 1)

`dockhand checksums` refused every port whose Portfile declares its crates or Go modules (`cargo.crates`, `cargo.crates_github`, `go.vendors`). The refusal was "fetch customization or vendored source requires a dedicated preparer", from `archives.CheckPolicy`. The refresh bound the Portfile's archives as written, and those declarations make MacPorts fetch archives whose checksums dockhand doesn't check. So `create` left txt's checksums as zeros, and its advice, `dockhand checksums txt`, met the same refusal. So would the refresh of any existing Rust or Go-vendored port.

`update` already had the step it lacked (`dependencyBase`): set the declarations aside, compute the source archive's checksums, and put them back. The refresh now does the same:
- It refreshes the Portfile with the declarations set aside, through the same machinery as any refresh. That covers every context and variant, the fidelity checks, and a family's shared checksums.
- `dependency.Plan.Apply` puts the declarations back byte for byte. Setting them aside leaves each command where it was, empty, so they go back where they were. A probe of five layouts confirmed it: comments around the block, blank lines, a block at the end with no newline, a crates block beside a Git crates one, and extra spacing.
- The port is then evaluated as a whole. It must read its refreshed checksums followed by the ones its declarations append, as it read the old ones, with nothing else changed.
- A port whose declarations' checksums come before its own can't have the two told apart, and is refused. Both PortGroups append theirs.

The crates' checksums are Cargo.lock's, or go.sum's, and none of them is fetched.

A live run against the real tree, in a scratch clone: alacritty (277 crates) and lazysql (`go.vendors`) both refresh. Before, both were refused. With one of alacritty's own checksums zeroed, `checksums alacritty` restored its Portfile exactly as it was.
