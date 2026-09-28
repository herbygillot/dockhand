package archives

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
)

// MacPortsMirror is MacPorts' distfiles mirror, which keeps each archive a
// port shipped under the port's dist_subdir, whatever upstream serves now.
const MacPortsMirror = "https://distfiles.macports.org/"

// Shipped is an archive as its Portfile's checksums declare it: the one
// MacPorts shipped.
type Shipped struct {
	Download
	// Mirror is true when upstream no longer served it as declared, and
	// the mirror did.
	Mirror bool
}

// Shipped fetches each of sources as the Portfile's checksums declare it.
// That is from upstream, or, where upstream now serves something else
// under its name, or nothing, from the client's mirror, under the port's
// dist_subdir: its name, unless the Portfile sets one, as a stealth update
// does to keep the earlier archive apart. An archive the Portfile declares
// no checksums for, or that neither has as declared, fails.
func (s *Store) Shipped(ctx context.Context, info macports.PortInfo, sources []Source) ([]Shipped, error) {
	declared := Declared(info.Options["checksums"])
	subdir := strings.Trim(info.Options["dist_subdir"], "/")
	if subdir == "" {
		subdir = info.Name
	}
	var shipped []Shipped
	for _, source := range sources {
		want, ok := declared[source.Name]
		if !ok && len(declared) == 1 && len(sources) == 1 {
			want, ok = declared[""]
		}
		if !ok {
			return shipped, fmt.Errorf("%s has no checksums in the Portfile", source.Name)
		}
		download, err := s.Fetch(ctx, info, source)
		if err == nil && !Differs(want, download.Checksum) {
			shipped = append(shipped, Shipped{Download: download})
			continue
		}
		discard(download)
		if ctx.Err() != nil {
			return shipped, ctx.Err()
		}
		if s.client.Mirror == "" {
			return shipped, fmt.Errorf("upstream no longer serves %s as the Portfile's checksums describe it", source.Name)
		}
		mirrored, err := s.Fetch(ctx, info, Source{Name: source.Name, URL: strings.TrimRight(s.client.Mirror, "/") + "/" + subdir + "/" + url.PathEscape(source.Name)})
		if err != nil || Differs(want, mirrored.Checksum) {
			discard(mirrored)
			return shipped, fmt.Errorf("neither upstream nor MacPorts' mirror has %s as the Portfile's checksums describe it", source.Name)
		}
		shipped = append(shipped, Shipped{Download: mirrored, Mirror: true})
	}
	return shipped, nil
}

// discard removes a kept archive that isn't the one wanted.
func discard(download Download) {
	if download.Path != "" {
		_ = os.Remove(download.Path)
	}
}

// Declared reads an evaluated checksums option, by distfile: a lone
// archive's may be declared without its name, under "".
func Declared(value string) map[string]portfile.Checksum {
	declared := map[string]portfile.Checksum{}
	name := ""
	words := strings.Fields(value)
	for i := 0; i < len(words); i++ {
		word := words[i]
		if !portfile.IsChecksumKind(word) || i+1 == len(words) {
			name = word
			continue
		}
		i++
		sum := declared[name]
		sum.Name = name
		switch word {
		case "rmd160":
			sum.RMD160 = words[i]
		case "sha256":
			sum.SHA256 = words[i]
		case "size":
			sum.Size, _ = strconv.ParseInt(words[i], 10, 64)
		case "md5":
			sum.MD5 = words[i]
		case "sha1":
			sum.SHA1 = words[i]
		}
		declared[name] = sum
	}
	return declared
}

// Differs reports whether an archive's sums differ from what a Portfile
// declares, in what it declares of sha256, rmd160, and size. Legacy md5 or
// sha1 alone can't tell.
func Differs(declared, sum portfile.Checksum) bool {
	return declared.SHA256 != "" && declared.SHA256 != sum.SHA256 ||
		declared.RMD160 != "" && declared.RMD160 != sum.RMD160 ||
		declared.Size != 0 && declared.Size != sum.Size
}
