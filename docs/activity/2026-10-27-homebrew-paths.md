# Literal Homebrew library paths an update adds are said

2026-10-27, field testing's outdated-ports block (roadmap 76f07f2c), at the
person's "OK, go ahead and add the `dlopen` scan as well.", read as the
source comparison's version since built binaries are ruled out. hashcat
8.0's new src/ext_zlib.c, ext_zstd.c, ext_lzma.c, ext_iconv.c, and
ext_openssl.c dlopen lists like "/opt/homebrew/lib/libz.1.dylib" and
"/usr/local/lib/libz.1.dylib": its check passed and its Links were clean,
and a MacPorts hashcat would lose compression and crypto at run time.

- The archive reading reads C, C++, Objective-C, Swift, Go, and Rust
  source files, by extension, for string literals under /opt/homebrew/,
  /usr/local/lib/, or /usr/local/opt/ (project.Reading's `HostPaths`), a
  quick byte check first so most files cost nothing more.
- The source comparison says each new-version file whose literals the old
  version's source has nowhere, a notice, holding nothing: "upstream:
  src/ext_zlib.c adds /opt/homebrew/lib/libz.1.dylib and
  /usr/local/lib/libz.1.dylib, so it may look for libraries at run time
  only outside ${prefix}" (sourcecompare's `hostPaths`, kind `host-path`).
  Build files, as a Makefile's `-L /usr/local/lib`, and documentation
  aren't read for it, and no built binary is scanned. Assess policy 28.

TestHomebrewLibraryPathsAnUpdateAddsAreSaid: four new files named,
ext_iconv.c's, which the old version had, and the Makefile's say nothing.
