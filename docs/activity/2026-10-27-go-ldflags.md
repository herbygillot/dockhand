# A Go port's -X target is still declared

2026-10-27, the docs dig's I18 (roadmap 79d20803). Go's linker ignores
`-X` for a symbol that isn't there, so a port's version string goes
silently wrong when upstream moves or drops the variable: termshot,
dblab, atlas, and mage built and passed lint reporting the wrong version,
and mp4ff moved its version variable to another package.

- `macports.PortInfo.GoLinkTargets` reads the `-X importpath.name=`
  arguments in the evaluated `build.pre_args` and `build.args`, the
  latter now among the options the evaluator reports.
- The archive reading keeps each named package's Go files, not tests'
  (project.Spec's `GoPackages`, from `ReadingSpec`): a directory whose
  path ends the import path, and the top for package main.
- The assessment (assess's `goLinkTargets`, rule `go-ldflags-target`,
  policy 27) finds the package's directory below the module go.mod names
  and parses its files (`go/parser`; nothing is run). Where they declare
  no package-level variable of the name, the update holds, naming the
  argument; where the package isn't in the archive, or a file is cut
  short or doesn't parse, it's coverage, holding nothing. A target
  outside the module, and a port that passes no `-X`, are out of reach.

TestAGoLinkTargetMustStillBeDeclared: mp4ff's shape holds, a declared
variable says nothing, and a missing or unparsable package is coverage.
