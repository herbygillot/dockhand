# PyPA packaging's PEP 440 and PEP 508 test vectors

`vectors.json` holds test cases taken from [pypa/packaging](https://github.com/pypa/packaging)
26.3 (tag `26.3`, released 2026-08-04), the reference implementation of PEP 440 (versions and
specifiers) and PEP 508 (environment markers). `project`'s own readers are tested against them
(`conformance_test.go`) rather than replaced, since no Go library reads markers.

They come from `tests/test_version.py`, `tests/test_specifiers.py`, and `tests/test_markers.py`:

- `versions.ordered`: `VERSIONS`, which packaging's tests hold in ascending order;
- `versions.invalid`: `test_invalid_versions`' cases;
- `specifiers.contains`: `test_specifiers`' cases, which packaging asks with `prereleases=True`;
- `markers.valid` and `markers.invalid`: `test_parses_valid`'s and `test_parses_invalid`'s;
- `markers.evaluates`: `test_evaluates`' cases with an environment given; those evaluated in
  the running interpreter's are the host's, not the marker's, and are left out.

dockhand differs from packaging in two places, by design, which the tests say:

- local version labels are set aside in ordering, since what's asked is whether an installed
  version meets a requirement;
- ordering comparisons of strings that aren't versions are unknown, never a guess, where
  packaging's newer rules make them false; those cases (`TestOperatorEvaluation`) aren't taken.

To take a newer release's, check out its tag and run:

```sh
python3 extract.py /path/to/packaging/tests > vectors.json
```

packaging is available under either the Apache License 2.0 or the BSD 2-Clause License;
`LICENSE.BSD` is its BSD license, under which these cases are used.
