# Livecheck documents

Real documents a Portfile's livecheck reads, kept as Base's curl fetch
received them, for the differential test that runs Base's own matching loop
and dockhand's mirror of it over the same bytes.

- `flyctl-latest.json`: `https://api.github.com/repos/superfly/flyctl/releases/latest`, fetched 2026-09-21 through Pextlib's `curl fetch`; devel/flyctl matches `"tag_name": "v(\d+(?:\.\d+)+)"`.
- `cmake-tags.json`: `https://api.github.com/repos/Kitware/CMake/tags?per_page=30`, fetched 2026-09-21 as an authenticated API client sees it; devel/cmake matches `"name": "v(3\.[0-9.]+)"`, which no longer matches once the tags are all 4.x.
- `myloss-info.plist`: `https://raw.githubusercontent.com/balp/MyLoss/master/MyLoss-Info.plist`, fetched 2026-09-21; aqua/MyLoss matches it whole with regexm.
- `overlap.txt`: synthetic; matches that share their last character, and a match that ends where it began, which Base's loop never leaves.
