# Checksum values in tables

The queue's next item after the corpus replay: 517 qt5 and qt6 subports and a handful of others were refused with "checksum value has no unique literal owner", because their `checksums` declaration reads its digests from somewhere else.

## The shapes

Two, in the tree. pcre keeps arrays, `set sha256(pcre) …` per subport, and declares `checksums sha256 $sha256(${subport})`. The qt5 and qt6 Portfiles keep one `array set modules { qtbase { { rmd sha size } … } … }` table and, in a `foreach` over it, declare `checksums rmd160 [lindex [lindex ${module_info} 0] 0] …` inside each subport. In both, the literal that must change is not in the declaration.

## The rule

The declaration's words were already required to be literals equal to the evaluated values. A value word that is not a literal is now traced: the evaluated value, a hex digest or a decimal size, is searched for as a whole token across the Portfile, bounded by whitespace, braces, quotes, or a line continuation and outside comment lines. Exactly one occurrence is its owner, and the binding's token points there; the existing check that the evaluated values equal the declared ones still holds, since the table feeds the declaration. Two occurrences, or none, leave the port unsupported with the same message as before. The message was already worded for this rule; the code had not implemented it.

A traced group is edited where its values are written and is never rewritten whole: the legacy-block modernization keeps its hands off it, since a rewrite of the declaration would not touch the table.

## Obsolete contexts

With the binding working, qt6 stopped one step later: on Darwin 18 to 22 the qt6 Portfile loads the obsolete PortGroup and names `qt64-` or `qt67-` successors, so those modeled contexts have no distfiles at all and were reported as "no source archives". A port that evaluates as an obsolete follower in a context, `replaced_by` set and nothing fetched, now adds no archive requirement there, in assessment, in the archive plan, and in the checksum refresh plan. That is the obsolete-follower rule from earlier in the day, applied per context.

## A pool bug the population caught

Re-assessing the 562 qt entries through the new `assess` pool produced 514 evaluation failures, `bad index "qqq-6"`, that no single assessment reproduced. The probe marker is dockhand's own: version probing writes candidate contents into the target Portfile and restores it, and every qt subport shares one Portfile, so eight subports probing at once read one another's candidates. The pool's premise, that the shared snapshot is read-only, holds for distinct Portfiles and not for ports of the same one. Ports are now grouped by Portfile: distinct Portfiles overlap, subports of one are assessed one after another. Explicit names are mapped to their Portfile through the index, which `assess` now stages for explicit selections as it already did for indexed ones; the resolver needed it a moment later in any case. The `-v` output says how many ports and Portfiles a run covers.

## Exercise

`assess pcre pcre2 qt5-qtbase qt6-qtbase qt6-qtdeclarative qt69-qtbase`: all ready, checksums associated. `refresh-checksums pcre --diff` and `refresh-checksums qt6-qtbase --diff` downloaded the archives, found the table digests current, and reported nothing to commit; the qt6 one read a 50 MB tarball through the 1,500-line Portfile's table. Re-assessing the same 562 entries the deployment-target sink had left at 23 input-found: 541 input-found, 15 unsupported, 6 unknown. The 21 left are the 11 qt metaports with no archive of their own, five host-dependent evaluations, two remote patchfiles, a post-fetch hook, TeXShop's `${distname}`-named group, and one declaration no context covers; none is a table. Against the survey's index, that is 1.2% of all rows moved by this item, on top of the 3.5% the checksum modernization moved. The run took 31 minutes, since the subports of one Portfile are assessed one after another.

A qt bump is still not a one-command affair: the same table carries each module's revision, read through `lindex`, and the revision reset of a bump does not trace through tables yet. That is the next boundary for qt, and a smaller one.

## Tests

`distfiles`: an array-element port and an lindex-table port both bind with traced spans on the digests and size, a comment does not count as an occurrence, and a second literal occurrence makes the value unowned. `portedit`: a refresh on a table port rewrites the table's three values in place and leaves the declaration reading it.
