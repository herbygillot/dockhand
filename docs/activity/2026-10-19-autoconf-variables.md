# A configure.ac library named by a variable isn't removed

From the roadmap's Next (b4a2031f), GraphicsMagick's field notes:
1.3.45 to 1.3.49 held on about twenty libraries "removed", freetype and
zstd among them. 1.3.49 checks them as `AC_CHECK_LIB([$zstd], ...)`, with
`zstd=zstd` set above, and project/autoconf.go skipped a name it couldn't
read literally, so 1.3.49 read as 24 libraries against 1.3.45's 43.

- `ReadAutoconf` reads a `$name` or `${name}` library as each literal word
  the file sets `name=` to, on a line of its own.
- One the file never sets to a literal, as 1.3.49's `$pnglibbase` from
  sed's output, sets the new `AutoconfFacts.LibrariesIncomplete`.
- sourcecompare's `autoconfWords` says no library removed where the new
  list is incomplete, nor added where the old one is.
- 1.3.45 to 1.3.49 now reads: "--with-urwbase35otf-font-dir added;
  pkg-config module libsharpyuv added; pkg-config module libturbojpeg
  removed". The module change is real: 1.3.49 checks libjpeg instead.
- Test: `TestALibraryNamedByAVariableIsNotRemoved`, from the two releases'
  configure.ac, kept gzipped in internal/project/testdata.
