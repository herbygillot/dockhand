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

	"golang.org/x/mod/module"

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
	// PyPI, Crates, GoGet, and GoProxy are where each answers, the
	// registry's own unless a test sets another; GoGet is the scheme and
	// host before a module path, https:// where empty, and GoProxy Go's
	// module proxy, https://proxy.golang.org where empty.
	PyPI, Crates, GoGet, GoProxy string
}

// maxAnswer bounds what a registry's answer is read of.
const maxAnswer = 8 << 20

// ErrNoSource is a registry that names no source repository for the
// project.
var ErrNoSource = errors.New("registry: no source repository named")

// Found is what a registry says of a project: the address of the source
// repository it names, and its latest version there, where the registry
// says one (Go's doesn't, here).
type Found struct {
	Source  string
	Version string
	// Directory is where a Go module is below its repository's root, as
	// gopls is in golang.org/x/tools; empty at the root. Package is the
	// module path of that root, golang.org/x/tools, which the golang
	// PortGroup's go.setup takes.
	Directory, Package string
}

// Find is what a registry name's registry says of its project: the
// address of the source repository it names (its repository, homepage, or
// source link, or the module's own path), and its latest version, which names the release where its
// repository marks none, as a project published to PyPI alone may not
// (the Vx port's field testing, 2026-10-03).
func (c Client) Find(ctx context.Context, argument string) (Found, error) {
	switch {
	case strings.HasPrefix(argument, "pypi:"):
		return c.pypi(ctx, strings.TrimPrefix(argument, "pypi:"))
	case strings.HasPrefix(argument, "crates:"):
		return c.crate(ctx, strings.TrimPrefix(argument, "crates:"))
	case strings.HasPrefix(argument, "go:"):
		path := strings.TrimPrefix(argument, "go:")
		source, root, err := c.module(ctx, path)
		directory := below(path, root)
		if err != nil || directory == "" {
			return Found{Source: source, Package: root}, err
		}
		// A module below its repository's root is tagged as
		// <directory>/v<version>, which a repository's releases rarely
		// name, so its version is the module proxy's (the docs dig's I33).
		version, err := c.latestModule(ctx, path)
		return Found{Source: source, Version: version, Directory: directory, Package: root}, err
	}
	return Found{Source: argument}, nil
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

func (c Client) pypi(ctx context.Context, project string) (Found, error) {
	if !plainName.MatchString(project) {
		return Found{}, fmt.Errorf("registry: %q is not a PyPI project name", project)
	}
	base := c.PyPI
	if base == "" {
		base = "https://pypi.org/pypi/"
	}
	var answer struct {
		Info struct {
			HomePage    string            `json:"home_page"`
			ProjectURLs map[string]string `json:"project_urls"`
			Version     string            `json:"version"`
		} `json:"info"`
	}
	if err := c.get(ctx, strings.TrimRight(base, "/")+"/"+url.PathEscape(project)+"/json", func(body io.Reader) error {
		return json.NewDecoder(body).Decode(&answer)
	}); err != nil {
		return Found{}, fmt.Errorf("pypi:%s: %w", project, err)
	}
	// The link a project names its source by, in PyPI's usual labels,
	// before any other link to a forge.
	for _, label := range []string{"Source", "Source Code", "Repository", "Code", "GitHub", "Homepage", "Home"} {
		for key, link := range answer.Info.ProjectURLs {
			if strings.EqualFold(key, label) && forge(link) {
				return Found{Source: link, Version: answer.Info.Version}, nil
			}
		}
	}
	if forge(answer.Info.HomePage) {
		return Found{Source: answer.Info.HomePage, Version: answer.Info.Version}, nil
	}
	return Found{}, fmt.Errorf("pypi:%s: %w", project, ErrNoSource)
}

func (c Client) crate(ctx context.Context, name string) (Found, error) {
	if !plainName.MatchString(name) {
		return Found{}, fmt.Errorf("registry: %q is not a crate name", name)
	}
	base := c.Crates
	if base == "" {
		base = "https://crates.io/api/v1/crates/"
	}
	var answer struct {
		Crate struct {
			Repository       string `json:"repository"`
			MaxStableVersion string `json:"max_stable_version"`
		} `json:"crate"`
	}
	if err := c.get(ctx, strings.TrimRight(base, "/")+"/"+url.PathEscape(name), func(body io.Reader) error {
		return json.NewDecoder(body).Decode(&answer)
	}); err != nil {
		return Found{}, fmt.Errorf("crates:%s: %w", name, err)
	}
	if answer.Crate.Repository == "" {
		return Found{}, fmt.Errorf("crates:%s: %w", name, ErrNoSource)
	}
	return Found{Source: answer.Crate.Repository, Version: answer.Crate.MaxStableVersion}, nil
}

// goImport is a go-import meta tag: the import prefix, the version
// control system, and the repository's root.
var goImport = regexp.MustCompile(`(?i)<meta\s+name=["']go-import["']\s+content=["']([^"']+)["']`)

// module is the repository a module path is in, and the module path of
// its root.
func (c Client) module(ctx context.Context, path string) (string, string, error) {
	if !modulePath.MatchString(path) {
		return "", "", fmt.Errorf("registry: %q is not a module path", path)
	}
	// A module path that names its forge is its repository's address.
	if parts := strings.Split(path, "/"); len(parts) >= 3 && forge("https://"+path) {
		root := strings.Join(parts[:3], "/")
		return "https://" + root, root, nil
	}
	// golang.org/x's repositories are read from their GitHub mirror,
	// github.com/golang/<repository>, which the golang PortGroup fetches
	// from (golang-1.0.tcl's go._translate_package_id).
	if parts := strings.Split(path, "/"); len(parts) >= 3 && parts[0] == "golang.org" && parts[1] == "x" {
		return "https://github.com/golang/" + parts[2], strings.Join(parts[:3], "/"), nil
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
		return "", "", fmt.Errorf("go:%s: %w", path, err)
	}
	for _, match := range goImport.FindAllStringSubmatch(string(page), -1) {
		fields := strings.Fields(match[1])
		if len(fields) == 3 && fields[1] == "git" && (path == fields[0] || strings.HasPrefix(path, fields[0]+"/")) {
			return strings.TrimSuffix(fields[2], ".git"), fields[0], nil
		}
	}
	return "", "", fmt.Errorf("go:%s: %w", path, ErrNoSource)
}

// below is the directory a module path names below its repository's
// root; empty at the root. A major version's suffix, /v2 and on, is the
// module's, not a directory, as Go's modules reference has it.
func below(path, root string) string {
	under := strings.TrimPrefix(strings.TrimPrefix(path, root), "/")
	if majorSuffix.MatchString(under) {
		return ""
	}
	if i := strings.LastIndex(under, "/"); i >= 0 && majorSuffix.MatchString(under[i+1:]) {
		under = under[:i]
	}
	return under
}

// latestModule is a module's latest version, without its v, as Go's
// module proxy answers for it (the GOPROXY protocol's $module/@latest).
func (c Client) latestModule(ctx context.Context, path string) (string, error) {
	base := c.GoProxy
	if base == "" {
		base = "https://proxy.golang.org"
	}
	escaped, err := module.EscapePath(path)
	if err != nil {
		return "", fmt.Errorf("go:%s: %w", path, err)
	}
	var answer struct {
		Version string `json:"Version"`
	}
	if err := c.get(ctx, strings.TrimRight(base, "/")+"/"+escaped+"/@latest", func(body io.Reader) error {
		return json.NewDecoder(body).Decode(&answer)
	}); err != nil {
		return "", fmt.Errorf("go:%s: the module proxy: %w", path, err)
	}
	if !strings.HasPrefix(answer.Version, "v") {
		return "", fmt.Errorf("go:%s: the module proxy names no version", path)
	}
	return strings.TrimPrefix(answer.Version, "v"), nil
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
