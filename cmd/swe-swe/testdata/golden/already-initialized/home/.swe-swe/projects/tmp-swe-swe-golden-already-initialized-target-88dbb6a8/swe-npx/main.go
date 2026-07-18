// swe-npx resolves a distribute-go-bin-style npm package to its platform
// binary, caches it under a user-level dir, and execs it.
//
// It is NOT a general npx replacement: it only handles dependency-free
// packages published with per-platform optionalDependencies (e.g.
// @choonkeat/md-serve -> @choonkeat/md-serve-linux-x64) whose tarball
// carries the real binary at package/bin/<name>. Anything else (real JS
// packages like @playwright/mcp) must keep using real npx.
//
// Usage:
//
//	swe-npx [-y] <@scope/name>[@<version>|@latest] [args...]
//
// -y is accepted and ignored for drop-in compatibility with the npx call
// sites it replaces. Everything after the package token is passed verbatim
// to the resolved binary via exec (no wrapper process left behind).
//
// Environment:
//
//	SWE_NPX_REGISTRY    registry base URL (default https://registry.npmjs.org)
//	SWE_NPX_CACHE_DIR   cache dir (default ~/.swe-swe/npx-cache)
//	SWE_NPX_LATEST_TTL  how long a dist-tags "latest" answer is trusted
//	                    before re-checking the registry (default 15m)
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha1"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type options struct {
	registry string
	cacheDir string
	ttl      time.Duration
	goos     string
	goarch   string
	client   *http.Client
	stderr   io.Writer
}

// execFn is syscall.Exec behind a var so tests can stub it.
var execFn = syscall.Exec

// platformPackage maps a distribute-go-bin main package to its per-platform
// binary package: @scope/name + linux/amd64 -> @scope/name-linux-x64.
func platformPackage(pkg, goos, goarch string) (string, error) {
	var osPart string
	switch goos {
	case "linux":
		osPart = "linux"
	case "darwin":
		osPart = "darwin"
	default:
		return "", fmt.Errorf("unsupported OS %q (swe-npx supports linux and darwin only)", goos)
	}
	var archPart string
	switch goarch {
	case "amd64":
		archPart = "x64"
	case "arm64":
		archPart = "arm64"
	default:
		return "", fmt.Errorf("unsupported architecture %q (swe-npx supports amd64 and arm64 only)", goarch)
	}
	return fmt.Sprintf("%s-%s-%s", pkg, osPart, archPart), nil
}

// escapePackage encodes a scoped package name for use in a registry URL path:
// @scope/name -> @scope%2Fname (npm registry convention).
func escapePackage(pkg string) string {
	return strings.Replace(pkg, "/", "%2F", 1)
}

// binaryName is the tarball binary basename: @scope/name -> name.
func binaryName(pkg string) string {
	if i := strings.LastIndex(pkg, "/"); i >= 0 {
		return pkg[i+1:]
	}
	return pkg
}

// parseArgs implements: swe-npx [-y] <@scope/name>[@version] [args...].
// -y is swallowed for npx compatibility. The version suffix is split off the
// package token (the leading @ of the scope is not a version separator).
func parseArgs(args []string) (pkg, version string, rest []string, err error) {
	i := 0
	for i < len(args) && args[i] == "-y" {
		i++
	}
	if i >= len(args) {
		return "", "", nil, fmt.Errorf("usage: swe-npx [-y] <@scope/name>[@version|@latest] [args...]")
	}
	token := args[i]
	rest = args[i+1:]
	if rest == nil {
		rest = []string{}
	}
	// split version off: look for '@' after the first char (scoped names
	// start with '@')
	if at := strings.LastIndex(token, "@"); at > 0 {
		pkg = token[:at]
		version = token[at+1:]
	} else {
		pkg = token
	}
	if pkg == "" {
		return "", "", nil, fmt.Errorf("invalid package token %q", token)
	}
	return pkg, version, rest, nil
}

