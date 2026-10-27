# A C++ standard upstream raised is said

2026-10-27, the docs dig's I20 (roadmap 27183215). MacPorts picks a
compiler from `compiler.cxx_standard`, so a raise the Portfile misses
builds on the host's release and fails on the older releases the
buildbots build: field testing raised i2pd 3.0.0's to 2020 by hand,
where its build/CMakeLists.txt probes `-std=c++23` and `-std=c++20`.

- **The floor** (project's `CMakeDocument.CXXFloor`): the lowest standard
  a CMake document names, in any branch, as `CMAKE_CXX_STANDARD`, a
  target's `CXX_STANDARD`, a `cxx_std_NN` feature, or a `-std=c++NN` or
  `gnu++NN` flag, with the file and line. A version's floor is the highest
  of its CMakeLists.txt files' floors (assess's `cxxFloor`), so a test
  directory asking for less doesn't hide what the build asks.
- **The finding** (`cxx-standard`, assess policy 26): where the new
  version's floor rose above the old version's and above the Portfile's
  `compiler.cxx_standard`, now among the options the evaluator reports,
  it says "upstream: build/CMakeLists.txt:5 asks for C++20, above the
  Portfile's compiler.cxx_standard 2017; MacPorts picks the compiler from
  it, so the older releases may need 2020". It holds nothing until its
  false positives are measured.

TestARaisedCXXStandardIsSaid (i2pd's shape: said, then nothing where the
Portfile is at 2020 or the floor didn't move) and TestACMakeDocumentsCXXFloor.
Raising Policy makes each assessment made under 25 again, a few fetches
each.
