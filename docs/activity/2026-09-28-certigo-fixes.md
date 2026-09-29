# 2026-09-28: the certigo run's fixes

The hugo exercise's certigo run ([review](../reviews/2026-09-28-hugo-bump-exercise.md#certigo-with-a-hands-on-binary-test); verified in the roadmap's Reviews). Findings 2 to 7, each in its own commit. Finding 1, running what a port installs, is under the roadmap's Later.

## A subject kept beside a person's edit (finding 2)

certigo's update was still uncommitted when the person fixed its version string by hand, in the same Portfile. tidy then proposed "(needs a subject)", with a ✗, though dockhand had recorded "certigo: update to 1.18.1" for that update. The recorded subject is used only while the port's changes are exactly dockhand's edits, one after another. A person's change breaks that, and there was no commit to take a subject from, since the update was uncommitted. Deriving one from the change to the Portfile found no `version` line, since certigo's version is in `go.setup`.

Now, where no commit gives the subject, dockhand's edits of the directory give it while every line they wrote still stands in the final files. It's noted, "subject from dockhand's edits, which the other changes leave standing", beside "has changes dockhand's commands did not make; review them", so the proposal is reviewed, and it carries no Generated-By. What names the edits is what names them when they stand alone (`chainSubject`): a new port's subject, else an update's, whatever edits followed.
- Lines dockhand didn't write are the person's to change.
- Once a line it wrote is gone, as when the person takes the version back, the subject is theirs to give.

That is a comparison of lines, not a reading of the Portfile. Reading a declared version, `go.setup`'s included, is still the private-helper review's finding 2, inside item 6, where tidy's other readings of a Portfile move.

`TestAPersonsEditBesideAnUpdateKeepsItsSubject` covers the kept subject and its note, a line dockhand didn't write, and a version taken back. Its chain is an update followed by a revision bump, whose subjects differ. The existing tidy test's update and checksum refresh record the same subject, so it hadn't pinned which edit names the chain. Five mutations each fail a test.
