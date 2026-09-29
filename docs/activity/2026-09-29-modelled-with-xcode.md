# 2026-09-29: an unstated context is modelled with Xcode

The person decided D2 on 2026-09-29: modelled contexts default to the Xcode profile, as MacPorts' builders are set up, which is what the model is meant to match. The oracle had settled on the tools profile on 2026-09-26, dockhand's base images', and the roadmap had kept the question open since.

Now a modelled context that doesn't state its developer tools is modelled with Xcode, from the facts table's Xcode row (`macports.Toolchain`): Xcode's version, its developer directory, and its SDKs and compilers. A context that states its tools is modelled with them, as before. A Tart release with its Xcode image states Xcode, and one without it states the Command Line Tools, so checks are as they were. Where the table has no Xcode row, the tools profile stands; every release the table has a tools row for has an Xcode row today, so that path isn't reached, and isn't tested.

What changes:
- **An evaluation for another release** whose tools aren't stated, the evaluator's own session for it included, now sees `xcodeversion`, `developer_dir`, and Xcode present, as MacPorts' builders do.
- **The index a host that isn't a Mac builds** is told the builders' tools; a Mac builds an index with its own tools, and is told only the platform, as before. A cache's generations were keyed by the platform's variables alone, though the indexer is told more off a Mac, so an index built under the tools profile would have been reused. Both now use what the indexer is told (`told`).

The oracle's design note says so.

Tests:
- `TestAnUnstatedContextIsModelledWithXcode`, in `macports`, and `TestAnUnstatedContextIsEvaluatedWithXcode`, in `eval`, through MacPorts' interpreter;
- `TestTheIndexerIsToldTheBuildersTools`, in `portindex`;
- the tests of the tools profile's own reading, which now state the Command Line Tools.

Five mutations each fail a test.
