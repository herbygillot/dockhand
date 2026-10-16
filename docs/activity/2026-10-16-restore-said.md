# A deleted base image's refusal says it's a restore (D-R3)

The rc9 full stage's D-R3: with dockhand-base-golden-gate gone and its
golden copy kept, check said "dockhand setup tart golden-gate makes
dockhand-base-golden-gate", which reads as an hour and 60 GB, where setup
restores it from the golden copy in about 70 seconds.

The person decided (2026-10-16) that check refuses rather than restoring
the image itself, which would write tens of gigabytes unasked. The Tart
provider's `Environments` now says the restore where the golden copy is
listed: "no Tart image for macOS 15 (Sequoia): dockhand setup tart sequoia
restores dockhand-base-sequoia from dockhand-golden-sequoia, in about a
minute"; without one, it says "makes" as before. This was v0.4.0 part 1's
last item.
