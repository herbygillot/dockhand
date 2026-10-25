# The corpus's rest

2026-10-25, first after v0.5.0's tag, at the person's "The 0.5.0 PR is
merged. Please have implementation continue forwards with the roadmap."
The corpus entry's "After v0.5.0's tag" half: the other shapes'
exemplars, the CI job, and the PortGroup stand-ins the corpus replaces.

## The other shapes' exemplars

Five rows join `testdata/corpus/rows.tsv`, refreshed from macports-ports
with `_resources` kept at ee76ef2c:

| Row | Bump | Shape | Outcome |
| --- | --- | --- | --- |
| terraform-1.16 | 2d65c4acf9c, 1.16.6 | a family of 24 subports, the obsolete `terraform` stub moving with its series, fetching nothing | plans |
| py-lmdb | dceb0abff5a, 1.7.5 | a PyPI port of the python PortGroup's subports | plans |
| qemu | 48ac9de2ce3, 11.1.2 | a large port, its livecheck a regex against its own master_sites | plans |
| perl5.40 | b60062b118b, 5.40.4 | a large subport family made in a loop | refuses: no editable version input established |
| postgresql18-server | ed2c0d83c61, 18.6 | a port that fetches nothing, its version alone | plans |

perl5's series each take their version, revision, and checksums from a
row of one table, `perl5.versions_info`, which update doesn't edit: the
refusal says to edit it by hand, as before. It's a row that refuses, not a
fix, by the entry's rule for what changes nothing dockhand does.

The whole corpus, 19 rows, matches its bumps in 4 minutes 18 seconds.

## CI

ci.yml gains a `corpus` job on macos-latest: MacPorts as the tests install
it, then go2port and cargo2port from MacPorts' own tree and archives
(the tests' nosync source doesn't reach them), then `make test-corpus`
with the workflow's token for GitHub's API.

## The stand-ins retired

editprep's dependency tests modelled two PortGroup behaviours with
stand-ins, each now a corpus row against the real PortGroup: golang 1.0
turning `extract.rename` off for go.vendors (`fixture_extract`, 52d14a6c;
git-credential-oauth's row), and cargo_fetch 1.0's `dist_subdir` (dust's
row). The scenarios go. The `fixture_vendors` and `fixture_crates` procs
stay: they drive the tests' overrides, patches, and failures, which no
bump in MacPorts' history shows.
