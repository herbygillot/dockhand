package tart

import (
	"context"
	_ "embed"
	"encoding/json"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/staging"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

func atomicFile(path string, data []byte, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".dockhand-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err = file.Chmod(mode); err == nil {
		_, err = file.Write(data)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(file.Name(), path); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
func safeToken(s string) bool {
	return s != "" && !strings.HasPrefix(s, "-") && !strings.ContainsAny(s, "/\\") && strings.IndexFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) < 0
}
func validPortfile(s string) bool {
	return fs.ValidPath(s) && !strings.ContainsAny(s, "\\\x00\r\n") && len(strings.Split(s, "/")) == 3 && filepath.Base(s) == "Portfile"
}

//go:embed guest.tcl
var guestScript []byte

type guestInput struct {
	Protocol int
	ID       string
	Digest   string
	Spec     record.BuildSpec
	Prefix   string
}
type guestResult struct {
	Environment  *record.GuestEnvironment
	TestOmission string
	Protocol     int
	ID           string
	Digest       string
	State        string
	Verdict      record.Verdict
	Steps        []record.StepResult
	Failure      *record.Failure
	Detail       string
}

// verifierDigest changes with the guest program and its launch protocol.
func verifierDigest() string {
	return digest(append([]byte("tart-verification-v2\x00"+guestExecScript+"\x00"+string(guestPlist("/prefix"))), guestScript...))
}

func makeInput(ctx context.Context, repo *git.Repository, request verify.Request, c Config, directory string, client *http.Client) (string, error) {
	input, err := json.Marshal(guestInput{Protocol: 1, ID: string(request.ID), Digest: buildDigest(request.Spec), Spec: request.Spec, Prefix: c.GuestPrefix})
	if err != nil {
		return "", err
	}
	payload := map[string][]byte{"guest.tcl": guestScript, "input.json": input, "guest.plist": guestPlist(c.GuestPrefix)}
	path := filepath.Join(directory, "input.tar")
	err = staging.Archive(ctx, repo, staging.Request{Source: request.Spec.Source, Target: request.Spec.Target, Platform: request.Spec.Config.Platform, Index: portindex.Config{
		Executable: c.PortIndexExecutable, Digest: c.PortIndexDigest, MirrorURL: c.PortIndexURL, CacheDirectory: filepath.Join(c.ArtifactDirectory, "indexes"),
	}}, path, payload, client)
	return path, err
}
