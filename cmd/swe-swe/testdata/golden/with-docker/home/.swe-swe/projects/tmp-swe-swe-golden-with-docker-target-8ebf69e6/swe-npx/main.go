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
// Cache entries are self-verifying: the sha256 of the extracted binary is
// recorded alongside it at download time and re-checked before every exec,
// so a cache entry tampered with after the fact is discarded rather than
// run.
//
// Environment:
//
//	SWE_NPX_REGISTRY    registry base URL (default https://registry.npmjs.org);
//	                    must be https, except for loopback hosts
//	SWE_NPX_CACHE_DIR   cache dir (default ~/.swe-swe/npx-cache)
//	SWE_NPX_LATEST_TTL  how long a dist-tags "latest" answer is trusted
//	                    before re-checking the registry (default 15m)
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Resource caps. A hostile or broken registry must not be able to exhaust
// memory or disk: metadata and the compressed tarball are read through a
// LimitReader, and extraction stops once the unpacked total is reached.
const (
	maxMetadataBytes = 8 << 20   // packument / version doc
	maxTarballBytes  = 128 << 20 // compressed tarball
	maxUnpackedBytes = 512 << 20 // total bytes written during extraction
)

// digestFileName holds "sha256-<hex>" of bin/<name> inside a cache entry.
const digestFileName = ".swe-npx-digest"

// errNotCached means the cache has no entry at all (cold miss), as opposed
// to an entry that exists but failed verification.
var errNotCached = errors.New("not cached")

// securityError marks failures that must never be papered over by falling
// back to a cached copy: integrity mismatches, tarball shape violations,
// and blown size caps.
type securityError struct{ err error }

func (e *securityError) Error() string { return e.err.Error() }
func (e *securityError) Unwrap() error { return e.err }

func secErrf(format string, args ...any) error {
	return &securityError{err: fmt.Errorf(format, args...)}
}

type options struct {
	registry string
	cacheDir string
	ttl      time.Duration
	goos     string
	goarch   string
	// client fetches registry metadata (short timeout).
	client *http.Client
	// dlClient fetches tarballs (long timeout); falls back to client.
	dlClient *http.Client
	stderr   io.Writer
}

// downloadClient is the long-timeout client for tarball bodies. Metadata and
// payload deliberately do not share a deadline: a 5s budget that is right for
// a packument will abort a multi-megabyte binary on a slow link.
func (o options) downloadClient() *http.Client {
	if o.dlClient != nil {
		return o.dlClient
	}
	return o.client
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

// newestTrustedCached returns the newest cached version whose recorded digest
// still matches the binary on disk. Untrusted entries are skipped silently --
// they are never a candidate for offline fallback.
func newestTrustedCached(opts options, platformPkg, binName string) (string, bool) {
	for _, v := range cachedVersions(opts.cacheDir, platformPkg) {
		dir := cacheVersionDir(opts.cacheDir, platformPkg, v)
		if err := verifyCacheEntry(dir, binName); err == nil {
			return v, true
		}
	}
	return "", false
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

// fileSHA256 hashes a file, streaming (the binary can be large).
func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// writeCacheDigest records the sha256 of the extracted binary inside the
// (still temporary) cache entry, so a later exec can prove the bytes are the
// ones whose tarball integrity we verified.
func writeCacheDigest(dir, binName string) error {
	sum, err := fileSHA256(filepath.Join(dir, "bin", binName))
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, digestFileName), []byte("sha256-"+sum+"\n"), 0644)
}

// verifyCacheEntry re-checks a cache entry before it is trusted for exec:
// the binary must exist, be a plain non-group/world-writable file, and hash
// to the digest recorded at download time. Returns errNotCached when there is
// nothing there at all.
func verifyCacheEntry(dir, binName string) error {
	binPath := filepath.Join(dir, "bin", binName)
	info, err := os.Stat(binPath)
	if err != nil {
		return fmt.Errorf("%w: %v", errNotCached, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("cached %s is not a regular file", binPath)
	}
	if info.Mode().Perm()&0022 != 0 {
		return fmt.Errorf("cached %s is group/world-writable (%v)", binPath, info.Mode().Perm())
	}
	recorded, err := os.ReadFile(filepath.Join(dir, digestFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no recorded digest for %s", binPath)
		}
		return err
	}
	want, ok := strings.CutPrefix(strings.TrimSpace(string(recorded)), "sha256-")
	if !ok || want == "" {
		return fmt.Errorf("unreadable digest record for %s", binPath)
	}
	got, err := fileSHA256(binPath)
	if err != nil {
		return err
	}
	if !constantTimeEqualHex(got, want) {
		return fmt.Errorf("cached %s does not match its recorded digest", binPath)
	}
	return nil
}

func constantTimeEqualHex(a, b string) bool {
	return hashEqual([]byte(a), []byte(b))
}

// lookupLatest returns the dist-tags.latest version for platformPkg,
// memoized in the cache dir for opts.ttl. Within the TTL no network request
// is made. On registry failure it falls back to the newest verified cached
// version with a stderr note; with no such cache, it returns an error.
func lookupLatest(opts options, platformPkg, binName string) (string, error) {
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
		if v, ok := newestTrustedCached(opts, platformPkg, binName); ok {
			fmt.Fprintf(opts.stderr, "swe-npx: registry unreachable (%v); using cached %s@%s\n", err, platformPkg, v)
			return v, nil
		}
		return "", fmt.Errorf("registry unreachable (%v) and no verified cached copy of %s exists; check network or SWE_NPX_REGISTRY", err, platformPkg)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("registry has no package %s; swe-npx only supports distribute-go-bin platform packages -- use real npx for anything else", platformPkg)
	}
	if resp.StatusCode != http.StatusOK {
		if v, ok := newestTrustedCached(opts, platformPkg, binName); ok {
			fmt.Fprintf(opts.stderr, "swe-npx: registry returned %d; using cached %s@%s\n", resp.StatusCode, platformPkg, v)
			return v, nil
		}
		return "", fmt.Errorf("registry returned %d for %s and no verified cached copy exists", resp.StatusCode, platformPkg)
	}
	var doc packument
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxMetadataBytes)).Decode(&doc); err != nil {
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
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxMetadataBytes)).Decode(&doc); err != nil {
		return nil, fmt.Errorf("decoding version doc for %s@%s: %w", platformPkg, version, err)
	}
	return &doc, nil
}

