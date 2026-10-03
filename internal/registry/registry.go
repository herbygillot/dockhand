// Package registry finds where a project named in a language's registry
// keeps its source, so create can start from "crates:ripgrep" as from a
// repository's address. Each registry is asked by its documented interface
// alone: PyPI's JSON API (https://docs.pypi.org/api/json/), crates.io's
// API (https://crates.io/data-access), whose policy asks for a User-Agent,
// and Go's remote import paths, a module path that names its host or a
// go-import meta tag the path answers ?go-get=1 with
// (https://go.dev/ref/mod#vcs-find).
package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/herbygillot/dockhand/internal/fetch"
)

// Prefixes are the registries a name may be given from.
var Prefixes = []string{"pypi:", "crates:", "go:"}

// Named reports whether an argument names a project in a registry rather
// than by its address.
func Named(argument string) bool {
	for _, prefix := range Prefixes {
		if strings.HasPrefix(argument, prefix) {
			return true
		}
	}
	return false
}

// Client asks the registries.
type Client struct {
	HTTP *http.Client
	// PyPI, Crates, and GoGet are where each answers, the registry's own
	// unless a test sets another; GoGet is the scheme and host before a
	// module path, https:// where empty.
	PyPI, Crates, GoGet string
}

// maxAnswer bounds what a registry's answer is read of.
const maxAnswer = 8 << 20

// ErrNoSource is a registry that names no source repository for the
// project.
var ErrNoSource = errors.New("registry: no source repository named")

// ErrSubdirectory is a module path below its repository's root: a module
// in a subdirectory, whose port builds there, which create can't yet start
// from (Codex's review of 386ac2cc, finding 3).
var ErrSubdirectory = errors.New("registry: the module is below its repository's root")

// Source is the address of the source repository a registry name's
// project names: its repository, homepage, or source link, or the
// module's own path.
func (c Client) Source(ctx context.Context, argument string) (string, error) {
	switch {
	case strings.HasPrefix(argument, "pypi:"):
		return c.pypi(ctx, strings.TrimPrefix(argument, "pypi:"))
	case strings.HasPrefix(argument, "crates:"):
		return c.crate(ctx, strings.TrimPrefix(argument, "crates:"))
	case strings.HasPrefix(argument, "go:"):
		return c.module(ctx, strings.TrimPrefix(argument, "go:"))
	}
	return argument, nil
}

func (c Client) get(ctx context.Context, address string, into func(io.Reader) error) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", fetch.UserAgent)
	response, err := fetch.Open(c.HTTP, request, maxAnswer)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	return into(response.Body)
}

func (c Client) pypi(ctx context.Context, project string) (string, error) {
	if !plainName.MatchString(project) {
		return "", fmt.Errorf("registry: %q is not a PyPI project name", project)
	}
	base := c.PyPI
	if base == "" {
		base = "https://pypi.org/pypi/"
	}
	var answer struct {
		Info struct {
			HomePage    string            `json:"home_page"`
			ProjectURLs map[string]string `json:"project_urls"`
		} `json:"info"`
	}
	if err := c.get(ctx, strings.TrimRight(base, "/")+"/"+url.PathEscape(project)+"/json", func(body io.Reader) error {
		return json.NewDecoder(body).Decode(&answer)
	}); err != nil {
		return "", fmt.Errorf("pypi:%s: %w", project, err)
	}
	// The link a project names its source by, in PyPI's usual labels,
	// before any other link to a forge.
	for _, label := range []string{"Source", "Source Code", "Repository", "Code", "GitHub", "Homepage", "Home"} {
		for key, link := range answer.Info.ProjectURLs {
			if strings.EqualFold(key, label) && forge(link) {
				return link, nil
			}
		}
	}
	if forge(answer.Info.HomePage) {
		return answer.Info.HomePage, nil
	}
	return "", fmt.Errorf("pypi:%s: %w", project, ErrNoSource)
}

func (c Client) crate(ctx context.Context, name string) (string, error) {
	if !plainName.MatchString(name) {
		return "", fmt.Errorf("registry: %q is not a crate name", name)
	}
	base := c.Crates
	if base == "" {
		base = "https://crates.io/api/v1/crates/"
	}
	var answer struct {
		Crate struct {
			Repository string `json:"repository"`
		} `json:"crate"`
	}
	if err := c.get(ctx, strings.TrimRight(base, "/")+"/"+url.PathEscape(name), func(body io.Reader) error {
		return json.NewDecoder(body).Decode(&answer)
	}); err != nil {
		return "", fmt.Errorf("crates:%s: %w", name, err)
	}
	if answer.Crate.Repository == "" {
		return "", fmt.Errorf("crates:%s: %w", name, ErrNoSource)
	}
	return answer.Crate.Repository, nil
}

// goImport is a go-import meta tag: the import prefix, the version
// control system, and the repository's root.
var goImport = regexp.MustCompile(`(?i)<meta\s+name=["']go-import["']\s+content=["']([^"']+)["']`)

func (c Client) module(ctx context.Context, path string) (string, error) {
	if !modulePath.MatchString(path) {
		return "", fmt.Errorf("registry: %q is not a module path", path)
	}
	// A module path that names its forge is its repository's address.
	if parts := strings.Split(path, "/"); len(parts) >= 3 && forge("https://"+path) {
		root := strings.Join(parts[:3], "/")
		if err := atRoot(path, root); err != nil {
			return "", err
		}
		return "https://" + root, nil
	}
	base := c.GoGet
	if base == "" {
		base = "https://"
	}
	var page []byte
	if err := c.get(ctx, base+path+"?go-get=1", func(body io.Reader) error {
		var err error
		page, err = io.ReadAll(body)
		return err
	}); err != nil {
		return "", fmt.Errorf("go:%s: %w", path, err)
	}
	for _, match := range goImport.FindAllStringSubmatch(string(page), -1) {
		fields := strings.Fields(match[1])
		if len(fields) == 3 && fields[1] == "git" && (path == fields[0] || strings.HasPrefix(path, fields[0]+"/")) {
			if err := atRoot(path, fields[0]); err != nil {
				return "", err
			}
			return strings.TrimSuffix(fields[2], ".git"), nil
		}
	}
	return "", fmt.Errorf("go:%s: %w", path, ErrNoSource)
}

// atRoot refuses a module path below its repository's root, naming the
// subdirectory. A major version's suffix, /v2 and on, is the module's,
// not a directory, as Go's modules reference has it.
func atRoot(path, root string) error {
	below := strings.TrimPrefix(strings.TrimPrefix(path, root), "/")
	if majorSuffix.MatchString(below) {
		below = ""
	} else if i := strings.LastIndex(below, "/"); i >= 0 && majorSuffix.MatchString(below[i+1:]) {
		below = below[:i]
	}
	if below == "" {
		return nil
	}
	return fmt.Errorf("go:%s is in %s's subdirectory %s, and create starts a port only from a module at its repository's root: %w", path, root, below, ErrSubdirectory)
}

var (
	majorSuffix = regexp.MustCompile(`^v[2-9][0-9]*$|^v[1-9][0-9]+$`)
	plainName   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	modulePath  = regexp.MustCompile(`^[a-z0-9.-]+\.[a-z]{2,}(/[A-Za-z0-9._~-]+)*$`)
)

// forge reports an address on a forge whose repositories create reads or
// will: GitHub, GitLab, Codeberg.
func forge(address string) bool {
	u, err := url.Parse(address)
	return err == nil && (u.Host == "github.com" || u.Host == "gitlab.com" || u.Host == "codeberg.org")
}
