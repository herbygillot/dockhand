package archive

import (
	"context"
	"slices"
	"strings"
)

// Tree is each regular file an archive holds, by its path below the
// archive's one top directory, where it has one (Tops), with its
// SHA-256: what an archive's contents are, apart from its packaging, its
// compression, its members' times and owners, and the name of its root,
// which gitea-tea's regenerated archive renamed (the docs dig's I19).
func Tree(ctx context.Context, filename string) (map[string]string, error) {
	files := map[string]string{}
	var tops Tops
	err := Walk(ctx, filename, func(member Member) error {
		name, ok := member.Clean()
		if !ok || !member.Regular {
			return nil
		}
		tops.Add(name)
		digest, err := member.Digest()
		if err != nil {
			return err
		}
		files[name] = digest
		return nil
	})
	top, one := tops.One()
	if err != nil || !one {
		return files, err
	}
	below := make(map[string]string, len(files))
	for name, digest := range files {
		if rest, ok := strings.CutPrefix(name, top+"/"); ok {
			name = rest
		}
		below[name] = digest
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
