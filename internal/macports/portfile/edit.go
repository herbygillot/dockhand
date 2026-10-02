package portfile

type Edit struct {
	Path  string
	After []byte
	// Delete removes the file, as dropping a patch upstream merged
	// removes it from files/.
	Delete bool
}
