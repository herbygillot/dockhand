package binaryarchive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/subprocess"
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
// kept as, by its digest: its signify signature and its RSA one, written
// into directory, which the caller removes. The archive itself isn't
// copied.
func Sign(ctx context.Context, keys Keys, archive Archive, directory string) (Entry, error) {
	if !Installable(archive.Port, archive.Name) {
		return Entry{}, fmt.Errorf("binaryarchive: %s's %s can't be a site's entry", archive.Port, archive.Name)
	}
	data, err := os.ReadFile(archive.Path)
	if err != nil {
		return Entry{}, err
	}
	if sum := sha256.Sum256(data); "sha256:"+hex.EncodeToString(sum[:]) != archive.Digest {
		return Entry{}, fmt.Errorf("binaryarchive: %s isn't the %s it was kept as", archive.Path, archive.Digest)
	}
	sig, rmd160 := filepath.Join(directory, archive.Name+".sig"), filepath.Join(directory, archive.Name+".rmd160")
	if err := os.WriteFile(sig, keys.Signify.Sign(data, "verify with "+SignifyPublicKey), 0o600); err != nil {
		return Entry{}, err
	}
	// Signed as pubkeys.conf says to sign one's own archives.
	if _, err := subprocess.Run(ctx, subprocess.Spec{Tool: "openssl", Command: "dgst", Path: "/usr/bin/openssl",
		Args: []string{"dgst", "-ripemd160", "-sign", keys.RSA, "-out", rmd160, archive.Path}}); err != nil {
		os.Remove(sig)
		return Entry{}, err
	}
	return Entry{Port: archive.Port, Files: map[string]string{archive.Name: archive.Path, archive.Name + ".sig": sig, archive.Name + ".rmd160": rmd160}}, nil
}
