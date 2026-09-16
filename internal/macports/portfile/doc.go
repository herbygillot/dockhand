// Package portfile identifies and edits literal inputs in Tcl Portfiles.
//
// It uses syntax spans to replace version candidates, revisions, and checksum
// values while preserving surrounding source. Evaluated values and replacement
// data come from callers; portedit performs native evaluation and checks the
// meaning of the resulting edits.
package portfile
