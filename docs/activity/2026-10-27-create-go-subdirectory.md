# create go:<module> below its repository's root

2026-10-27, Then's item 14, its I33 part (the docs dig's I33, the
person's word of 2026-10-26 22:20Z).

- **registry** no longer refuses a module below its repository's root
  (`ErrSubdirectory` is gone). For `go:` it returns:
  - the module's directory (`Found.Directory`, "gopls");
  - its root's module path (`Found.Package`, "golang.org/x/tools");
  - for a module below the root, its version from Go's module proxy
    (`$module/@latest`, the GOPROXY protocol). Such a module is tagged
    `<directory>/v<version>`, which a repository's releases rarely name.

  `golang.org/x/<repo>` is read from its GitHub mirror,
  github.com/golang/<repo>, which the golang PortGroup fetches from
  (golang-1.0.tcl's `go._translate_package_id`).
- **create** reads such a project at the module's tag (`publishedTag`,
  with `directoryTag`'s "gopls/" prefix). Its files come from the module's
  directory, and it is named for that directory.
- **The Portfile**, as devel/gopls is written:
  - `go.setup golang.org/x/tools 0.20.0 gopls/v`; golang.org/x's packages
    are named as they are, and others as github.com/<owner>/<project>;
  - `dist_subdir ${name}`, `build.dir ${worksrcpath}/gopls`;
  - `xinstall ${build.dir}/${name}` in the destroot;
  - `go.offline_build no`, marked unconfirmed, and no go.vendors block,
    since go2port reads a module at its repository's root and gopls's
    go.mod replaces golang.org/x/tools with `../`.
- **Checked**: the written Portfile for gopls 0.24.0 evaluates with
  MacPorts (`port info`), and its distfile is
  github.com/golang/tools/archive/gopls/v0.24.0. A build in Tart wasn't
  run here.
- Tests: the registry test's module cases,
  TestAModuleBelowItsRootBuildsInItsDirectory, and
  TestAModuleBelowItsRootIsObservedAtItsTag.
