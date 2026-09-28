// Package signify makes the keys and signatures OpenBSD's signify(1)
// verifies, which MacPorts checks an archive fetched from an archive site
// with (signify -V -p key -x signature -m archive): an Ed25519 key, and a
// signature of the whole file. A file of each is two lines, an untrusted
// comment and the base64 of the algorithm, the key's number, and the key
// or the signature.
package signify

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
)

// algorithm names Ed25519 in signify's files.
const algorithm = "Ed"

// Key is a signing key: its number, which ties its signatures to its
// public key, and its Ed25519 seed.
type Key struct {
	Number [8]byte
	Seed   [ed25519.SeedSize]byte
}

// Generate makes a new key.
func Generate() (Key, error) {
	var key Key
	if _, err := rand.Read(key.Number[:]); err != nil {
		return key, err
	}
	_, err := rand.Read(key.Seed[:])
	return key, err
}

// MarshalBinary is the key's number and seed, as a file keeps it.
func (k Key) MarshalBinary() ([]byte, error) {
	return append(k.Number[:], k.Seed[:]...), nil
}

// UnmarshalBinary reads a key MarshalBinary wrote.
func (k *Key) UnmarshalBinary(data []byte) error {
	if len(data) != len(k.Number)+len(k.Seed) {
		return errors.New("signify: a key is its number and seed")
	}
	copy(k.Number[:], data)
	copy(k.Seed[:], data[len(k.Number):])
	return nil
}

// PublicKey is the public key's file, which MacPorts lists in
// pubkeys.conf, as a .pub file, to verify signatures with.
func (k Key) PublicKey(comment string) []byte {
	public := ed25519.NewKeyFromSeed(k.Seed[:]).Public().(ed25519.PublicKey)
	return file(comment, k.Number, public)
}

// Sign is a signature file for message, the whole of it.
func (k Key) Sign(message []byte, comment string) []byte {
	return file(comment, k.Number, ed25519.Sign(ed25519.NewKeyFromSeed(k.Seed[:]), message))
}

func file(comment string, number [8]byte, value []byte) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "untrusted comment: %s\n", comment)
	b.WriteString(base64.StdEncoding.EncodeToString(append(append([]byte(algorithm), number[:]...), value...)))
	b.WriteByte('\n')
	return b.Bytes()
}
