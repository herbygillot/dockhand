package buildcmd_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/buildenv/buildcmd"
	"github.com/herbygillot/dockhand/internal/buildenv/starter"
	"github.com/herbygillot/dockhand/internal/buildlog"
	"github.com/herbygillot/dockhand/internal/coord"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

type ports map[string][]string

func (p ports) Ports(_ context.Context, _ model.Source, directory string, _ model.Environment, variants map[string]bool) ([]macports.PortInfo, error) {
	names, ok := p[directory]
	if !ok {
		return nil, errors.New("no Portfile")
	}
	var infos []macports.PortInfo
	for _, name := range names {
		infos = append(infos, macports.PortInfo{Name: name, Options: map[string]string{}})
	}
	// A git.url entry has each directory's main port fetched with Git
	// from it, at its tag jq-1.8.1.
	if url := p["git.url"]; len(url) == 1 && len(infos) > 0 {
		infos[0].Options = map[string]string{"fetch.type": "git", "git.url": url[0], "git.branch": "jq-1.8.1"}
	}
	// A variants entry has each directory's main port declare them, the
	// last a default.
	if names := p["variants"]; len(names) > 0 && len(infos) > 0 {
		infos[0].Options["variants"] = strings.Join(names, " ")
		var vinfo []string
		for i, name := range names {
			vinfo = append(vinfo, name+map[bool]string{false: " {}", true: " {is_default +}"}[i == len(names)-1])
		}
		infos[0].Options["vinfo"] = strings.Join(vinfo, " ")
	}
	return infos, nil
}

func (p ports) Directory(context.Context, model.Source, string) (string, error) {
	return "", errors.New("no such port")
}

// checked runs a check of an edit to jq with the given script.
func checked(t *testing.T, body string) (model.Run, string, *engine.Engine) {
	t.Helper()
	e, session, queued := queuedCheck(t, body, 0)
	finished, err := e.Drive(t.Context(), session, queued)
	require.NoError(t, err)
	return finished, filepath.Join(e.LogDirectory(), finished.Name(), "command-1"), e
}

// queuedCheck queues a check of an edit to jq with the given script, and
// the grace a canceled one has, for a session to drive.
func queuedCheck(t *testing.T, body string, grace time.Duration) (*engine.Engine, *coord.Session, model.RunID) {
	t.Helper()
	return queuedCheckOf(t, body, grace, ports{"textproc/jq": {"jq", "jq-docs"}}, nil)
}

// checkSetup is what a check adds to queuedCheck's.
type checkSetup struct {
	// environment is what the script's environment adds, given the
	// repository MacPorts' master is in.
	environment func(upstream string) []string
	// plan changes the check's request, as --only and --variants do.
	plan func(*engine.PlanRequest)
}

// queuedCheckOf is queuedCheck with the ports as reader has them, and
// what setup adds where it's given.
func queuedCheckOf(t *testing.T, body string, grace time.Duration, reader ports, setup *checkSetup) (*engine.Engine, *coord.Session, model.RunID) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	run := filepath.Join(root, "build ports.sh")
	testsupport.WriteExecutable(t, run, "#!/bin/sh\n"+body+"\n")
	run = "'" + run + "'"
	upstream, clone := filepath.Join(root, "upstream"), filepath.Join(root, "macports-ports")
	require.NoError(t, os.MkdirAll(filepath.Join(upstream, "textproc/jq"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(upstream, "textproc/jq/Portfile"), []byte("name jq\nversion 1.7.1\n"), 0o644))
	testsupport.Git(t, upstream, "init", "-q")
	testsupport.Git(t, upstream, "add", "-A")
	testsupport.Git(t, upstream, "commit", "-q", "-m", "init")
	testsupport.Git(t, root, "clone", "-q", upstream, clone)

	e, err := engine.Open(t.Context(), engine.Options{Env: os.Environ(), Tree: clone, Database: filepath.Join(root, "db", "dockhand.db"), Upstream: upstream, Worktrees: filepath.Join(root, "worktrees")})
	require.NoError(t, err)
	t.Cleanup(func() { e.Close() })
	branch, err := e.Start(t.Context(), engine.StartRequest{Name: "jq-update", Here: true})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(clone, "textproc/jq/Portfile"), []byte("name jq\nversion 1.8.1\n"), 0o644))
	capture, err := e.Capture(t.Context(), engine.CaptureRequest{Branch: branch, Mode: engine.CaptureWorking})
	require.NoError(t, err)
	e.Ports.Reader = reader
	env := os.Environ()
	if setup != nil && setup.environment != nil {
		env = append(env, setup.environment(upstream)...)
	}
	self, err := starter.Self()
	require.NoError(t, err)
	e.Providers = map[string]buildenv.Provider{"command": &buildcmd.Provider{Run: run, Label: "my build box", Repo: e.Repo, Grace: grace, Env: env, Starter: self}}
	request := engine.PlanRequest{Revision: capture.Revision, Environments: []model.Environment{{Provider: "command"}}}
	if setup != nil && setup.plan != nil {
		setup.plan(&request)
	}
	plan, err := e.PlanCheck(t.Context(), request)
	require.NoError(t, err)
	queued, err := e.Enqueue(t.Context(), branch, plan, model.OriginPerson)
	require.NoError(t, err)
	session, err := (&coord.Coordinator{Store: e.Store, Repository: e.Repository, Heartbeat: time.Second}).Start(t.Context(), model.SessionForeground, "test")
	require.NoError(t, err)
	return e, session, queued.ID
}

