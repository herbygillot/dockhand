// Package portfile identifies and edits literal inputs in Tcl Portfiles.
package portfile

type Edit struct {
	Path  string
	After []byte
}
