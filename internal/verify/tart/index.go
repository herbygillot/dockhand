package tart

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/git"
	"github.com/herbygillot/dockhand/v2/internal/record"
)

const portIndexName = "PortIndex"
const quickIndexName = "PortIndex.quick"
const portIndexReconciliationCommits = 10
const maxPortIndexBytes = 128 << 20

func defaultPortIndexURL(platform record.Platform) (string, error) {
	for _, value := range []string{platform.OS, platform.Version, platform.Architecture} {
		if value == "" || strings.ContainsAny(value, "/\\\x00\r\n\t ") {
			return "", fmt.Errorf("tart: complete platform required for PortIndex mirror")
		}
	}
	profile := strings.Join([]string{platform.OS, platform.Version, platform.Architecture}, "_")
	return "https://ftp.fau.de/macports/release/tarballs/PortIndex_" + profile + "/PortIndex", nil
}

func resolvePortIndexTool(ctx context.Context, c Config) (Config, error) {
	path := c.PortIndexExecutable
	if path == "" {
		var err error
		path, err = exec.LookPath("portindex")
		if err != nil {
			return c, fmt.Errorf("tart: locating portindex: %w", err)
		}
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return c, err
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return c, fmt.Errorf("tart: locating portindex: %w", err)
	}
	file, err := os.Open(abs)
	if err != nil {
		return c, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return c, err
	}
	if !info.Mode().IsRegular() || info.Mode()&0111 == 0 {
		return c, fmt.Errorf("tart: portindex executable is not an executable regular file")
	}
	hash := sha256.New()
	buffer := make([]byte, 1<<20)
	for {
		if err = ctx.Err(); err != nil {
			return c, err
		}
		count, readErr := file.Read(buffer)
		if count > 0 {
			_, _ = hash.Write(buffer[:count])
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return c, readErr
		}
	}
	identity := "sha256:" + hex.EncodeToString(hash.Sum(nil))
	if c.PortIndexDigest != "" && c.PortIndexDigest != identity {
		return c, fmt.Errorf("tart: portindex executable changed after the build was accepted")
	}
	c.PortIndexExecutable, c.PortIndexDigest = abs, identity
	return c, nil
}

func stagePortIndex(ctx context.Context, repo *git.Repository, source record.Source, platform record.Platform, c Config, root string, client *http.Client) error {
	resolved, err := resolvePortIndexTool(ctx, c)
	if err != nil {
		return err
	}
	profile := digest([]byte(resolved.PortIndexDigest + "\x00" + resolved.PortIndexURL + "\x00" + platform.OS + "\x00" + platform.Version + "\x00" + platform.Architecture))
	cacheRoot := filepath.Join(resolved.ArtifactDirectory, "indexes", profile)
	guard, err := acquire(ctx, filepath.Join(cacheRoot, "index.lock"))
	if err != nil {
		return err
	}
	defer guard.Close()
	entry, temporary, err := ensurePortIndex(ctx, repo, source, platform, resolved, cacheRoot, root, client)
	if err != nil {
		return err
	}
	if temporary {
		defer os.RemoveAll(filepath.Dir(entry))
	}
	for _, name := range []string{portIndexName, quickIndexName} {
		if err := copyIndexFile(filepath.Join(entry, name), filepath.Join(root, name)); err != nil {
			return err
		}
	}
	return nil
}