// verifyIntegrity checks data against an npm integrity string
// ("sha512-<base64>"), falling back to a hex sha1 shasum when integrity is
// absent. A version doc advertising neither is rejected: an unchecksummed
// tarball is indistinguishable from a substituted one.
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
		if !hashEqual(sum[:], want) {
			return fmt.Errorf("tarball integrity mismatch (sha512)")
		}
		return nil
	}
	if d.Shasum != "" {
		sum := sha1.Sum(data)
		if !hashEqual([]byte(hex.EncodeToString(sum[:])), []byte(strings.ToLower(d.Shasum))) {
			return fmt.Errorf("tarball integrity mismatch (sha1 shasum)")
		}
		return nil
	}
	return fmt.Errorf("version doc advertises no integrity or shasum; refusing to run an unchecksummed tarball")
}

func hashEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := range a {
		v |= a[i] ^ b[i]
	}
	return v == 0
}

// checkTarballURL rejects a tarball location that would downgrade transport
// security. The registry doc is not a trust anchor, so a doc that points the
// download at a different host is worth surfacing even when allowed.
func checkTarballURL(opts options, tarball string) error {
	u, err := url.Parse(tarball)
	if err != nil {
		return secErrf("unparseable tarball URL %q: %v", tarball, err)
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !isLoopbackHost(u.Hostname()) {
			return secErrf("refusing plain-http tarball URL %q", tarball)
		}
	default:
		return secErrf("refusing tarball URL with scheme %q", u.Scheme)
	}
	if reg, err := url.Parse(opts.registry); err == nil && reg.Host != "" && u.Host != reg.Host {
		fmt.Fprintf(opts.stderr, "swe-npx: note: tarball host %s differs from registry host %s\n", u.Host, reg.Host)
	}
	return nil
}

// readCapped reads at most max bytes, treating an over-long body as hostile
// rather than truncating it.
func readCapped(r io.Reader, max int64, what string) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, secErrf("%s exceeds %d byte cap", what, max)
	}
	return data, nil
}

