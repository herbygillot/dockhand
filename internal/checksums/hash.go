package checksums

import (
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"io"
	"os"

	"golang.org/x/crypto/ripemd160" //nolint:staticcheck // rmd160 is what MacPorts records; weak-for-crypto is beside the point.
)

// Hash streams bytes into the checksum triple: an io.Writer for
// fetchers that hash in flight.
type Hash struct {
	rmd  hash.Hash
	sha  hash.Hash
	size int64
}

// New returns an empty Hash.
func New() *Hash {
	return &Hash{rmd: ripemd160.New(), sha: sha256.New()}
}

// Write implements io.Writer; it cannot fail.
func (h *Hash) Write(p []byte) (int, error) {
	h.rmd.Write(p) //nolint:errcheck // hash writes cannot fail
	h.sha.Write(p) //nolint:errcheck
	h.size += int64(len(p))
	return len(p), nil
}

// Sums returns the triple of everything written so far.
func (h *Hash) Sums() Sums {
	return Sums{
		Rmd160: hex.EncodeToString(h.rmd.Sum(nil)),
		Sha256: hex.EncodeToString(h.sha.Sum(nil)),
		Size:   h.size,
	}
}

// HashFile computes the checksum triple of a local file — the hashing
// half of a fetch whose transfer happened elsewhere (portfetch's
// MacPorts-driven downloads).
func HashFile(path string) (Sums, error) {
	f, err := os.Open(path)
	if err != nil {
		return Sums{}, err
	}
	defer f.Close() //nolint:errcheck // read-path close
	h := New()
	if _, err := io.Copy(h, f); err != nil {
		return Sums{}, err
	}
	return h.Sums(), nil
}
