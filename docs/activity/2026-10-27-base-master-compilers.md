# A modelled context's compiler list on MacPorts base's master

2026-10-27, after I3 (roadmap d2ce517d), the gap 33251823 left. Base's
master also runs `get_clang_compilers` in its own interpreter, aliased
into each worker: it read the tree's port1.0/compilers/clang_compilers.tcl
unobserved, and cached the list by that file alone, so a second context
one process observes got the first's.

- `get_clang_compilers` joins the lookups `install_lookups` copies into a
  modelled worker (toolchain.tcl), with `macports::os_platform` among the
  values it carries, `getportresourcepath` aliased to Base's, and the
  list's cache the worker's. Its `file exists` of the list is observed in
  the worker, as 2.12's is.
- install_lookups now runs in every modelled context, not only one the
  facts table has tools for: Darwin 9 has no row, so its lookups had
  stayed in Base's parent, and its compiler check of /usr/bin/clang++,
  which 2.12 sees as a host read, went unseen.

`TestCompilerDependentFetchInputComesFromTheTable` runs its Darwin 9 half
on master unskipped and passes; `TestEachContextGetsItsOwnCompilerList`
observes Darwin 25 and then Darwin 16 in one process and gets each its
own list, as a process of its own does, which failed on master without
the copy. The evaluator and macports tests pass on master and on 2.12.