// A canceled check stops what its script started, not only the shell: the
// script leads a session of its own, whose group is interrupted, then,
// after its grace, killed. A background job of a script ignores the
// interrupt, as sh has it, so only the kill reaches this one.
func TestACanceledScriptStopsWhatItStarted(t *testing.T) {
	t.Parallel()
	child := filepath.Join(t.TempDir(), "child.pid")
	e, session, queued := queuedCheck(t, `sleep 300 &
echo $! > '`+child+`'
wait`, 200*time.Millisecond)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = e.Drive(ctx, session, queued)
	}()
	var pid string
	require.Eventually(t, func() bool {
		data, err := os.ReadFile(child)
		pid = strings.TrimSpace(string(data))
		return err == nil && pid != ""
	}, testsupport.Settle, 20*time.Millisecond, "the script started its child")
	cancel()
	<-done
	require.Eventually(t, func() bool {
		return exec.Command("kill", "-0", pid).Run() != nil
	}, testsupport.Settle, 50*time.Millisecond, "the child is stopped with its script")
}

func TestTheScriptGetsTheRevisionAndReportsEachTarget(t *testing.T) {
	t.Parallel()
	// The script proves the bundle applies on top of the base, then reports
	// jq passed with its tests failing and jq-docs failed at install.
	build := `dir=$(dirname "$1")
clone=$(mktemp -d)
git clone -q --no-checkout "$(sed -n 's/.*"bundle": "\(.*\)".*/\1/p' "$1")" "$clone" 2>"$dir/clone.err" || true
echo started > "$dir/build.log"
cat > "$dir/result.json" <<'JSON'
{"version": 1, "reference": "https://ci.example.org/builds/42", "targets": [
  {"id": "jq", "outcome": "passed", "tests": "failed", "log": "jq.log"},
  {"id": "jq-docs", "outcome": "failed", "phase": "install"}
]}
JSON`
	run, dir, e := checked(t, build)
	require.Equal(t, model.RunFailed, run.State, run.Detail)
	require.Equal(t, "jq-docs did not pass", run.Detail)

	var request buildcmd.Request
	data, err := os.ReadFile(filepath.Join(dir, "request.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &request))
	require.Equal(t, buildcmd.Version, request.Version)
	require.Equal(t, "check-1", request.Run)
	require.Regexp(t, `^command_[a-z0-9]{16}$`, request.Execution, "the provider run's ID, to label the script's own logs")
	// The script's own name for the run is recorded, and finds it.
	execution, err := e.ExecutionNamed(t.Context(), "https://ci.example.org/builds/42")
	require.NoError(t, err)
	require.Equal(t, model.ExecutionID(request.Execution), execution.ID)
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
	testsupport.Git(t, filepath.Dir(ports), "clone", "-q", filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(dir)))), "upstream"), ports)
	testsupport.Git(t, ports, "fetch", "-q", request.Bundle, request.Ref)
	testsupport.Git(t, ports, "checkout", "-q", "--detach", request.Commit)
	portfile, err := os.ReadFile(filepath.Join(ports, "textproc/jq/Portfile"))
	require.NoError(t, err)
	require.Equal(t, "name jq\nversion 1.8.1\n", string(portfile), "the snapshot's files")
	require.FileExists(t, filepath.Join(dir, "command.log.gz"), "kept compressed once the check ended (D6)")
}