// dist is the relevant slice of an npm version document.
type dist struct {
	Tarball   string `json:"tarball"`
	Integrity string `json:"integrity"`
	Shasum    string `json:"shasum"`
}

type versionDoc struct {
	Version string `json:"version"`
	Dist    dist   `json:"dist"`
}

type packument struct {
	DistTags map[string]string     `json:"dist-tags"`
	Versions map[string]versionDoc `json:"versions"`
}

// cachePkgDir is the directory holding all cached versions and the latest
// memo for one platform package: <cache>/@scope (the package base name keys
// the entries inside it).
func cacheVersionDir(cacheDir, platformPkg, version string) string {
	return filepath.Join(cacheDir, filepath.FromSlash(platformPkg)+"@"+version)
}

func cachedBinaryPath(cacheDir, platformPkg, version, binName string) string {
	return filepath.Join(cacheVersionDir(cacheDir, platformPkg, version), "bin", binName)
}

func memoPath(cacheDir, platformPkg string) string {
	return filepath.Join(cacheDir, filepath.FromSlash(platformPkg)+".latest")
}

// cachedVersions lists versions of platformPkg present in the cache, newest
// first (numeric dot-segment comparison, lexicographic tiebreak).
func cachedVersions(cacheDir, platformPkg string) []string {
	dir := filepath.Dir(filepath.Join(cacheDir, filepath.FromSlash(platformPkg)))
	base := filepath.Base(filepath.FromSlash(platformPkg))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var versions []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, base+"@") {
			versions = append(versions, strings.TrimPrefix(name, base+"@"))
		}
	}
	sort.Slice(versions, func(i, j int) bool {
		return versionLess(versions[j], versions[i])
	})
	return versions
}

// versionLess reports a < b, comparing dot-separated numeric segments.
func versionLess(a, b string) bool {
	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		an, aerr := strconv.Atoi(as[i])
		bn, berr := strconv.Atoi(bs[i])
		if aerr == nil && berr == nil {
			if an != bn {
				return an < bn
			}
			continue
		}
		if as[i] != bs[i] {
			return as[i] < bs[i]
		}
	}
	return len(as) < len(bs)
}

// lookupLatest returns the dist-tags.latest version for platformPkg,
// memoized in the cache dir for opts.ttl. Within the TTL no network request
// is made. On registry failure it falls back to the newest cached version
// with a stderr note; with no cache either, it returns an error.
func lookupLatest(opts options, platformPkg string) (string, error) {
	memo := memoPath(opts.cacheDir, platformPkg)
	if info, err := os.Stat(memo); err == nil && opts.ttl > 0 && time.Since(info.ModTime()) < opts.ttl {
		if data, err := os.ReadFile(memo); err == nil {
			if v := strings.TrimSpace(string(data)); v != "" {
				return v, nil
			}
		}
	}

	url := opts.registry + "/" + escapePackage(platformPkg)
	resp, err := opts.client.Get(url)
	if err != nil {
		if fallback := cachedVersions(opts.cacheDir, platformPkg); len(fallback) > 0 {
			fmt.Fprintf(opts.stderr, "swe-npx: registry unreachable (%v); using cached %s@%s\n", err, platformPkg, fallback[0])
			return fallback[0], nil
		}
		return "", fmt.Errorf("registry unreachable (%v) and no cached copy of %s exists; check network or SWE_NPX_REGISTRY", err, platformPkg)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("registry has no package %s; swe-npx only supports distribute-go-bin platform packages -- use real npx for anything else", platformPkg)
	}
	if resp.StatusCode != http.StatusOK {
		if fallback := cachedVersions(opts.cacheDir, platformPkg); len(fallback) > 0 {
			fmt.Fprintf(opts.stderr, "swe-npx: registry returned %d; using cached %s@%s\n", resp.StatusCode, platformPkg, fallback[0])
			return fallback[0], nil
		}
		return "", fmt.Errorf("registry returned %d for %s and no cached copy exists", resp.StatusCode, platformPkg)
	}
	var doc packument
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return "", fmt.Errorf("decoding packument for %s: %w", platformPkg, err)
	}
	latest := doc.DistTags["latest"]
	if latest == "" {
		return "", fmt.Errorf("packument for %s has no dist-tags.latest", platformPkg)
	}
	if err := os.MkdirAll(filepath.Dir(memo), 0755); err == nil {
		os.WriteFile(memo, []byte(latest+"\n"), 0644)
	}
	return latest, nil
}

