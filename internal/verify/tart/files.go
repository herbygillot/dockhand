package tart

import (
	"context"
	_ "embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/tart/host"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/staging"
)

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
	// TestTimeoutSeconds bounds the test phase; the guest kills a longer run.
	TestTimeoutSeconds int
}
type guestResult struct {
	Environment  *record.GuestEnvironment
	TestOmission string
	TestFailure  string
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
	return digest(append([]byte("tart-verification-v2\x00"+host.ExecScript+"\x00"+string(guestPlist("/prefix"))), guestScript...))
}

func makeInput(ctx context.Context, repo *git.Repository, request verify.Request, c Config, indexCache, directory string, client *http.Client) (string, error) {
	input, err := json.Marshal(guestInput{Protocol: 1, ID: string(request.ID), Digest: buildDigest(request.Spec), Spec: request.Spec, Prefix: c.GuestPrefix, TestTimeoutSeconds: int(c.testTimeout() / time.Second)})
	if err != nil {
		return "", err
	}
	payload := map[string][]byte{"guest.tcl": guestScript, "input.json": input, "guest.plist": guestPlist(c.GuestPrefix)}
	path := filepath.Join(directory, "input.tar")
	err = staging.Archive(ctx, repo, staging.Request{AdditionalTargets: request.Spec.Preinstall, Source: request.Spec.Source, Target: request.Spec.Target, Platform: request.Spec.Config.Platform, Index: sourceIndex(c, indexCache)}, path, payload, client)
	return path, err
}