// downloadAndCache downloads platformPkg@version, verifies integrity,
// extracts the tarball's package/ tree into a temp dir under the cache root,
// records the binary's digest, and atomically renames it into place. Losing a
// concurrent race is fine: if the final dir already exists and verifies, ours
// is discarded and the winner used.
func downloadAndCache(opts options, platformPkg, version, binName string) (string, error) {
	doc, err := fetchVersionDoc(opts, platformPkg, version)
	if err != nil {
		return "", err
	}
	if err := checkTarballURL(opts, doc.Dist.Tarball); err != nil {
		return "", err
	}
	resp, err := opts.downloadClient().Get(doc.Dist.Tarball)
	if err != nil {
		return "", fmt.Errorf("downloading %s: %w", doc.Dist.Tarball, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("tarball download returned %d for %s", resp.StatusCode, doc.Dist.Tarball)
	}
	data, err := readCapped(resp.Body, maxTarballBytes, "tarball "+doc.Dist.Tarball)
	if err != nil {
		return "", fmt.Errorf("reading tarball: %w", err)
	}
	if err := verifyIntegrity(data, doc.Dist); err != nil {
		return "", secErrf("%s@%s: %v", platformPkg, version, err)
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
		return "", secErrf("%s@%s tarball has no package/bin/%s; not a distribute-go-bin package -- use real npx", platformPkg, version, binName)
	}
	if err := os.Chmod(binInTmp, 0755); err != nil {
		return "", fmt.Errorf("chmod binary: %w", err)
	}
	if err := writeCacheDigest(tmpDir, binName); err != nil {
		return "", fmt.Errorf("recording cache digest: %w", err)
	}

	if err := os.Rename(tmpDir, finalDir); err != nil {
		// a concurrent swe-npx may have won the race; use its copy only if
		// it verifies
		if verr := verifyCacheEntry(finalDir, binName); verr == nil {
			return cachedBinaryPath(opts.cacheDir, platformPkg, version, binName), nil
		}
		return "", fmt.Errorf("caching %s@%s: %w", platformPkg, version, err)
	}
	return cachedBinaryPath(opts.cacheDir, platformPkg, version, binName), nil
}

// extractPackageTree extracts the "package/" subtree of an npm tar.gz into
// destDir (entries outside package/ are ignored; path traversal rejected;
// total unpacked bytes capped so a decompression bomb cannot fill the disk).
func extractPackageTree(tgz []byte, destDir string) error {
	gz, err := gzip.NewReader(bytes.NewReader(tgz))
	if err != nil {
		return fmt.Errorf("gunzip: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	remaining := int64(maxUnpackedBytes)
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
			return secErrf("tar entry escapes package tree: %q", hdr.Name)
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
			// never group/world-writable, never setuid, always readable
			mode := os.FileMode(hdr.Mode)&0755 | 0644
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
			if err != nil {
				return err
			}
			n, err := io.Copy(f, io.LimitReader(tr, remaining+1))
			if err != nil {
				f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
			if n > remaining {
				return secErrf("unpacked size exceeds %d byte cap", maxUnpackedBytes)
			}
			remaining -= n
		default:
			// symlinks etc. have no place in a distribute-go-bin tarball
			continue
		}
	}
}

// resolve maps pkg@version to a verified cached executable path, downloading
// on miss. When the requested version cannot be downloaded and the caller did
// not pin a version, it falls back to the newest verified cached copy so an
// offline box can still start.
func resolve(opts options, pkg, version string) (string, error) {
	platformPkg, err := platformPackage(pkg, opts.goos, opts.goarch)
	if err != nil {
		return "", err
	}
	binName := binaryName(pkg)
	pinned := version != "" && version != "latest"

	if !pinned {
		v, err := lookupLatest(opts, platformPkg, binName)
		if err != nil {
			return "", err
		}
		version = v
	}

	verDir := cacheVersionDir(opts.cacheDir, platformPkg, version)
	switch err := verifyCacheEntry(verDir, binName); {
	case err == nil:
		return cachedBinaryPath(opts.cacheDir, platformPkg, version, binName), nil
	case errors.Is(err, errNotCached):
		// cold miss: download below
	default:
		// entry exists but cannot be trusted -- discard it rather than exec it
		fmt.Fprintf(opts.stderr, "swe-npx: discarding untrusted cache entry (%v)\n", err)
		os.RemoveAll(verDir)
	}

	binPath, dlErr := downloadAndCache(opts, platformPkg, version, binName)
	if dlErr == nil {
		return binPath, nil
	}
	var secErr *securityError
	if pinned || errors.As(dlErr, &secErr) {
		return "", dlErr
	}
	if v, ok := newestTrustedCached(opts, platformPkg, binName); ok {
		fmt.Fprintf(opts.stderr, "swe-npx: cannot fetch %s@%s (%v); using cached %s@%s\n", platformPkg, version, dlErr, platformPkg, v)
		return cachedBinaryPath(opts.cacheDir, platformPkg, v, binName), nil
	}
	return "", dlErr
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

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// validateRegistry refuses a registry URL that would let an attacker with env
// access, or a passive network attacker, substitute the binaries we exec.
func validateRegistry(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("SWE_NPX_REGISTRY is not a valid URL (%q): %w", raw, err)
	}
	if u.Host == "" {
		return "", fmt.Errorf("SWE_NPX_REGISTRY has no host: %q", raw)
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !isLoopbackHost(u.Hostname()) {
			return "", fmt.Errorf("SWE_NPX_REGISTRY must use https (got %q); plain http is allowed for loopback hosts only", raw)
		}
	default:
		return "", fmt.Errorf("SWE_NPX_REGISTRY must be an http(s) URL, got %q", raw)
	}
	return strings.TrimSuffix(raw, "/"), nil
}

func defaultOptions() (options, error) {
	registry := os.Getenv("SWE_NPX_REGISTRY")
	if registry == "" {
		registry = "https://registry.npmjs.org"
	}
	registry, err := validateRegistry(registry)
	if err != nil {
		return options{}, err
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
		registry: registry,
		cacheDir: cacheDir,
		ttl:      ttl,
		goos:     runtime.GOOS,
		goarch:   runtime.GOARCH,
		client:   &http.Client{Timeout: 15 * time.Second},
		dlClient: &http.Client{Timeout: 5 * time.Minute},
		stderr:   os.Stderr,
	}, nil
}

func main() {
	opts, err := defaultOptions()
	if err == nil {
		err = run(opts, os.Args[1:])
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "swe-npx: %v\n", err)
		os.Exit(1)
	}
}