// fetchVersionDoc gets the version document (tarball URL + integrity) for an
// explicit version, without consulting dist-tags.
func fetchVersionDoc(opts options, platformPkg, version string) (*versionDoc, error) {
	url := opts.registry + "/" + escapePackage(platformPkg) + "/" + version
	resp, err := opts.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("registry unreachable fetching %s@%s: %w", platformPkg, version, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("registry has no %s@%s; swe-npx only supports distribute-go-bin platform packages -- use real npx for anything else", platformPkg, version)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry returned %d for %s@%s", resp.StatusCode, platformPkg, version)
	}
	var doc versionDoc
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, fmt.Errorf("decoding version doc for %s@%s: %w", platformPkg, version, err)
	}
	return &doc, nil
}

// verifyIntegrity checks data against an npm integrity string
// ("sha512-<base64>"), falling back to a hex sha1 shasum when integrity is
// absent. Empty both -> accepted (registry did not advertise a checksum).
func verifyIntegrity(data []byte, d dist) error {
	if d.Integrity != "" {
		parts := strings.SplitN(d.Integrity, "-", 2)
		if len(parts) != 2 || parts[0] != "sha512" {
			return fmt.Errorf("unsupported integrity format %q", d.Integrity)
		}
		want, err := base64.StdEncoding.DecodeString(parts[1])
		if err != nil {
			return fmt.Errorf("bad integrity encoding: %w", err)
		}
		sum := sha512.Sum512(data)
		if !hmacEqual(sum[:], want) {
			return fmt.Errorf("tarball integrity mismatch (sha512)")
		}
		return nil
	}
	if d.Shasum != "" {
		sum := sha1.Sum(data)
		if hex.EncodeToString(sum[:]) != strings.ToLower(d.Shasum) {
			return fmt.Errorf("tarball integrity mismatch (sha1 shasum)")
		}
	}
	return nil
}

func hmacEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := range a {
		v |= a[i] ^ b[i]
	}
	return v == 0
}

