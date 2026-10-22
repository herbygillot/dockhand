# L2a: an archive the new version no longer fetches, said and held

Roadmap part 2, item 1, and the README's known issue, which goes. The
assessment set an archive the candidate no longer fetches apart, holding
nothing, and update said nothing of it, though the Portfile keeps the old
checksum lines and the port may have used what it held, a vendored crate
it dropped.

The `source-removed` finding now holds, saying "upstream: X is no longer
fetched; look at what the port used of it, and at its checksum lines in
the Portfile", which update's comparison lines, submit's preview, and
serve's holds say as any hold. Assessment policy 25, so assessments made
before are made again.

The update path never meets it: an update whose new version fetches a
different set of archives is refused as a change of their shape ("candidate
changed archive/checksum structure"), before any are paired, so the
archive no longer fetched arrives only where a person changed the
Portfile, whose revision's assessment now holds on it. A test pins the
refusal (`TestAnUpdateThatDropsAnArchiveIsRefused`).
