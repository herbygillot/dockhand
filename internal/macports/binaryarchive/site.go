package binaryarchive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/herbygillot/dockhand/internal/atomicfile"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
)

// The public keys' files at a site's root: signify's, and the RSA key's,
// which a MacPorts installation is told to trust for the site's archives.
const (
	SignifyPublicKey = "dockhand.pub"
	RSAPublicKey     = "dockhand.pem"
)

// PublicKeys are the files at a site's root a MacPorts installation
// verifies its archives with, by name.
func (k Keys) PublicKeys() map[string][]byte {
	return map[string][]byte{SignifyPublicKey: k.Signify.PublicKey("dockhand archives"), RSAPublicKey: k.RSAPublic}
}

// Installable reports whether a port's archive can be a site's entry: the
// port's name a port's (macports.ValidName), which the entry's URL,
// <site>/<port>/<archive>, takes as a path segment as it is, and the file
// named as MacPorts names one.
func Installable(port, name string) bool {
	return macports.ValidName(port) && !strings.ContainsAny(port, "#?%") && model.ValidArchiveName(name)
}

// EntryPath is where a site keeps a port's archive: in its port's
// directory, as MacPorts fetches one.
func EntryPath(site, port, name string) string {
	return path.Join(site, port, name)
}

// Entry is one archive signed for a site: the archive and its two
// signatures, each a local file, by the name its site path ends with.
type Entry struct {
	Port string
	// Files are the archive and its signatures, local paths by the name
	// each has in the port's directory of the site.
	Files map[string]string
}

// Archive is a built archive kept on this host: its port, MacPorts' name
// for its file, its digest, sha256:<hex>, and where it is.
type Archive struct {
	Port, Name, Digest string
	Path               string
}

// Sign makes an archive a site's entry, once it is the archive it was
// kept as, by its digest: its signify signature and its RSA one. They're
// kept beside the archive, named by the keys that made them, and made
// once: an archive given to every guest of every check was signed again
// for each, which the person chose to end (D18, 2026-10-02). The digest is
// checked on every call, before the entry is given out; the archive is
// read through a read-only map, whose pages the system can drop, rather
// than into memory whole. Whoever removes the archive removes its
// signatures, every file whose name is the archive's and a dot more.
func Sign(ctx context.Context, keys Keys, archive Archive) (Entry, error) {
	if !Installable(archive.Port, archive.Name) {
		return Entry{}, fmt.Errorf("binaryarchive: %s's %s can't be a site's entry", archive.Port, archive.Name)
	}
	data, unmap, err := mapped(archive.Path)
	if err != nil {
		return Entry{}, err
	}
	defer unmap()
	if sum := sha256.Sum256(data); "sha256:"+hex.EncodeToString(sum[:]) != archive.Digest {
		return Entry{}, fmt.Errorf("binaryarchive: %s isn't the %s it was kept as", archive.Path, archive.Digest)
	}
	id := keys.id()
	sig, rmd160 := archive.Path+"."+id+".sig", archive.Path+"."+id+".rmd160"
	if err := signOnce(sig, func() ([]byte, error) { return keys.Signify.Sign(data, "verify with "+SignifyPublicKey), nil }); err != nil {
		return Entry{}, err
	}
	if err := signOnce(rmd160, func() ([]byte, error) { return keys.signRIPEMD160(data) }); err != nil {
		return Entry{}, err
	}
	return Entry{Port: archive.Port, Files: map[string]string{archive.Name: archive.Path, archive.Name + ".sig": sig, archive.Name + ".rmd160": rmd160}}, nil
}

// signOnce writes a signature where none is yet, whole or not at all.
func signOnce(path string, sign func() ([]byte, error)) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	signature, err := sign()
	if err != nil {
		return err
	}
	return atomicfile.Write(path, signature, 0o600)
}

// mapped is a file's contents through a read-only map, and how to let it
// go; an empty file, which can't be mapped, is empty.
func mapped(path string) ([]byte, func(), error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, nil, err
	}
	if info.Size() == 0 {
		return []byte{}, func() {}, nil
	}
	data, err := unix.Mmap(int(file.Fd()), 0, int(info.Size()), unix.PROT_READ, unix.MAP_SHARED)
	if err != nil {
		return nil, nil, fmt.Errorf("binaryarchive: mapping %s: %w", path, err)
	}
	return data, func() { _ = unix.Munmap(data) }, nil
}
