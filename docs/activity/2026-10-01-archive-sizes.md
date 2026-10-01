# 2026-10-01: archives of any size, read to 4 GiB

At the person's word, after the rust and cargo run found rustc's source past both of dockhand's archive limits, and the limits sweep listed them: "We should have no limit on download size (I do realize that's dangerous). Archive size limits should be around 4GB."

## What changed

- **A download has no size limit.** A distfile's download stopped at 512 MiB, and a forge's archive of a commit, for a Git-fetched port, at the same; rustc-1.99.0-src.tar.gz is 566 MB. Neither is bounded now (`archives.Client.MaxBytes` is none when unset, as it always is; `upstream.sourceArchiveLimit`).
- **A download is bounded by a stall, not its length.** A download also stopped after two minutes in all, which would bound its size as surely as a limit: at 5 MB a second, 600 MB. A download now goes on as long as data keeps arriving, and is given up after a minute with no byte, the wait for a response included: "no data arrived for 1m0s, so dockhand gave up on it" (`fetch.Stall`, which `fetch` owns beside its transport).
- **An archive is read to 4 GiB.** A walk of an archive read at most 1 GiB uncompressed; rustc's source holds 3.5 GiB. It reads 4 GiB now, and says the limit in the unit that reads naturally. A tar stream is read through to its end, since it has no index; a zip archive's members are read only as asked, and what's read of them now counts against the same limit, where nothing bounded a zip before. `diff --archive`'s extraction is bounded by it too.
- **`assess.Policy` is 9,** so what was "not compared" for these limits, rust's and rust-src's archives among them, is assessed again.

## Decided along the way

- **The forge's archive is a download too,** so it's unbounded with the distfiles.
- **Documents aren't downloads.** A livecheck page (16 MiB), a GitHub document (16 MiB), PyPI's JSON (8 MiB), and the mirror's PortIndex (128 MiB) keep their bounds: each is read whole into memory or parsed, and none has neared its bound.
- **No free-space check was added.** A download that fills the disk fails as a write does; whether dockhand should refuse one that, by its Content-Length, won't fit, is the person's to ask for.

## Verification

- `TestDownloadRejectsErrorBodiesAndSizeOverflow`: a stalled download said as one, and one that trickles in for longer than the bound in all, never that long without a byte, completes; `TestAWalkPastTheLimitSaysSo`, with a zip archive whose members count as they're read.
- The full suite with `DOCKHAND_TEST_MACPORTS_TCLSH`, vet, fmt-check, vendor-check, deadcode, and lint.
