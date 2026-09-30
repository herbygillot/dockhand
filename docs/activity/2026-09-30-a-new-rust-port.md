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

## A new port's license and line, from its manifest (findings 3 and 5)

GitHub said NOASSERTION of txt's two license files, so `create` wrote `license unknown`. Yet txt's Cargo.toml declares `MIT OR Apache-2.0`. GitHub's description was a pitch, where Cargo.toml's is a line in MacPorts' style.

- `newport.Declare` reads a project's own license and description: Cargo.toml's `[package]`, or pyproject.toml's `[project]`. What isn't a plain string is left out, such as a license a Cargo workspace inherits, or a PEP 621 license table.
- `macports.License` now owns MacPorts' names; the map moved from `newport`, since a manifest's license is its second reader. It reads an SPDX expression as the Guide's `license` keyword has it:
  - `OR` is a braced choice, `{MIT Apache-2}`; Cargo's old `/` is one too;
  - `AND` is licenses side by side;
  - parentheses are allowed around a term or a choice.

  It says nothing, rather than guess, of the rest: a license it has no name for, an exception (`WITH`), and a choice among licenses that apply together, which a braced list can't say.
- The map's one wrong entry is fixed. `CC0-1.0` was `CC0-1`, which `port lint` refuses (a name can't end in a digit) and no port uses. It and `Unlicense` are now `public-domain`, as 446 ports say.
- `create` decides these once, as it observes the project:
  - the license comes from the manifest where MacPorts has a name for it, else from GitHub;
  - the description is the manifest's line, else GitHub's.

  The header says which it used: "Cargo.toml says MIT OR Apache-2.0". The Portfile marks it, "from Cargo.toml's license field", and so does the unconfirmed list, "license (from Cargo.toml)".

## The category's guess said with its value (finding 4)

The unconfirmed list said only "category". Now it says what the guess chose, "category devel (guessed from the build system; --category chooses)", since the category picks the directory.

## A destroot for a Cargo or Go port (finding 2)

Neither the cargo nor the golang PortGroup installs anything, and `create` wrote no destroot, so a new Rust or Go port installed nothing. It now writes one, marked unconfirmed, in the form the tree's ports use:
- **Cargo:** `xinstall -m 0755` of `${worksrcpath}/target/[cargo.rust_platform]/release/<bin>`, for each of Cargo.toml's `[[bin]]` targets, else its package.
- **Go:** `xinstall -m 0755 ${worksrcpath}/<bin>`. `<bin>` is the program `go build` makes at go.mod's module: its path's last element, less a major version's `/vN` (`dependency.GoBinary`).

A program named for the port is `${name}`. Where the manifest names none, as a Cargo workspace's root doesn't, the Portfile marks it rather than guess. `destroot` joins the unconfirmed list for both.

## A Cargo or Go port's checksums name their file (found as this batch ran)

Linting the txt Portfile `create` made showed `Error: invalid checksum field: adler2-2.0.1.crate`. A port whose crates or modules append their checksums has several distfiles, so its own must name their file. 358 Cargo ports and all 50 sampled `go.vendors` ports write `checksums ${distname}${extract.suffix} \`, and `newport` wrote an unnamed group. It now names the file for Cargo and Go ports. The live txt Portfile then lints with no errors. Its 750 warnings are the crates' missing rmd160 and size, as alacritty's are: the Cargo PortGroup gives crates sha256 alone.

## HTTP URLs said (finding 6, widened)

MacPorts prefers HTTPS.
- `macports.PortInfo.PlainHTTP` names a port's plain-HTTP homepage, and its `master_sites` that are URLs, with their tags taken off by Base's own `tagged_url_re`. A mirror group, `gnu` or `sourceforge:project`, is MacPorts' own list, and none of the port's.
- The engine asks each one's `https://` form: HEAD, then a one-byte GET for a server that refuses HEAD, within ten seconds, with an answer below 400 counting.
- `update` and `checksums` say them, with the answers ("MacPorts prefers HTTPS; over plain HTTP: …"), as does `--json` (`plain_http`). They change nothing, since changing them is the maintainer's call.
- `create` writes GitHub's `http://` homepage as its `https://` form where that answers, and says so. Where it doesn't, the homepage is written as given and said.
- Tests never ask the network: the engine's and the command's test worlds stand in for the probe.

## Live, against the real project

`create https://github.com/ErikHellman/txt --new`, in a scratch clone of the ports tree, printed:

```
txt 0.8.1 · Rust (Cargo.toml) · Cargo.toml says MIT OR Apache-2.0 · "A fast, intuitive terminal text editor"
Created devel/txt/Portfile from the github and cargo PortGroups
  cargo.crates: 374 crates, from Cargo.lock
  checksums: 1 distfile + 374 crates
  homepage: over HTTPS, as MacPorts prefers; GitHub gives http://txt.hellman.io/
  Unconfirmed, marked in the file: category devel (guessed from the build system; --category chooses), license (from Cargo.toml), long_description, maintainers, destroot
```

It wrote `license {MIT Apache-2}` and `homepage https://txt.hellman.io/`, filled in the checksums, and wrote the destroot. Each of the run's six findings is answered.

## Tests

- `TestAPortWithCratesRefreshesItsOwnChecksums` runs against MacPorts.
- `TestALicenseExpressionInMacPortsWords` and `TestAProjectsManifestSaysItsLicenseAndItsLine` cover the license and the manifest.
- `TestANewPortInstallsWhatItsManifestNames` covers the destroot.
- `TestAPortsPlainHTTPURLs`, `TestAnUpdateSaysThePortsPlainHTTPURLs`, and `TestPlainHTTPURLsAreSaid` cover the HTTP notice.
- `TestCreateTakesTheManifestsLicenseAndLine` covers txt's shape: a license, a line, a category, and an https homepage where it answers and where it doesn't.

Mutations were run on the checksum refresh and the license expressions. Each survivor was either killed by a new case, such as `(MIT) OR (ISC)` for parentheses around a choice, or removed as equivalent: a guard that restated `Apply`'s own guarantee.