// A Git-fetched target's request names its repository, git.branch, and
// the commit git.branch named when the check was planned, which its fetch
// must check out. The commit the script says the fetch checked out is kept
// with the result, and one other than the request's fails the target at
// fetch, whatever its outcome says (batch 20).
func TestTheScriptIsToldTheCommitAGitFetchMustCheckOut(t *testing.T) {
	t.Parallel()
	upstream := t.TempDir()
	testsupport.Git(t, upstream, "init", "-q")
	testsupport.Git(t, upstream, "commit", "-q", "--allow-empty", "-m", "1.8.1")
	testsupport.Git(t, upstream, "tag", "jq-1.8.1")
	commit := testsupport.Git(t, upstream, "rev-parse", "HEAD")
	reader := ports{"textproc/jq": {"jq", "jq-docs"}, "git.url": {upstream}}
	reporting := func(fetched string) string {
		return `cat > "$(dirname "$1")/result.json" <<'JSON'
{"version": 1, "targets": [
  {"id": "jq", "outcome": "passed", "fetched": "` + fetched + `"},
  {"id": "jq-docs", "outcome": "passed"}
]}
JSON`
	}
	checked := func(body string) (model.Run, engine.RunLogs, string) {
		e, session, queued := queuedCheckOf(t, body, 0, reader, nil)
		run, err := e.Drive(t.Context(), session, queued)
		require.NoError(t, err)
		logs, err := e.Logs(t.Context(), run.ID)
		require.NoError(t, err)
		return run, logs, filepath.Join(e.LogDirectory(), run.Name(), "command-1")
	}

	run, logs, dir := checked(reporting(commit))
	require.Equal(t, model.RunPassed, run.State, run.Detail)
	var request buildcmd.Request
	data, err := os.ReadFile(filepath.Join(dir, "request.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &request))
	require.Equal(t, &buildcmd.Git{URL: upstream, Branch: "jq-1.8.1", Commit: commit}, request.Targets[0].Git)
	require.Nil(t, request.Targets[1].Git, "a subport fetched otherwise")
	require.Equal(t, model.ObjectID(commit), logs.Executions[0].Git["jq"].Fetched, "kept with its result")

	other := strings.Repeat("b", 40)
	run, logs, _ = checked(reporting(other))
	require.Equal(t, model.RunFailed, run.State)
	jq := logs.Executions[0].Results[slices.IndexFunc(logs.Executions[0].Results, func(result model.TargetResult) bool { return result.Target == "jq" })]
	require.Equal(t, model.OutcomeFailed, jq.Outcome)
	require.Equal(t, model.PhaseFetch, jq.Phase)
	require.Contains(t, jq.Detail, "the source moved: git.branch jq-1.8.1 named "+commit[:7])

	run, _, _ = checked(reporting("jq-1.8.1"))
	require.Equal(t, model.RunAttention, run.State, "a commit that isn't one is trouble with the script")
}

func TestNoResultFileIsInfrastructureTrouble(t *testing.T) {
	t.Parallel()
	run, dir, _ := checked(t, `echo "the build box is unreachable" >&2; exit 3`)
	require.Equal(t, model.RunAttention, run.State)
	require.Contains(t, run.Detail, "failed 3 times")
	log, err := buildlog.ReadFile(filepath.Join(dir, "command.log"))
	require.NoError(t, err)
	require.Contains(t, string(log), "unreachable")
	require.DirExists(t, strings.TrimSuffix(dir, "1")+"3", "three attempts, each with its own directory")
}

// documentedScript is the example script docs/command-provider.md gives,
// between its markers, so the page is the one copy.
func documentedScript(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("../../../docs/command-provider.md")
	require.NoError(t, err)
	_, block, found := strings.Cut(string(data), "<!-- ssh-example: begin -->\n")
	require.True(t, found, "the page marks where its script begins")
	block, _, found = strings.Cut(block, "<!-- ssh-example: end -->")
	require.True(t, found, "the page marks where its script ends")
	block, found = strings.CutPrefix(block, "```sh\n#!/bin/sh\n")
	require.True(t, found, "a sh code block")
	block, found = strings.CutSuffix(block, "```\n")
	require.True(t, found, "a sh code block")
	return block
}

// The script docs/command-provider.md gives for a Mac over SSH reads the
// request file the provider writes, and writes a result file it reads
// back: here ssh, scp, sudo, port, and port-tclsh stand in for the other
// Mac, acting on this one. The bundle is fetched into the clone the
// other Mac has, at the commit; each target is built in order on its
// Portfile, as its subport with its variants; a failed one names its
// phase; and a Git target's fetched is the commit its worksrcpath is at.
func TestTheDocumentedSSHScriptBuildsThroughTheProvider(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("the documented script reads the request with jq, which this host hasn't")
	}
	source := t.TempDir()
	testsupport.Git(t, source, "init", "-q")
	testsupport.Git(t, source, "commit", "-q", "--allow-empty", "-m", "1.8.1")
	testsupport.Git(t, source, "tag", "jq-1.8.1")
	commit := testsupport.Git(t, source, "rev-parse", "HEAD")

	stubs := t.TempDir()
	testsupport.WriteExecutable(t, filepath.Join(stubs, "ssh"), `#!/bin/sh
while [ $# -gt 0 ]; do
  case $1 in -o) shift 2 ;; -*) shift ;; *) break ;; esac
done
echo "$1" >> "$SSH_HOSTS"
shift
exec /bin/sh -c "$*"
`)
	testsupport.WriteExecutable(t, filepath.Join(stubs, "scp"), `#!/bin/sh
while [ $# -gt 2 ]; do shift; done
echo "${2%%:*}" >> "$SSH_HOSTS"
exec cp "$1" "${2#*:}"
`)
	testsupport.WriteExecutable(t, filepath.Join(stubs, "sudo"), `#!/bin/sh
[ "$1" = -n ] || { echo "sudo would prompt" >&2; exit 1; }
shift
exec "$@"
`)
	// port is given -N -D <portdir> <action...> subport=<name> <variants>.
	// Its fetch needs the branch's Portfile there, and jq-docs fails to
	// install.
	testsupport.WriteExecutable(t, filepath.Join(stubs, "port"), `#!/bin/sh
echo "$*" >> "$PORT_CALLS"
case " $* " in
*" fetch "*) grep -q 'version 1.8.1' "$3/Portfile" || { echo "Error: not the branch's Portfile"; exit 1; } ;;
*" install subport=jq-docs ") echo "Error: Failed to install jq-docs"; exit 1 ;;
esac
`)
	// jq declares its tests with +tests -x11, only where MacPorts opens it
	// with them, and not with its defaults.
	testsupport.WriteExecutable(t, filepath.Join(stubs, "port-tclsh"), `#!/bin/sh
script=$(cat)
case $script in
*"option worksrcpath"*) echo "$WORKSRC" ;;
*"subport {jq}] {tests + x11 -}]"*"option test.run"*) echo yes ;;
*"option test.run"*) echo no ;;
esac
`)
	reader := ports{"textproc/jq": {"jq", "jq-docs"}, "git.url": {source}, "variants": {"tests", "x11"}}
	// checked runs a check through the documented script, the other Mac's
	// clone, ssh's hosts, and port's calls its own.
	checked := func(plan func(*engine.PlanRequest)) (model.Run, *engine.Engine, buildcmd.Request, string, []string) {
		other := t.TempDir()
		clone, hosts, calls := filepath.Join(other, "macports-ports"), filepath.Join(other, "hosts"), filepath.Join(other, "port calls")
		environment := func(upstream string) []string {
			testsupport.Git(t, other, "clone", "-q", upstream, clone)
			return []string{"PATH=" + stubs + string(os.PathListSeparator) + os.Getenv("PATH"), "BUILD_HOST=oldmac.example.org", "BUILD_PORTS=" + clone,
				"SSH_HOSTS=" + hosts, "PORT_CALLS=" + calls, "WORKSRC=" + source}
		}
		e, session, queued := queuedCheckOf(t, documentedScript(t), 0, reader, &checkSetup{environment: environment, plan: plan})
		run, err := e.Drive(t.Context(), session, queued)
		require.NoError(t, err)
		var request buildcmd.Request
		data, err := os.ReadFile(filepath.Join(e.LogDirectory(), run.Name(), "command-1", "request.json"))
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(data, &request))
		require.Equal(t, request.Commit, testsupport.Git(t, clone, "rev-parse", "HEAD"), "the other Mac's clone is at the commit")
		data, err = os.ReadFile(hosts)
		require.NoError(t, err)
		for _, host := range strings.Fields(string(data)) {
			require.Equal(t, "oldmac.example.org", host, "only BUILD_HOST is reached")
		}
		data, err = os.ReadFile(calls)
		require.NoError(t, err)
		return run, e, request, filepath.Join(clone, "textproc/jq"), strings.Split(strings.TrimSpace(string(data)), "\n")
	}
	result := func(request buildcmd.Request) buildcmd.Result {
		var result buildcmd.Result
		data, err := os.ReadFile(request.Result)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(data, &result))
		return result
	}

	run, e, request, portdir, calls := checked(nil)
	require.Equal(t, model.RunFailed, run.State, run.Detail)
	require.Equal(t, "jq-docs did not pass", run.Detail)
	require.Equal(t, buildcmd.Result{Version: buildcmd.Version, Targets: []buildcmd.TargetResult{
		{ID: "jq", Outcome: "passed", Tests: "none", Log: "target-1.log", Fetched: commit},
		{ID: "jq-docs", Outcome: "failed", Phase: "install", Tests: "none", Log: "target-2.log"},
	}}, result(request))

	logs, err := e.Logs(t.Context(), run.ID)
	require.NoError(t, err)
	results := logs.Executions[0].Results
	jq := results[slices.IndexFunc(results, func(result model.TargetResult) bool { return result.Target == "jq" })]
	require.Equal(t, model.OutcomePassed, jq.Outcome)
	require.Equal(t, model.TestsNone, jq.Tests, "its defaults declare no tests")
	require.Equal(t, model.ObjectID(commit), logs.Executions[0].Git["jq"].Fetched)
	docs := results[slices.IndexFunc(results, func(result model.TargetResult) bool { return result.Target == "jq-docs" })]
	require.Equal(t, model.OutcomeFailed, docs.Outcome)
	require.Equal(t, model.PhaseInstall, docs.Phase)
	log, err := buildlog.ReadFile(docs.Log)
	require.NoError(t, err)
	require.Contains(t, string(log), "Failed to install jq-docs", "the target's port output is its log")

	var want []string
	for _, action := range []string{"lint", "clean --work", "fetch", "checksum", "destroot", "install"} {
		want = append(want, "-N -D "+portdir+" "+action+" subport=jq")
	}
	for _, action := range []string{"lint", "clean --work", "fetch", "checksum", "destroot", "install"} {
		want = append(want, "-N -D "+portdir+" "+action+" subport=jq-docs")
	}
	require.Equal(t, want, calls, "each target in order, as its subport")

	// A target with variants is built with them, as port takes them, and
	// MacPorts opens it with them to read its test.run.
	run, e, request, portdir, calls = checked(func(plan *engine.PlanRequest) {
		plan.Only, plan.Variants = []string{"jq"}, map[string]bool{"tests": true, "x11": false}
	})
	require.Equal(t, model.RunPassed, run.State, run.Detail)
	require.Len(t, request.Targets, 1)
	require.Equal(t, map[string]bool{"tests": true, "x11": false}, request.Targets[0].Variants)
	require.Equal(t, buildcmd.Result{Version: buildcmd.Version, Targets: []buildcmd.TargetResult{
		{ID: "jq +tests -x11", Outcome: "passed", Tests: "passed", Log: "target-1.log", Fetched: commit},
	}}, result(request))
	logs, err = e.Logs(t.Context(), run.ID)
	require.NoError(t, err)
	require.Equal(t, model.TestsPassed, logs.Executions[0].Results[0].Tests)
	want = nil
	for _, action := range []string{"lint", "clean --work", "fetch", "checksum", "destroot", "install", "test"} {
		want = append(want, "-N -D "+portdir+" "+action+" subport=jq +tests -x11")
	}
	require.Equal(t, want, calls, "each port call carries the target's variants")
}
