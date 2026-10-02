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
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"golang.org/x/crypto/ripemd160" //nolint:staticcheck // MacPorts verifies a site's RSA signatures over RIPEMD-160 (pubkeys.conf).

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
	// RSA is the RSA key's file, and RSAPublic its public half, as openssl
	// writes one.
	RSA       string
	RSAPublic []byte
	rsa       *rsa.PrivateKey
}

// rmd160DigestInfo is the DER prefix of a PKCS #1 v1.5 signature's
// DigestInfo for RIPEMD-160 as OpenSSL writes it, TeleTrusT's OID with
// NULL parameters, which openssl dgst -verify, and so MacPorts, checks.
// Go's crypto.RIPEMD160 writes ISO's OID without parameters, which openssl
// rejects (the library survey, verified 2026-10-02).
var rmd160DigestInfo = []byte{0x30, 0x21, 0x30, 0x09, 0x06, 0x05, 0x2b, 0x24, 0x03, 0x02, 0x01, 0x05, 0x00, 0x04, 0x14}

// signRIPEMD160 is the signature openssl dgst -ripemd160 -sign writes of
// the data with the RSA key, byte for byte, since PKCS #1 v1.5 is
// deterministic: as pubkeys.conf says to sign one's own archives.
func (k Keys) signRIPEMD160(data []byte) ([]byte, error) {
	if k.rsa == nil {
		return nil, fmt.Errorf("binaryarchive: no RSA key loaded")
	}
	digest := ripemd160.New()
	digest.Write(data)
	return rsa.SignPKCS1v15(nil, k.rsa, 0, append(slices.Clone(rmd160DigestInfo), digest.Sum(nil)...))
}

// id names the keys, for the signatures they made: a signature kept
// beside an archive is for the keys of its name, so new keys sign again.
func (k Keys) id() string {
	sum := sha256.Sum256(append(slices.Clone(k.RSAPublic), k.Signify.PublicKey("")...))
	return hex.EncodeToString(sum[:4])
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
	keys.rsa = private
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
