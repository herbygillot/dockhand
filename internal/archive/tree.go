package archive

import (
	"context"
	"slices"
	"strings"
)

// Tree is each regular file an archive holds, by its path below the
// archive's one top directory, where all of it is under one, with its
// SHA-256: what an archive's contents are, apart from its packaging, its
// compression, its members' times and owners, and the name of its root,
// which gitea-tea's regenerated archive renamed (the docs dig's I19).
func Tree(ctx context.Context, filename string) (map[string]string, error) {
	files := map[string]string{}
	top, shared := "", true
	err := Walk(ctx, filename, func(member Member) error {
		name, ok := member.Clean()
		if !ok || !member.Regular {
			return nil
		}
		first, _, nested := strings.Cut(name, "/")
		switch {
		case !nested:
			shared = false
		case top == "":
			top = first
		case first != top:
			shared = false
		}
		digest, err := member.Digest()
		if err != nil {
			return err
		}
		files[name] = digest
		return nil
	})
	if err != nil || !shared || top == "" {
		return files, err
	}
	below := make(map[string]string, len(files))
	for name, digest := range files {
		below[strings.TrimPrefix(name, top+"/")] = digest
	}
	return below, nil
}

// Differences are the paths two trees hold differently, one holds and the
// other doesn't, or whose contents differ, sorted.
func Differences(old, now map[string]string) []string {
	var differ []string
	for name, digest := range old {
		if now[name] != digest {
			differ = append(differ, name)
		}
	}
	for name := range now {
		if _, ok := old[name]; !ok {
			differ = append(differ, name)
		}
	}
	slices.Sort(differ)
	return differ
}
