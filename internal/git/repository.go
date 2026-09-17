package git

import (
	"bytes"
	"context"
	"errors"
	"github.com/herbygillot/dockhand/internal/subprocess"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Repository struct {
	Root       string
	CommonDir  string
	Executable string
}

func Open(ctx context.Context, directory, executable string) (*Repository, error) {
	if executable == "" {
		executable = "git"
	}
	r := &Repository{Root: directory, Executable: executable}
	root, err := r.output(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, err
	}
	r.Root = strings.TrimSpace(string(root))
	common, err := r.output(ctx, "rev-parse", "--git-common-dir")
	if err != nil {
		return nil, err
	}
	r.CommonDir = strings.TrimSpace(string(common))
	if !filepath.IsAbs(r.CommonDir) {
		r.CommonDir = filepath.Join(r.Root, r.CommonDir)
	}
	r.CommonDir, err = filepath.EvalSymlinks(r.CommonDir)
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Repository) Resolve(ctx context.Context, revision string) (string, error) {
	out, err := r.output(ctx, "rev-parse", "--verify", "--end-of-options", revision)
	return strings.TrimSpace(string(out)), err
}

func (r *Repository) ReadBlob(ctx context.Context, object string) ([]byte, error) {
	return r.output(ctx, "cat-file", "blob", object)
}

func (r *Repository) output(ctx context.Context, args ...string) ([]byte, error) {
	return r.run(ctx, nil, nil, args...)
}

func (r *Repository) command(ctx context.Context, env []string, args ...string) *exec.Cmd {
	executable := r.Executable
	if executable == "" {
		executable = "git"
	}
	command := exec.CommandContext(ctx, executable, append([]string{"-c", "core.hooksPath=" + os.DevNull, "-c", "core.fsync=committed,reference", "-c", "core.fsyncMethod=fsync", "-c", "commit.gpgSign=false"}, args...)...)
	command.Dir = r.Root
	command.Env = append(repositoryEnv(), env...)
	if file, ok := ctx.Value(branchLockKey{}).(*os.File); ok {
		command.ExtraFiles = []*os.File{file}
	}
	command.WaitDelay = time.Second
	return command
}

func (r *Repository) run(ctx context.Context, input []byte, env []string, args ...string) ([]byte, error) {
	command := r.command(ctx, env, args...)
	result, err := subprocess.Run(ctx, subprocess.Spec{Tool: "git", Command: args[0], Path: command.Path, Args: command.Args[1:], Dir: command.Dir, Env: command.Env, Stdin: bytes.NewReader(input), ExtraFiles: command.ExtraFiles, WaitDelay: command.WaitDelay})
	if err != nil {
		return nil, err
	}
	if args[0] == "for-each-ref" && len(result.Stderr) != 0 {
		return nil, &subprocess.Error{Tool: "git", Command: args[0], Stderr: strings.TrimSpace(string(result.Stderr)), Cause: errors.New("reference lookup reported a warning")}
	}
	return result.Output, nil
}

func repositoryEnv() []string {
	blocked := map[string]bool{
		"GIT_DIR": true, "GIT_WORK_TREE": true, "GIT_COMMON_DIR": true,
		"GIT_INDEX_FILE": true, "GIT_NAMESPACE": true, "GIT_OBJECT_DIRECTORY": true,
		"GIT_ALTERNATE_OBJECT_DIRECTORIES": true, "GIT_CEILING_DIRECTORIES": true,
		"GIT_CONFIG_PARAMETERS": true, "GIT_CONFIG_COUNT": true,
		"GIT_EXTERNAL_DIFF": true, "GIT_DIFF_OPTS": true,
		"GIT_PAGER": true, "LC_ALL": true,
		"GIT_REPLACE_REF_BASE": true, "GIT_NO_REPLACE_OBJECTS": true,
		"GIT_NO_LAZY_FETCH": true, "GIT_TERMINAL_PROMPT": true, "GIT_OPTIONAL_LOCKS": true,
		"GIT_AUTHOR_NAME": true, "GIT_AUTHOR_EMAIL": true, "GIT_AUTHOR_DATE": true,
		"GIT_COMMITTER_NAME": true, "GIT_COMMITTER_EMAIL": true, "GIT_COMMITTER_DATE": true,
	}
	var result []string
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if !blocked[key] && !strings.HasPrefix(key, "GIT_CONFIG_KEY_") && !strings.HasPrefix(key, "GIT_CONFIG_VALUE_") {
			result = append(result, entry)
		}
	}
	return append(result, "GIT_PAGER=cat", "LC_ALL=C", "GIT_NO_REPLACE_OBJECTS=1", "GIT_NO_LAZY_FETCH=1", "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")
}