func ensurePortIndex(ctx context.Context, repo *git.Repository, source record.Source, platform record.Platform, c Config, cacheRoot, targetRoot string, client *http.Client) (string, bool, error) {
	target := filepath.Join(cacheRoot, string(source.Tree))
	if validIndexEntry(target) {
		return target, false, nil
	}
	seedTree, err := sourceBaseTree(ctx, repo, source)
	if err != nil {
		return "", false, err
	}
	if seedTree == "" || seedTree == string(source.Tree) {
		if err := buildPortIndex(ctx, c, platform, targetRoot, target, "", nil, true); err != nil {
			return "", false, err
		}
		return target, false, nil
	}
	seed := filepath.Join(cacheRoot, seedTree)
	if !validIndexEntry(seed) {
		snapshot, err := repo.Materialize(ctx, seedTree)
		if err != nil {
			return "", false, err
		}
		if c.PortIndexURL != "" {
			mirror, changed, mirrorErr := mirroredPortIndex(ctx, repo, source, seedTree, c.PortIndexURL, cacheRoot, client)
			if mirrorErr == nil {
				err = buildPortIndex(ctx, c, platform, snapshot.Root, seed, mirror, changed, false)
				_ = os.RemoveAll(mirror)
			}
		}
		if !validIndexEntry(seed) {
			err = buildPortIndex(ctx, c, platform, snapshot.Root, seed, "", nil, false)
		}
		closeErr := snapshot.Close()
		if err != nil {
			return "", false, errors.Join(err, closeErr)
		}
		if closeErr != nil {
			return "", false, closeErr
		}
	}
	paths, err := repo.ChangedPaths(ctx, seedTree, string(source.Tree))
	if err != nil {
		return "", false, err
	}
	candidateRoot, err := os.MkdirTemp(cacheRoot, ".candidate-")
	if err != nil {
		return "", false, err
	}
	target = filepath.Join(candidateRoot, "index")
	if requiresFullIndex(paths) {
		err = buildPortIndex(ctx, c, platform, targetRoot, target, "", nil, true)
	} else {
		err = buildPortIndex(ctx, c, platform, targetRoot, target, seed, paths, true)
	}
	if err != nil {
		return "", false, errors.Join(err, os.RemoveAll(candidateRoot))
	}
	return target, true, nil
}

func mirroredPortIndex(ctx context.Context, repo *git.Repository, source record.Source, baseTree, address, cacheRoot string, client *http.Client) (string, []string, error) {
	if source.Base == "" {
		return "", nil, fmt.Errorf("tart: source base commit required for mirror reconciliation")
	}
	typ, err := repo.ObjectType(ctx, string(source.Base))
	if err != nil || typ != "commit" {
		return "", nil, errors.Join(err, fmt.Errorf("tart: source base commit required for mirror reconciliation"))
	}
	ancestor, err := repo.Resolve(ctx, fmt.Sprintf("%s~%d", source.Base, portIndexReconciliationCommits))
	if err != nil {
		return "", nil, err
	}
	changed, err := repo.ChangedPaths(ctx, ancestor, baseTree)
	if err != nil {
		return "", nil, err
	}
	directory, err := os.MkdirTemp(cacheRoot, ".mirror-")
	if err != nil {
		return "", nil, err
	}
	if err = downloadPortIndex(ctx, client, address, filepath.Join(directory, portIndexName)); err != nil {
		return "", nil, errors.Join(err, os.RemoveAll(directory))
	}
	return directory, changed, nil
}

func downloadPortIndex(ctx context.Context, client *http.Client, address, destination string) error {
	parsed, err := url.Parse(address)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return fmt.Errorf("tart: invalid PortIndex mirror URL")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return err
	}
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.Request.URL.Scheme != "https" {
		return fmt.Errorf("tart: PortIndex mirror redirected to an insecure URL")
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("tart: PortIndex mirror returned HTTP %d", response.StatusCode)
	}
	if response.ContentLength > maxPortIndexBytes {
		return fmt.Errorf("tart: mirrored PortIndex exceeds %d bytes", maxPortIndexBytes)
	}
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(file, io.LimitReader(response.Body, maxPortIndexBytes+1))
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written == 0 || written > maxPortIndexBytes {
		return fmt.Errorf("tart: mirrored PortIndex is empty or exceeds %d bytes", maxPortIndexBytes)
	}
	return nil
}

func sourceBaseTree(ctx context.Context, repo *git.Repository, source record.Source) (string, error) {
	if source.Base == "" {
		return "", nil
	}
	typ, err := repo.ObjectType(ctx, string(source.Base))
	if err != nil {
		return "", err
	}
	if typ == "tree" {
		return string(source.Base), nil
	}
	if typ != "commit" {
		return "", fmt.Errorf("tart: source base is not a commit or tree")
	}
	trees, err := repo.CommitTrees(ctx, []string{string(source.Base)})
	return trees[string(source.Base)], err
}

func requiresFullIndex(paths []string) bool {
	for _, name := range paths {
		if name == "_resources" || strings.HasPrefix(name, "_resources/") {
			return true
		}
	}
	return false
}

