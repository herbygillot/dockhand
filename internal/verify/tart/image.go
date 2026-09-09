package tart

import (
	"os"
	"path/filepath"
	"strings"
)

// tartHome is where tart keeps its OCI cache. It is a variable so a
// test can point it somewhere it owns.
var tartHome = func() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".tart"), nil
}

// ImageDigest resolves a tagged image reference to the digest tart
// actually has for it, "" when nothing here can say.
//
// THE TAG IS NOT THE IMAGE. Bases are built from
// ghcr.io/cirruslabs/macos-<release>-vanilla:latest, and `latest` is a
// moving target — so a verdict that said "built on Tahoe" named a
// release and not an image, and two passes months apart could disagree
// about what that meant with nothing in either record to show it.
//
// It is read from the OCI cache rather than asked of the registry
// because the question is WHICH IMAGE IS ON THIS DISK, not which one
// the tag points at now. Those are the same only until upstream
// publishes again, and the base a verdict was earned on is the one that
// was pulled — possibly weeks earlier. Asking the registry at verify
// time would confidently record the wrong digest.
//
// tart stores each pulled tag as a symlink beside the digest directory
// it resolves to, so the answer is the link's target. That is tart's
// layout and not its contract, which is why a failure here is silence:
// "I could not find out" is answered as nothing at all, never as a
// digest somebody made up (rule 7).
func ImageDigest(ref string) string {
	repo, tag, ok := strings.Cut(ref, ":")
	if !ok || strings.Contains(ref, "@") {
		return "" // already digest-pinned, or not a tagged reference
	}
	home, err := tartHome()
	if err != nil {
		return ""
	}
	target, err := os.Readlink(filepath.Join(home, "cache", "OCIs", filepath.FromSlash(repo), tag))
	if err != nil {
		return ""
	}
	digest := filepath.Base(target)
	if !strings.HasPrefix(digest, "sha256:") {
		return ""
	}
	return digest
}

// Pinned is a reference naming exactly one image: the repository and
// the digest, with the tag dropped because it is the part that moves.
// It answers "" when the digest is unknown, so a caller writing it down
// records nothing rather than a half-name.
func Pinned(ref string) string {
	digest := ImageDigest(ref)
	if digest == "" {
		return ""
	}
	repo, _, _ := strings.Cut(ref, ":")
	return repo + "@" + digest
}

// basePath is where a base image's provenance is kept: the exact image
// the base was built from, beside the worker attributions and for the
// same reason — it is a fact about this machine's disk that no record
// in a checkout can hold, because a base outlives every change that
// runs on it and is shared by all of them.
func basePath(vm string) (string, error) {
	dir, err := cacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "bases", vm+".image"), nil
}

// NoteBase records the image a base was built from, at the moment it is
// built and never after.
//
// THE MOMENT MATTERS. `latest` moves, so resolving it at verify time
// would name whatever upstream published most recently rather than the
// image the base on this disk actually descends from — confidently, and
// wrongly, for every base older than the last release. Written here,
// the digest is the one that was pulled.
//
// Best-effort: a base whose provenance could not be written reads back
// as unknown, which is the same answer an older base gives, and both
// are honest.
func NoteBase(vm, pinned string) {
	if pinned == "" {
		return
	}
	path, err := basePath(vm)
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = os.WriteFile(path, []byte(pinned), 0o644)
}

// BaseImage is the image a base was built from, "" when nothing here
// recorded one — a base provisioned before this was written, or a write
// that failed. It is never guessed from the tag: see NoteBase.
func BaseImage(vm string) string {
	path, err := basePath(vm)
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// ForgetBase drops a base's provenance when the base itself goes.
func ForgetBase(vm string) {
	if path, err := basePath(vm); err == nil {
		_ = os.Remove(path)
	}
}
