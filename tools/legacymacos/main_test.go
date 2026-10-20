package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

// verifier is a program that verifies a certificate as the system does,
// which is what imports Security.framework's trust functions.
const verifier = `package main

import "crypto/x509"

func main() {
	_, _ = new(x509.Certificate).Verify(x509.VerifyOptions{})
}
`

// TestUnlistedFindsTheMontereyImport builds a program that verifies a
// certificate with this Go, unpatched, and checks that the import macOS 12
// added is reported.
func TestUnlistedFindsTheMontereyImport(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a program")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(verifier), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module verifier\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "verifier")
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Dir = dir
	build.Env = append(os.Environ(), "GOOS=darwin", "GOARCH=amd64", "CGO_ENABLED=0", "GOFLAGS=", "GOTOOLCHAIN=local")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, output)
	}
	extra, err := unlisted(binary, parseAllowed(allowedList))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(extra, "_SecTrustCopyCertificateChain") {
		t.Errorf("unlisted = %q, want _SecTrustCopyCertificateChain among them", extra)
	}
}

func TestParseAllowedSkipsComments(t *testing.T) {
	allowed := parseAllowed("# a comment\n\n_open\n  _close  \n")
	if len(allowed) != 2 || !allowed["_open"] || !allowed["_close"] {
		t.Errorf("parseAllowed = %v, want _open and _close", allowed)
	}
}
