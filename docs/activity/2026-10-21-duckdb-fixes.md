# The duckdb bump's two fixes

Field testing's duckdb 1.5.6 to 2.0.0, at 9bdc25ca, placed in b1a0b8af.

## --follow-moves on a branch already at the version

`update duckdb` said upstream had transferred from cwida to duckdb.
`update duckdb --follow-moves` in that branch then changed nothing,
because the move is looked for only where the update changes files
(engine/update.go). With `--follow-moves`, an update that finds the port
at its release now still asks GitHub, and points github.setup at the move
in the Portfile as the working files have it. That's an edit of its own,
"duckdb: follow the move to duckdb/duckdb", which compares no archives,
since the version's were compared already. Its output says "Updated
github.setup to …" and the file changed, not "Updated version." The
submit plan's fresh assessment then holds no longer on the transfer.

Replayed in scratch: `update duckdb --new`, then `update duckdb
--follow-moves -b duckdb-2.0.0`, left `github.setup duckdb duckdb 2.0.0
v`. After `tidy`, `submit --plan --no-check` held only on the
CMakeLists.txt change, not the transfer. README and the v0.4.0 notes
list it as a known issue.

## A removed option the Portfile sets, said first

duckdb 2.0.0 dropped ENABLE_EXTENSION_AUTOLOADING and
ENABLE_EXTENSION_AUTOINSTALL, which the Portfile passes as `-D…=1`, and
they fell into "and 7 more" behind the options added. `Compare`
(sourcecompare) takes `sets`, whether a Portfile that was read names an
option, beside `named`, which answers yes to everything where none was.
`cmakeWords` puts first, and never counts away, "option X, which the
Portfile sets, was removed", and a default of one it sets that moves.
`autoconfWords` does the same for a `--enable-` or `--with-` switch the
Portfile writes as `--enable-`/`--disable-` or `--with-`/`--without-`.
Where no Portfile was read, nothing is called the Portfile's.

Replayed in scratch: duckdb 1.5.6 to 2.0.0 now reads "CMakeLists.txt
changed: option ENABLE_EXTENSION_AUTOINSTALL, which the Portfile sets,
was removed; option ENABLE_EXTENSION_AUTOLOADING, which the Portfile
sets, was removed; option COVERAGE added, …".

## An added option says what it gates (72858428)

Reproduced on duckdb 1.5.6's and 2.0.0's sources. The gate is in
extension/extension_build_tools.cmake, which CMakeLists.txt include()s,
and which the comparison already read. In 2.0.0, `if(LINK_CORE_EXTENSIONS)
duckdb_extension_statically_link(${EXT})` sits in `foreach(EXT IN LISTS
BUILD_EXTENSIONS)`. In 1.5.6 no command linked an extension:
register_extension linked each one unless DONT_LINK was set. So nothing
the default build reached before is gone, and a rule of what the default
build reaches couldn't say it. Prioritization chose to name what an added
option gates instead, and claim nothing of what the build lost.

- `CMakeStatement.Loops` are the foreach() loops a statement is in, each
  by the names its list reads (`cmakeLoop`). They're kept apart from
  `Under`, where the placement had them join it. `Under` writes "lines
  change under if(…)", and a loop there would read as an if(). An include()
  inside a loop reads within it.
- `cmakeGated` (sourcecompare) names the commands an added option's if()s
  gate, by command and file, the first two and a count, as "gating
  duckdb_extension_statically_link in
  extension/extension_build_tools.cmake". It leaves out set(), list(),
  string(), message(), the blocks' own heads, and find_package. find_package
  is said with the packages already, under its option. A condition of NOT
  the option, or an else() of it, isn't its gate. Where a gated command is
  in a loop over a list the Portfile sets, the added option's line leads
  as the Portfile's, as removed options do since ee011731.

Replayed in scratch. duckdb's loop names BUILD_EXTENSIONS directly, which
its Portfile sets, so its line leads: "CMakeLists.txt changed: option
LINK_CORE_EXTENSIONS added, off by default, gating
duckdb_extension_statically_link in extension/extension_build_tools.cmake;
option ENABLE_EXTENSION_AUTOINSTALL, which the Portfile sets, was removed;
…". fluent-bit 5.1.2 to 5.1.3, from a tree at a1563aa1e88^, still reads
"find_package(Protobuf) added, under FLB_PROTOBUF_ENCODER", its option now
"gating add_subdirectory, flb_definition, and 1 more". Holds are as they
were.