// downloadAndCache downloads platformPkg@version, verifies integrity,
// extracts the tarball's package/ tree into a temp dir under the cache root,
// and atomically renames it into place. Losing a concurrent race is fine:
// if the final dir already exists, ours is discarded and the winner used.
func downloadAndCache(opts options, platformPkg, version, binName string) (string, error) {
	doc, err := fetchVersionDoc(opts, platformPkg, version)
	if err != nil {
		return "", err
	}
	resp, err := opts.client.Get(doc.Dist.Tarball)
	if err != nil {
		return "", fmt.Errorf("downloading %s: %w", doc.Dist.Tarball, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("tarball download returned %d for %s", resp.StatusCode, doc.Dist.Tarball)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading tarball: %w", err)
	}
	if err := verifyIntegrity(data, doc.Dist); err != nil {
		return "", fmt.Errorf("%s@%s: %w", platformPkg, version, err)
	}

	finalDir := cacheVersionDir(opts.cacheDir, platformPkg, version)
	if err := os.MkdirAll(filepath.Dir(finalDir), 0755); err != nil {
		return "", fmt.Errorf("creating cache dir: %w", err)
	}
	tmpDir, err := os.MkdirTemp(filepath.Dir(finalDir), ".tmp-"+filepath.Base(finalDir)+"-")
	if err != nil {
		return "", fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := extractPackageTree(data, tmpDir); err != nil {
		return "", fmt.Errorf("extracting %s@%s: %w", platformPkg, version, err)
	}
	binInTmp := filepath.Join(tmpDir, "bin", binName)
	if _, err := os.Stat(binInTmp); err != nil {
		return "", fmt.Errorf("%s@%s tarball has no package/bin/%s; not a distribute-go-bin package -- use real npx", platformPkg, version, binName)
	}
	if err := os.Chmod(binInTmp, 0755); err != nil {
		return "", fmt.Errorf("chmod binary: %w", err)
	}

	if err := os.Rename(tmpDir, finalDir); err != nil {
		// a concurrent swe-npx won the race; use its copy
		if _, statErr := os.Stat(finalDir); statErr == nil {
			return cachedBinaryPath(opts.cacheDir, platformPkg, version, binName), nil
		}
		return "", fmt.Errorf("caching %s@%s: %w", platformPkg, version, err)
	}
	return cachedBinaryPath(opts.cacheDir, platformPkg, version, binName), nil
}

// extractPackageTree extracts the "package/" subtree of an npm tar.gz into
// destDir (entries outside package/ are ignored; path traversal rejected).
func extractPackageTree(tgz []byte, destDir string) error {
	gz, err := gzip.NewReader(bytes.NewReader(tgz))
	if err != nil {
		return fmt.Errorf("gunzip: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("tar: %w", err)
		}
		rel, ok := strings.CutPrefix(hdr.Name, "package/")
		if !ok || rel == "" {
			continue
		}
		clean := filepath.Clean(filepath.FromSlash(rel))
		if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
			return fmt.Errorf("tar entry escapes package tree: %q", hdr.Name)
		}
		target := filepath.Join(destDir, clean)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode)&0777|0644)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
		default:
			// symlinks etc. have no place in a distribute-go-bin tarball
			continue
		}
	}
}

// resolve maps pkg@version to a cached executable path, downloading on miss.
func resolve(opts options, pkg, version string) (string, error) {
	platformPkg, err := platformPackage(pkg, opts.goos, opts.goarch)
	if err != nil {
		return "", err
	}
	binName := binaryName(pkg)

	if version == "" || version == "latest" {
		v, err := lookupLatest(opts, platformPkg)
		if err != nil {
			return "", err
		}
		version = v
	}

	binPath := cachedBinaryPath(opts.cacheDir, platformPkg, version, binName)
	if _, err := os.Stat(binPath); err == nil {
		return binPath, nil
	}
	return downloadAndCache(opts, platformPkg, version, binName)
}

// run parses argv, resolves the package, and execs the binary.
func run(opts options, args []string) error {
	pkg, version, rest, err := parseArgs(args)
	if err != nil {
		return err
	}
	binPath, err := resolve(opts, pkg, version)
	if err != nil {
		return err
	}
	return execFn(binPath, append([]string{binPath}, rest...), os.Environ())
}

func defaultOptions() options {
	registry := os.Getenv("SWE_NPX_REGISTRY")
	if registry == "" {
		registry = "https://registry.npmjs.org"
	}
	cacheDir := os.Getenv("SWE_NPX_CACHE_DIR")
	if cacheDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}
		cacheDir = filepath.Join(home, ".swe-swe", "npx-cache")
	}
	ttl := 15 * time.Minute
	if v := os.Getenv("SWE_NPX_LATEST_TTL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			ttl = d
		}
	}
	return options{
		registry: strings.TrimSuffix(registry, "/"),
		cacheDir: cacheDir,
		ttl:      ttl,
		goos:     runtime.GOOS,
		goarch:   runtime.GOARCH,
		client:   &http.Client{Timeout: 5 * time.Second},
		stderr:   os.Stderr,
	}
}

func main() {
	if err := run(defaultOptions(), os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "swe-npx: %v\n", err)
		os.Exit(1)
	}
}
