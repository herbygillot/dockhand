-- Q9: the facts table's toolchain against the guest's (ATTACH 'guest.db'
-- AS guest): the guest's host context is its own, so where its developer
-- tools differ from the table's row for its Darwin and architecture, the
-- table has drifted.
SELECT t.darwin, t.arch, t.profile, t.xcode, g.xcode AS guest_xcode, t.tools, g.tools AS guest_tools, t.sdks, g.sdks AS guest_sdks
FROM main.toolchain t JOIN guest.toolchain g ON g.darwin = t.darwin AND g.arch = t.arch AND g.profile = t.profile
WHERE t.xcode IS NOT g.xcode OR t.tools IS NOT g.tools OR t.sdks IS NOT g.sdks OR t.developer_dir IS NOT g.developer_dir;
