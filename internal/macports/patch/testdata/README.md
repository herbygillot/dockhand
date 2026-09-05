# Real patchfiles

Two patchfiles copied unmodified from the BSD-3-Clause `macports-ports` tree
at commit `ed5f9363801a03d1db72bdaa5734f6db91f757a6` (2026-09-04), existing
solely as test input. The tests build the files each patch is applied to;
the patches themselves are exactly what a maintainer committed, which is
the point: every byte the parser must keep — a tab before a timestamp, a
blank context line spelled as a single space, a backslash-continued shell
line — is a byte a real patch has.

| File | Port | What it pins |
|---|---|---|
| `patch-libraw-no-libstdcxx.diff` | `graphics/libraw` | Three files in one patch, one hunk each, so a relocation moving one hunk must leave the other two — and every byte between them — alone. The Makefile hunk carries tab-indented recipe lines and blank context lines. |
| `no-fink.patch` | `devel/nettle` | A comment line above the first `---` header, which the refreshed patch must begin with byte-for-byte. The hunk removes more than it adds (`-142,9 +142,7`), so the new side's count differs from the old side's. |

Regenerating: copy the files again from the tree and update the commit above.
They are captures, not goldens — if a later commit changes one, the test's
transcribed before-blocks and expected line numbers change with it.
