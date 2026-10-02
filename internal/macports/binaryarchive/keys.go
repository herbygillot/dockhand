// Package binaryarchive makes built MacPorts archives installable from an
// archive site of MacPorts' own kind: signed both ways MacPorts verifies a
// site's archives, beside the public keys a MacPorts installation is told
// to trust for them (decision 28). It owns the signing keys and a site
// entry's files; a provider copies those files where its builder's
// MacPorts reads them, and configures MacPorts to.
//
// Three kinds of archive are distinct in dockhand: upstream source
// archives (internal/archive, macports/distfetch), the packages MacPorts
// builds (model.Archive), and here, a package signed as a site's entry.
package binaryarchive

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/herbygillot/dockhand/internal/signify"
)

// Keys are the keys dockhand signs the archives it gives builders with,
// made on first use. MacPorts verifies an archive a site serves by
// whichever of its signature types it asks for first: openssl's RIPEMD-160
// with an RSA key, the kind pubkeys.conf documents for one's own archives,
// or signify's, the kind MacPorts' own site declares. So each archive is
// signed both ways, as packages.macports.org serves both.
type Keys struct {
	Signify signify.Key
	// RSA is the RSA key's file, which openssl signs with, and RSAPublic
	// its public half, as openssl writes one.
	RSA       string
	RSAPublic []byte
}

// LoadKeys makes the keys in a directory once, and reads them after. Two
// processes making them at once keep the first one's. The directory is
// where they have always been kept, beside dockhand's SSH keys, so that
// moving their owner rotated nothing.
func LoadKeys(directory string) (Keys, error) {
	var keys Keys
	data, err := once(filepath.Join(directory, "archives.key"), func() ([]byte, error) {
		key, err := signify.Generate()
		if err != nil {
			return nil, err
		}
		return key.MarshalBinary()
	})
	if err != nil {
		return keys, err
	}
	if err := keys.Signify.UnmarshalBinary(data); err != nil {
		return keys, err
	}
	keys.RSA = filepath.Join(directory, "archives-rsa.pem")
	data, err = once(keys.RSA, func() ([]byte, error) {
		private, err := rsa.GenerateKey(rand.Reader, 3072)
		if err != nil {
			return nil, err
		}
		return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(private)}), nil
	})
	if err != nil {
		return keys, err
	}
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "RSA PRIVATE KEY" {
		return keys, fmt.Errorf("binaryarchive: %s is not an RSA key", keys.RSA)
	}
	private, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return keys, fmt.Errorf("binaryarchive: %s: %w", keys.RSA, err)
	}
	public, err := x509.MarshalPKIXPublicKey(&private.PublicKey)
	if err != nil {
		return keys, err
	}
	keys.RSAPublic = pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: public})
	return keys, nil
}

// once reads a file, or makes it with what create gives, readable by its
// owner alone. Two processes making it at once keep the first one's.
func once(path string, create func() ([]byte, error)) ([]byte, error) {
	data, err := os.ReadFile(path)
	if !errors.Is(err, os.ErrNotExist) {
		return data, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	if data, err = create(); err != nil {
		return nil, err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-")
	if err != nil {
		return nil, err
	}
	defer os.Remove(temporary.Name())
	_, err = temporary.Write(data)
	if err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, err
	}
	if err := os.Link(temporary.Name(), path); errors.Is(err, os.ErrExist) {
		return os.ReadFile(path)
	} else if err != nil {
		return nil, err
	}
	return data, nil
}
