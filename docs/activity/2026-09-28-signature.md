# 2026-09-28: the pull request's signature line

The person asked for the description's last line to read with **dockhand** in bold and its whole version in parentheses. It was `Submitted by [dockhand](https://github.com/herbygillot/dockhand) ver. v0.0.0-…`, and is now `Submitted by **[dockhand](https://github.com/herbygillot/dockhand)** (ver. v0.0.0-…)`, rendered as "Submitted by **dockhand** (ver. v0.0.0-…)". The link stays. A build that doesn't know its version signs with the bold link alone.

A pull request already open gets the new line the next time it's submitted: the line is part of what dockhand writes, and a description a person edited keeps their edits, as before.
