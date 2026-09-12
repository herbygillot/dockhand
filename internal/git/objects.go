package git

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type TreeEntry struct {
	Name   string
	Mode   uint32
	Type   string
	Object string
}

type Signature struct {
	Name  string
	Email string
	When  time.Time
}

type Commit struct {
	Tree      string
	Parents   []string
	Message   string
	Author    Signature
	Committer Signature
}

func ValidObjectID(object string) bool {
	if len(object) != 40 && len(object) != 64 {
		return false
	}
	if strings.Trim(object, "0") == "" {
		return false
	}
	for _, c := range object {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func (r *Repository) ObjectType(ctx context.Context, object string) (string, error) {
	if !ValidObjectID(object) {
		return "", fmt.Errorf("git: invalid object ID %q", object)
	}
	out, err := r.output(ctx, "cat-file", "-t", object)
	return strings.TrimSpace(string(out)), err
}

func (r *Repository) ObjectTypes(ctx context.Context, objects []string) (map[string]string, error) {
	types := make(map[string]string, len(objects))
	if len(objects) == 0 {
		return types, nil
	}
	for _, object := range objects {
		if !ValidObjectID(object) {
			return nil, fmt.Errorf("git: invalid object ID %q", object)
		}
	}
	input := []byte(strings.Join(objects, "\n") + "\n")
	out, err := r.run(ctx, input, nil, "cat-file", "--batch-check=%(objectname) %(objecttype)")
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(lines) != len(objects) {
		return nil, fmt.Errorf("git: incomplete object lookup")
	}
	for i, line := range lines {
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[0] != objects[i] {
			return nil, fmt.Errorf("git: invalid object lookup %q", line)
		}
		switch fields[1] {
		case "blob", "tree", "commit", "tag":
			types[fields[0]] = fields[1]
		default:
			return nil, fmt.Errorf("git: cannot read object %s: %s", fields[0], fields[1])
		}
	}
	return types, nil
}

// CommitTrees resolves immutable commit IDs in one Git invocation. Checking the
// original object as well as its tree rejects tags and trees that Git can peel.
func (r *Repository) CommitTrees(ctx context.Context, commits []string) (map[string]string, error) {
	trees := make(map[string]string, len(commits))
	if len(commits) == 0 {
		return trees, nil
	}
	queries := make([]string, 0, 2*len(commits))
	for _, commit := range commits {
		if !ValidObjectID(commit) {
			return nil, fmt.Errorf("git: invalid commit ID %q", commit)
		}
		queries = append(queries, commit, commit+"^{tree}")
	}
	out, err := r.run(ctx, []byte(strings.Join(queries, "\n")+"\n"), nil, "cat-file", "--batch-check=%(objectname) %(objecttype)")
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(lines) != len(queries) {
		return nil, fmt.Errorf("git: incomplete commit/tree lookup")
	}
	for i, commit := range commits {
		original, tree := strings.Fields(lines[2*i]), strings.Fields(lines[2*i+1])
		if len(original) != 2 || original[0] != commit || original[1] != "commit" {
			return nil, fmt.Errorf("git: cannot read commit %s: %q", commit, lines[2*i])
		}
		if len(tree) != 2 || !ValidObjectID(tree[0]) || tree[1] != "tree" {
			return nil, fmt.Errorf("git: cannot resolve tree for %s: %q", commit, lines[2*i+1])
		}
		trees[commit] = tree[0]
	}
	return trees, nil
}

func (r *Repository) WriteBlob(ctx context.Context, data []byte) (string, error) {
	out, err := r.run(ctx, data, nil, "hash-object", "-w", "--stdin")
	return objectResult(out, err)
}

func (r *Repository) ReadTree(ctx context.Context, object string) ([]TreeEntry, error) {
	if !ValidObjectID(object) {
		return nil, fmt.Errorf("git: invalid object ID %q", object)
	}
	out, err := r.output(ctx, "ls-tree", "-z", object)
	if err != nil {
		return nil, err
	}
	var entries []TreeEntry
	for len(out) != 0 {
		record, rest, ok := bytes.Cut(out, []byte{0})
		if !ok {
			return nil, fmt.Errorf("git: unterminated tree entry")
		}
		out = rest
		metadata, name, ok := bytes.Cut(record, []byte{'\t'})
		fields := strings.Fields(string(metadata))
		if !ok || len(fields) != 3 {
			return nil, fmt.Errorf("git: invalid tree entry %q", record)
		}
		mode, err := strconv.ParseUint(fields[0], 8, 32)
		if err != nil || !ValidObjectID(fields[2]) {
			return nil, fmt.Errorf("git: invalid tree entry %q", record)
		}
		entries = append(entries, TreeEntry{Name: string(name), Mode: uint32(mode), Type: fields[1], Object: fields[2]})
	}
	return entries, nil
}

func (r *Repository) WriteTree(ctx context.Context, entries []TreeEntry) (string, error) {
	var input bytes.Buffer
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if entry.Name == "" || entry.Name == "." || entry.Name == ".." || strings.ContainsAny(entry.Name, "/\x00") || seen[entry.Name] || !ValidObjectID(entry.Object) {
			return "", fmt.Errorf("git: invalid or duplicate tree entry %q", entry.Name)
		}
		seen[entry.Name] = true
		valid := entry.Type == "blob" && (entry.Mode == 0o100644 || entry.Mode == 0o100755 || entry.Mode == 0o120000) || entry.Type == "tree" && entry.Mode == 0o40000 || entry.Type == "commit" && entry.Mode == 0o160000
		if !valid {
			return "", fmt.Errorf("git: invalid mode/type for tree entry %q", entry.Name)
		}
		fmt.Fprintf(&input, "%06o %s %s\t%s\x00", entry.Mode, entry.Type, entry.Object, entry.Name)
	}
	out, err := r.run(ctx, input.Bytes(), nil, "mktree", "-z")
	return objectResult(out, err)
}

func (r *Repository) WriteCommit(ctx context.Context, commit Commit) (string, error) {
	if !ValidObjectID(commit.Tree) {
		return "", fmt.Errorf("git: invalid commit tree %q", commit.Tree)
	}
	args := []string{"commit-tree", commit.Tree}
	for _, parent := range commit.Parents {
		if !ValidObjectID(parent) {
			return "", fmt.Errorf("git: invalid parent %q", parent)
		}
		args = append(args, "-p", parent)
	}
	var env []string
	for _, identity := range []struct {
		prefix string
		value  Signature
	}{{"GIT_AUTHOR", commit.Author}, {"GIT_COMMITTER", commit.Committer}} {
		signature := identity.value
		if signature.Name == "" || signature.Email == "" || signature.When.IsZero() || strings.ContainsAny(signature.Name+signature.Email, "\x00\r\n<>") {
			return "", fmt.Errorf("git: invalid commit signature")
		}
		env = append(env, identity.prefix+"_NAME="+signature.Name, identity.prefix+"_EMAIL="+signature.Email, identity.prefix+"_DATE="+signature.When.Format(time.RFC3339))
	}
	out, err := r.run(ctx, []byte(commit.Message), env, append(args, "-F", "-")...)
	return objectResult(out, err)
}

func objectResult(out []byte, err error) (string, error) {
	if err != nil {
		return "", err
	}
	object := strings.TrimSpace(string(out))
	if !ValidObjectID(object) {
		return "", fmt.Errorf("git: invalid object ID returned: %q", object)
	}
	return object, nil
}
