// Package project reads what an upstream project's own files say of it:
// its license files, its build files, and each ecosystem's manifest, as a
// typed record of that ecosystem's own (the assessment design, B). It
// reads one source tree at a root its caller names, and says what it
// couldn't read, so an empty reading never stands for one it couldn't
// make.
//
// It knows nothing of MacPorts: which PortGroups use which build system,
// where a port builds, and what a change means for a port are for the
// packages that know ports, which map their facts onto this package's.
package project
