# README look-over after the day's changes

A read of the README against the code found three statements the day had overtaken. "Dockhand refuses to publish anything it has not seen pass" and the matching sentence under standalone publish predate `--skip-verify`; both now say what the flag does and that the pull request discloses it. "There are no binary releases yet" is still true, but a MacPorts port is under review, so the requirement now points at the pull request and notes that the vendored dependencies make the source build need no network. The build section also says where `--version` comes from: the nearest Git tag, or `make VERSION=...` outside a checkout.

The rest was checked and stands: the image releases Monterey through Tahoe, the seven-day log retention, the fork workflow's `main.yml`, the `--os` names, the exit codes, the `review` command's unimplemented state, and the intake refusal pointing at `--no-publish`.
