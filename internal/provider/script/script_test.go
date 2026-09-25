package script_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/coord"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/provider/script"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=Test", "-c", "user.email=test@example.org", "-c", "init.defaultBranch=master"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	return strings.TrimSpace(string(out))
}

type ports map[string][]string

func (p ports) Ports(_ context.Context, _ model.Source, directory string, _ model.Platform) ([]macports.PortInfo, error) {
	names, ok := p[directory]
	if !ok {
		return nil, errors.New("no Portfile")
	}
	var infos []macports.PortInfo
	for _, name := range names {
		infos = append(infos, macports.PortInfo{Name: name, Options: map[string]string{}})
	}
	return infos, nil
}

func (p ports) Directory(context.Context, model.Source, string) (string, error) {
	return "", errors.New("no such port")
}

// checked runs a check of an edit to jq with the given script.
func checked(t *testing.T, body string) (model.Run, string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	run := filepath.Join(root, "build ports.sh")
	require.NoError(t, os.WriteFile(run, []byte("#!/bin/sh\n"+body+"\n"), 0o755))
	run = "'" + run + "'"
	upstream, clone := filepath.Join(root, "upstream"), filepath.Join(root, "macports-ports")
	require.NoError(t, os.MkdirAll(filepath.Join(upstream, "textproc/jq"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(upstream, "textproc/jq/Portfile"), []byte("name jq\nversion 1.7.1\n"), 0o644))
	git(t, upstream, "init", "-q")
	git(t, upstream, "add", "-A")
	git(t, upstream, "commit", "-q", "-m", "init")
	git(t, root, "clone", "-q", upstream, clone)

	e, err := engine.Open(t.Context(), engine.Options{Tree: clone, Database: filepath.Join(root, "db", "dockhand.db"), Upstream: upstream})
	require.NoError(t, err)
	t.Cleanup(func() { e.Close() })
	branch, err := e.Start(t.Context(), engine.StartRequest{Name: "jq-update", Here: true})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(clone, "textproc/jq/Portfile"), []byte("name jq\nversion 1.8.1\n"), 0o644))
	capture, err := e.Capture(t.Context(), engine.CaptureRequest{Branch: branch})
	require.NoError(t, err)
	e.PortReader = ports{"textproc/jq": {"jq", "jq-docs"}}
	e.Providers = map[string]engine.Provider{"command": &script.Provider{Run: run, Label: "my build box", Repo: e.Repo}}
	plan, err := e.PlanCheck(t.Context(), engine.PlanRequest{Revision: capture.Revision, Environments: []model.Environment{{Provider: "command"}}})
	require.NoError(t, err)
	queued, err := e.Enqueue(t.Context(), branch, plan, model.OriginPerson)
	require.NoError(t, err)
	session, err := (&coord.Coordinator{Store: e.Store, Repository: e.Repository, Heartbeat: time.Second}).Start(t.Context(), model.SessionForeground, "test")
	require.NoError(t, err)
	finished, err := e.Drive(t.Context(), session, queued.ID)
	require.NoError(t, err)

	return finished, filepath.Join(e.LogDirectory(), finished.Name(), "command-1")
}

func TestTheScriptGetsTheRevisionAndReportsEachTarget(t *testing.T) {
	// The script proves the bundle applies on top of the base, then reports
	// jq passed with its tests failing and jq-docs failed at install.
	build := `dir=$(dirname "$1")
clone=$(mktemp -d)
git clone -q --no-checkout "$(sed -n 's/.*"bundle": "\(.*\)".*/\1/p' "$1")" "$clone" 2>"$dir/clone.err" || true
echo started > "$dir/build.log"
cat > "$dir/result.json" <<'JSON'
{"version": 1, "targets": [
  {"id": "jq", "outcome": "passed", "tests": "failed", "log": "jq.log"},
  {"id": "jq-docs", "outcome": "failed", "phase": "install"}
]}
JSON`
	run, dir := checked(t, build)
	require.Equal(t, model.RunFailed, run.State, run.Detail)
	require.Equal(t, "jq-docs did not pass", run.Detail)

	var request script.Request
	data, err := os.ReadFile(filepath.Join(dir, "request.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &request))
	require.Equal(t, script.Version, request.Version)
	require.Equal(t, "check-1", request.Run)
	require.Equal(t, "declared", request.Tests)
	require.Len(t, request.Targets, 2)
	require.Equal(t, "jq-docs", request.Targets[1].Subport)
	verify := exec.Command("git", "bundle", "list-heads", request.Bundle)
	out, err := verify.CombinedOutput()
	require.NoError(t, err, string(out))
	require.Contains(t, string(out), request.Commit+" "+request.Ref)

	// As docs/command-provider.md says: a clone holding the base fetches
	// the bundle and checks out the commit.
	ports := filepath.Join(t.TempDir(), "ports")
	git(t, filepath.Dir(ports), "clone", "-q", filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(dir)))), "upstream"), ports)
	git(t, ports, "fetch", "-q", request.Bundle, request.Ref)
	git(t, ports, "checkout", "-q", "--detach", request.Commit)
	portfile, err := os.ReadFile(filepath.Join(ports, "textproc/jq/Portfile"))
	require.NoError(t, err)
	require.Equal(t, "name jq\nversion 1.8.1\n", string(portfile), "the snapshot's files")
	require.FileExists(t, filepath.Join(dir, "command.log"))
}

func TestNoResultFileIsInfrastructureTrouble(t *testing.T) {
	run, dir := checked(t, `echo "the build box is unreachable" >&2; exit 3`)
	require.Equal(t, model.RunAttention, run.State)
	require.Contains(t, run.Detail, "failed 3 times")
	log, err := os.ReadFile(filepath.Join(dir, "command.log"))
	require.NoError(t, err)
	require.Contains(t, string(log), "unreachable")
	require.DirExists(t, strings.TrimSuffix(dir, "1")+"3", "three attempts, each with its own directory")
}