func buildPortIndex(ctx context.Context, c Config, platform record.Platform, sourceRoot, destination, seed string, changed []string, strict bool) (err error) {
	if err = os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
		return err
	}
	temp, err := os.MkdirTemp(filepath.Dir(destination), ".index-")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, os.RemoveAll(temp)) }()
	configRoot, err := os.MkdirTemp(filepath.Dir(destination), ".portindex-config-")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, os.RemoveAll(configRoot)) }()
	args := []string{"-q"}
	if seed == "" {
		args = append(args, "-f")
	} else {
		if err = copyIndexFile(filepath.Join(seed, portIndexName), filepath.Join(temp, portIndexName)); err != nil {
			return err
		}
		quick := filepath.Join(seed, quickIndexName)
		if _, statErr := os.Stat(quick); statErr == nil {
			if err = copyIndexFile(quick, filepath.Join(temp, quickIndexName)); err != nil {
				return err
			}
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return statErr
		}
		indexInfo, err := os.Stat(filepath.Join(temp, portIndexName))
		if err != nil {
			return err
		}
		if err = setPortfileTimes(sourceRoot, changed, indexInfo.ModTime()); err != nil {
			return err
		}
	}
	if strict {
		args = append(args, "-e")
	}
	args = append(args, "-p", strings.Join([]string{platform.OS, platform.Version, platform.Architecture}, "_"), "-o", temp, sourceRoot)
	configuration := filepath.Join(configRoot, "macports.conf")
	sources := filepath.Join(configRoot, "sources.conf")
	variants := filepath.Join(configRoot, "variants.conf")
	portdb := filepath.Join(configRoot, "portdb")
	if err = os.MkdirAll(filepath.Join(portdb, "registry"), 0700); err != nil {
		return err
	}
	if err = os.WriteFile(sources, []byte((&url.URL{Scheme: "file", Path: sourceRoot}).String()+" [default,nosync]\n"), 0600); err != nil {
		return err
	}
	if err = os.WriteFile(variants, nil, 0600); err != nil {
		return err
	}
	configurationText := fmt.Sprintf("sources_conf %s\nvariants_conf %s\nportdbpath %s\n", sources, variants, portdb)
	if err = os.WriteFile(configuration, []byte(configurationText), 0600); err != nil {
		return err
	}
	command := exec.CommandContext(ctx, c.PortIndexExecutable, args...)
	command.Dir = sourceRoot
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "PORTSRC=") && !strings.HasPrefix(entry, "LC_ALL=") {
			command.Env = append(command.Env, entry)
		}
	}
	command.Env = append(command.Env, "PORTSRC="+configuration, "LC_ALL=C")
	output, runErr := command.CombinedOutput()
	if runErr != nil {
		return fmt.Errorf("tart: portindex: %w: %s", errors.Join(ctx.Err(), runErr), strings.TrimSpace(string(output)))
	}
	if !validIndexEntry(temp) {
		return fmt.Errorf("tart: portindex produced an incomplete index")
	}
	if err = os.RemoveAll(destination); err != nil {
		return err
	}
	if err = os.Rename(temp, destination); err != nil {
		return err
	}
	return nil
}

func setPortfileTimes(root string, changed []string, indexTime time.Time) error {
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type().IsRegular() && entry.Name() == "Portfile" {
			return os.Chtimes(path, indexTime, indexTime)
		}
		return nil
	}); err != nil {
		return err
	}
	newer := indexTime.Add(time.Second)
	seen := map[string]bool{}
	for _, name := range changed {
		parts := strings.Split(filepath.ToSlash(name), "/")
		if len(parts) < 3 || parts[0] == "_resources" {
			continue
		}
		portfile := filepath.Join(root, filepath.FromSlash(parts[0]+"/"+parts[1]+"/Portfile"))
		if seen[portfile] {
			continue
		}
		seen[portfile] = true
		if err := os.Chtimes(portfile, newer, newer); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func validIndexEntry(directory string) bool {
	for _, name := range []string{portIndexName, quickIndexName} {
		info, err := os.Lstat(filepath.Join(directory, name))
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			return false
		}
	}
	return true
}

func copyIndexFile(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return errors.Join(err, fmt.Errorf("tart: cached PortIndex is not a regular file"))
	}
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr == nil {
		copyErr = closeErr
	}
	if copyErr == nil {
		copyErr = os.Chtimes(destination, info.ModTime(), info.ModTime())
	}
	return copyErr
}
