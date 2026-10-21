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
