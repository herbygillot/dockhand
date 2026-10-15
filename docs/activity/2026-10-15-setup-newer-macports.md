# setup tart takes a guest MacPorts newer than it asked for (v0.3.1)

The v0.3.1 quick stage (2026-10-15): `dockhand setup tart` failed on the M1
after about six minutes' work. MacPorts 2.12.7 came out on 2026-10-13; setup
installs 2.12.6 (`macports.DefaultBaseVersion`), MacPorts' installer runs
selfupdate, which took the guest to 2.12.7, and setup rejected the image it
had just made as having the wrong version, with no way on in the message.
It's in v0.3.0 too. The person chose it for v0.3.1 (2026-10-15 18:47Z).

- `provision.baseAtLeast`: the three checks of a guest's MacPorts, a made
  image's, a found image's, and an upgraded one's, take the version asked for
  or newer. A found image with a newer base no longer asks for `--rebuild`;
  one older than asked still does, saying "or newer".
- TestAGuestMacPortsNewerThanAskedIsTaken. The setup protocol's pin moves:
  images made now hold what they did.

Made on release/v0.3, from 3892f092, for v0.3.1, and on main.
