# Captured manifests

Real output of `build.ManifestScript`, run by `/bin/sh` against a real MacPorts
installation and real Mach-O files. Nothing here was written by hand or
transcribed, and that is the whole point: every property the parser is held to
is a property of what `otool(1)` actually prints, and a fixture invented to
match the parser would agree with it forever.

| File | What it is | Source |
|---|---|---|
| `manifest-brotli.txt` | `brotli @1.2.0_0`, a real installed port | MacPorts 2.12.6 at `/opt/local`, macOS 26.6.2 arm64, `otool` from cctools-1040 (2026-09-03) |
| `manifest-universal.txt` | a scripted `port` over four files built for the purpose | same machine and same `otool`; the dylibs were built with `clang -arch arm64 -arch x86_64` |
| `manifest-universal-after.txt` | the same port at 3.0.0, so the pair is a before and an after | same |

What each one is here to pin:

**`manifest-brotli.txt`** — the ordinary case, and three traps inside it.

- Three files announce **one** install name: `libbrotlicommon.1.2.0.dylib`,
  `libbrotlicommon.1.dylib` and `libbrotlicommon.dylib` all say
  `/opt/local/lib/libbrotlicommon.1.dylib`. Keying a manifest by path reports
  three libraries where MacPorts installed one.
- The executable `/opt/local/bin/brotli` appears in the `id` section with a
  header and an **empty body**: `otool -D` on a program prints no install name.
  That emptiness is how a program is told from a library, and it is why `-D`
  and `-L` are joined rather than `-L`'s first line trusted — for a program
  that line is a dependency, not an id.
- `/opt/local/lib/libbrotlicommon.1.dylib` occurs both as a header (a file
  that was swept) and as a body line (the name three files announce). They
  differ by one colon, which is why headers are decided against the capture's
  own file list rather than by shape.

**`manifest-universal.txt`** — everything a universal file does.

- Two slices per file, always, because the script runs `otool -arch all`.
  Without it `otool` prints one section for a file whose slices include the
  host's exact subtype and a section per slice otherwise, so the same port
  would have a different number of libraries on two machines.
- `libwidget.mixed.dylib` is a genuine `lipo` of a 2.0.0 x86_64 slice onto a
  3.0.0 arm64 one: one path, two install names, two compatibility versions.
  Collapsing the slices would invent a measurement.
- `/tmp/dhfat/share/widget.pc` is answered on **stdout** with
  `is not an object file`, and `/tmp/dhfat/lib/gone.dylib` was listed by
  `port contents` and does not exist — so it is simply absent from both
  sections, `otool` complained on stderr, and the exit status was 1 while
  every good file still printed. A parser that read the exit status would
  throw away a complete manifest because one file vanished.

Regenerating: run the script `build.ManifestScript` produces against a prefix
whose `port` answers `-q installed` and `-q contents`, and copy the file it
writes. Update the table above with the machine and the date, since these are
captures and not goldens — a difference is new evidence, not a regression.
