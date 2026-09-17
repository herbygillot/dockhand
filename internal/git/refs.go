package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"unicode/utf8"
)

var (
	ErrRefConflict        = errors.New("git: ref precondition failed")
	ErrRefUpdateUncertain = errors.New("git: ref update outcome is uncertain")
	errSymbolicRef        = errors.New("git: expected a direct ref")
)

type RefValue struct {
	Exists bool
	Object string
}

type RefChange struct {
	Name     string
	Expected RefValue
	Desired  RefValue
}

type RefConflict struct {
	Name     string
	Expected RefValue
	Actual   RefValue
}

func (e *RefConflict) Error() string {
	return fmt.Sprintf("%v: %s: expected %+v, found %+v", ErrRefConflict, e.Name, e.Expected, e.Actual)
}

func (e *RefConflict) Unwrap() error { return ErrRefConflict }

// ValidRefName accepts a complete literal Git ref, including its refs/ namespace.
func ValidRefName(name string) bool { return utf8.ValidString(name) && validRefName(name) }

func validRefName(name string) bool {
	if !strings.HasPrefix(name, "refs/") || strings.ContainsAny(name, " ~^:?*[\\\x7f") || strings.Contains(name, "..") || strings.Contains(name, "@{") || strings.HasSuffix(name, ".") {
		return false
	}
	for _, c := range name {
		if c < 32 {
			return false
		}
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	return true
}

func (r *Repository) ReadRef(ctx context.Context, name string) (RefValue, error) {
	if !validRefName(name) {
		return RefValue{}, fmt.Errorf("git: invalid ref name %q", name)
	}
	refs, err := r.readRefs(ctx, name)
	if err != nil {
		return RefValue{}, err
	}
	if ref, ok := refs[name]; ok {
		return ref, nil
	}
	_, err = r.output(ctx, "show-ref", "--exists", name)
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 2 && ctx.Err() == nil {
		return RefValue{}, nil
	}
	if err != nil {
		return RefValue{}, err
	}
	return RefValue{}, fmt.Errorf("git: %s exists but could not be read as a direct ref", name)
}

func (r *Repository) ReadRefs(ctx context.Context, prefix string) (map[string]RefValue, error) {
	if !strings.HasSuffix(prefix, "/") || !validRefName(prefix+"entry") {
		return nil, fmt.Errorf("git: invalid ref prefix %q", prefix)
	}
	return r.readRefs(ctx, prefix)
}

func (r *Repository) readRefs(ctx context.Context, pattern string) (map[string]RefValue, error) {
	out, err := r.output(ctx, "for-each-ref", "--format=%(refname)%00%(objectname)%00%(symref)", pattern)
	if err != nil {
		return nil, err
	}
	refs := make(map[string]RefValue)
	for _, line := range bytes.Split(bytes.TrimSuffix(out, []byte{'\n'}), []byte{'\n'}) {
		fields := bytes.Split(line, []byte{0})
		if len(fields) == 1 && len(fields[0]) == 0 {
			continue
		}
		if len(fields) != 3 {
			return nil, fmt.Errorf("git: invalid ref output")
		}
		name, object := string(fields[0]), string(fields[1])
		if len(fields[2]) != 0 {
			return nil, fmt.Errorf("%w: %s", errSymbolicRef, name)
		}
		if !validRefName(name) || !ValidObjectID(object) {
			return nil, fmt.Errorf("git: invalid ref output for %s", name)
		}
		refs[name] = RefValue{Exists: true, Object: object}
	}
	return refs, nil
}

func (r *Repository) UpdateRefs(ctx context.Context, changes []RefChange) error {
	if len(changes) == 0 {
		return nil
	}
	var input bytes.Buffer
	input.WriteString("start\x00")
	seen := make(map[string]bool, len(changes))
	for _, change := range changes {
		if !validRefName(change.Name) || seen[change.Name] {
			return fmt.Errorf("git: invalid or duplicate ref %q", change.Name)
		}
		seen[change.Name] = true
		for _, value := range []RefValue{change.Expected, change.Desired} {
			if value.Exists && !ValidObjectID(value.Object) || !value.Exists && value.Object != "" {
				return fmt.Errorf("git: invalid value for %s", change.Name)
			}
		}
		input.WriteString("option no-deref\x00")
		switch {
		case change.Expected == change.Desired:
			fmt.Fprintf(&input, "verify %s\x00%s\x00", change.Name, change.Expected.Object)
		case !change.Expected.Exists:
			fmt.Fprintf(&input, "create %s\x00%s\x00", change.Name, change.Desired.Object)
		case !change.Desired.Exists:
			fmt.Fprintf(&input, "delete %s\x00%s\x00", change.Name, change.Expected.Object)
		default:
			fmt.Fprintf(&input, "update %s\x00%s\x00%s\x00", change.Name, change.Desired.Object, change.Expected.Object)
		}
	}
	input.WriteString("prepare\x00commit\x00")
	_, err := r.run(ctx, input.Bytes(), []string{"GIT_COMMITTER_NAME=Dockhand", "GIT_COMMITTER_EMAIL=dockhand@localhost"}, "update-ref", "--stdin", "-z", "--create-reflog", "-m", "dockhand ref transaction")
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return fmt.Errorf("%w: %w", ErrRefUpdateUncertain, err)
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() < 0 {
		return fmt.Errorf("%w: %w", ErrRefUpdateUncertain, err)
	}
	for _, change := range changes {
		actual, readErr := r.ReadRef(ctx, change.Name)
		if readErr != nil {
			return errors.Join(err, readErr)
		}
		if actual != change.Expected {
			return errors.Join(&RefConflict{Name: change.Name, Expected: change.Expected, Actual: actual}, err)
		}
	}
	return err
}

type Push struct {
	Remote         string
	Branch         string
	Commit         string
	ExpectedRemote RefValue
}
