# An option that reads the build directory is the same in each evaluation

Field testing at 30eaf4cc: `bump pyinfra` (3.6 to 3.10.0) was refused,
"pyinfra.build.pre_args changed", though sysutils/pyinfra/Portfile never
sets build.pre_args, and the update by hand was the version and its
checksums alone.

- **Cause:** Base's build.pre_args defaults to the build target, and the
  python PortGroup's for a PEP 517 port is `--wheel --outdir
  [shellescape ${workpath}]`. The work path is named for where the port
  was evaluated, and an update evaluates the port before and after in
  directories of its own, so the strict fidelity check saw build.pre_args
  move. The code checksum already wrote the build directory as `<build>`;
  the options read didn't.
- **Fix** (eval/evaluator.tcl): every option read is written with the
  port's build directory as `<build>`, but cargo.dir and patch.dir, which
  are read against worksrcpath just after and keep their paths. The one
  reader of build.pre_args, depblock's online-resolution test, looks for
  cargo flags, not paths.
- TestAnOptionReadingTheBuildDirectoryIsTheSameInEachEvaluation, with
  `build.pre_args --wheel --outdir ${workpath}`; against the evaluator
  before, it fails as pyinfra did: "fixture.build.pre_args changed".

The refusal's fallback named a variable the person never wrote; with the
cause gone, a version-only python update prepares itself again. A
refusal that names a derived option is left as it is: the option did
move, and saying which one is what lets a person find it.
