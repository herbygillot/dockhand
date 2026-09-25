# 2026-09-25: a stealth update bumps the revision, and dist_subdir follows it

This revises the [stealth update](2026-09-25-update-stealth-byhand-submit.md) from earlier the same day, at the maintainer's suggestion.

## What changed

- **A stealth update bumps the revision by default.** The source changed, so what users built from the old archive may differ. `dist_subdir` is then `${name}/${version}_${revision}`: each stealth update's revision bump gives it a directory of its own, with no count to keep.
- **`checksums --no-revbump`** leaves the revision, for a re-rolled tarball whose contents didn't really change. It numbers the directory instead: `${name}/${version}_1`, the MacPorts guide's recipe. Following the revision without a bump would put the new archive in the old one's directory, so dockhand refuses that combination and says why.
- **A numbered `dist_subdir` keeps counting, even with a bump.** Switching `${version}_1` to `_${revision}` while bumping the revision to 1 would name the directory the earlier archive is already in.
- **A version update removes a stealth `dist_subdir`**, in either form, since the new version's archive has a name of its own. `update` says so. Any other `dist_subdir` stays.
- **The edit's subject** is "<port>: update checksums after a stealth update".
- **`portfile.BumpRevision`** now writes a new `revision` line right after `version`, aligned with it, as MacPorts writes it. Before, it went at the end of the file after a blank line. This applies to every revbump, `revbump` and `--revbump-dependents` included. Without a top-level `version` line, it still goes at the end.
- If the revision can't be bumped (it's calculated, or set inside a block), dockhand says so and numbers the directory.
