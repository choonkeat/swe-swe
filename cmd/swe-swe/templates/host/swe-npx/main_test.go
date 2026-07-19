package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// --- helpers ---

// tarGz builds an npm-style tar.gz whose entries are the given path->content
// pairs (paths are tarball-internal, e.g. "package/bin/md-serve").
func tarGz(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{
			Name: name,
			Mode: 0644,
			Size: int64(len(content)),
		}); err != nil {
			t.Fatalf("tar header: %v", err)
		}
		if _, err := tw.Write(content); err != nil {
			t.Fatalf("tar write: %v", err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}

// zeroReader is an endless source of zero bytes, for building a compression
// bomb without materialising it.
type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

func sha512Integrity(data []byte) string {
	sum := sha512.Sum512(data)
	return "sha512-" + base64.StdEncoding.EncodeToString(sum[:])
}

// fakeRegistry serves packuments, version docs, and tarballs for one platform
// package, counting every request.
type fakeRegistry struct {
	t        *testing.T
	srv      *httptest.Server
	requests atomic.Int64
	// escaped platform package name, e.g. "@choonkeat%2Fmd-serve-linux-x64"
	escapedPkg string
	latest     string
	// version -> tarball bytes; integrity computed from bytes unless corrupt
	tarballs map[string][]byte
	// if set, integrity advertised for this version is deliberately wrong
	corruptIntegrity map[string]bool
	// if set, the version doc advertises no integrity and no shasum
	noIntegrity map[string]bool
	// if true, respond 404 to every metadata request
	notFound bool
}

func newFakeRegistry(t *testing.T, escapedPkg, latest string, tarballs map[string][]byte) *fakeRegistry {
	t.Helper()
	f := &fakeRegistry{
		t:                t,
		escapedPkg:       escapedPkg,
		latest:           latest,
		tarballs:         tarballs,
		corruptIntegrity: map[string]bool{},
		noIntegrity:      map[string]bool{},
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeRegistry) distFor(version string) map[string]any {
	data := f.tarballs[version]
	integrity := sha512Integrity(data)
	if f.corruptIntegrity[version] {
		integrity = sha512Integrity(append([]byte("corrupt"), data...))
	}
	d := map[string]any{"tarball": f.srv.URL + "/tarballs/" + version + ".tgz"}
	if !f.noIntegrity[version] {
		d["integrity"] = integrity
	}
	return d
}

func (f *fakeRegistry) handle(w http.ResponseWriter, r *http.Request) {
	f.requests.Add(1)
	path := r.URL.EscapedPath()
	if strings.HasPrefix(path, "/tarballs/") {
		version := strings.TrimSuffix(strings.TrimPrefix(path, "/tarballs/"), ".tgz")
		data, ok := f.tarballs[version]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(data)
		return
	}
	if f.notFound {
		http.NotFound(w, r)
		return
	}
	switch path {
	case "/" + f.escapedPkg:
		versions := map[string]any{}
		for v := range f.tarballs {
			versions[v] = map[string]any{"version": v, "dist": f.distFor(v)}
		}
		json.NewEncoder(w).Encode(map[string]any{
			"dist-tags": map[string]string{"latest": f.latest},
			"versions":  versions,
		})
	default:
		// version doc: /<escapedPkg>/<version>
		prefix := "/" + f.escapedPkg + "/"
		if strings.HasPrefix(path, prefix) {
			v := strings.TrimPrefix(path, prefix)
			if _, ok := f.tarballs[v]; !ok {
				http.NotFound(w, r)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"version": v, "dist": f.distFor(v)})
			return
		}
		http.NotFound(w, r)
	}
}

func testOpts(t *testing.T, registryURL string) options {
	t.Helper()
	return options{
		registry: registryURL,
		cacheDir: t.TempDir(),
		ttl:      15 * time.Minute,
		goos:     "linux",
		goarch:   "amd64",
		client:   &http.Client{Timeout: 5 * time.Second},
		stderr:   &bytes.Buffer{},
	}
}

// seedCache writes a cache entry the way downloadAndCache would: the binary
// plus the digest record that makes it trusted for exec.
func seedCache(t *testing.T, cacheDir, platformPkg, version, binName string, content []byte) string {
	t.Helper()
	dir := cacheVersionDir(cacheDir, platformPkg, version)
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0755); err != nil {
		t.Fatal(err)
	}
	binPath := filepath.Join(dir, "bin", binName)
	if err := os.WriteFile(binPath, content, 0755); err != nil {
		t.Fatal(err)
	}
	if err := writeCacheDigest(dir, binName); err != nil {
		t.Fatal(err)
	}
	return binPath
}

func readFileT(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

// --- 1. platform package derivation + escaping ---

func TestPlatformPackage(t *testing.T) {
	cases := []struct {
		pkg, goos, goarch, want string
	}{
		{"@choonkeat/md-serve", "linux", "amd64", "@choonkeat/md-serve-linux-x64"},
		{"@choonkeat/md-serve", "linux", "arm64", "@choonkeat/md-serve-linux-arm64"},
		{"@choonkeat/agent-chat", "darwin", "amd64", "@choonkeat/agent-chat-darwin-x64"},
		{"@choonkeat/whiteboard-mcp", "darwin", "arm64", "@choonkeat/whiteboard-mcp-darwin-arm64"},
	}
	for _, c := range cases {
		got, err := platformPackage(c.pkg, c.goos, c.goarch)
		if err != nil {
			t.Errorf("platformPackage(%q,%q,%q): %v", c.pkg, c.goos, c.goarch, err)
			continue
		}
		if got != c.want {
			t.Errorf("platformPackage(%q,%q,%q) = %q, want %q", c.pkg, c.goos, c.goarch, got, c.want)
		}
	}
	if _, err := platformPackage("@choonkeat/md-serve", "windows", "amd64"); err == nil {
		t.Errorf("platformPackage windows: want error, got nil")
	}
}

func TestRegistryEscape(t *testing.T) {
	got := escapePackage("@choonkeat/md-serve-linux-x64")
	want := "@choonkeat%2Fmd-serve-linux-x64"
	if got != want {
		t.Errorf("escapePackage = %q, want %q", got, want)
	}
}

func TestBinaryName(t *testing.T) {
	if got := binaryName("@choonkeat/md-serve"); got != "md-serve" {
		t.Errorf("binaryName = %q, want md-serve", got)
	}
	if got := binaryName("@choonkeat/agent-chat"); got != "agent-chat" {
		t.Errorf("binaryName = %q, want agent-chat", got)
	}
}

// --- 10. arg parsing: -y swallowed, version split, passthrough ---

func TestParseArgs(t *testing.T) {
	cases := []struct {
		in          []string
		pkg, ver    string
		rest        []string
		expectError bool
	}{
		{in: []string{"-y", "@choonkeat/md-serve@latest", "--port", "8080"},
			pkg: "@choonkeat/md-serve", ver: "latest", rest: []string{"--port", "8080"}},
		{in: []string{"@choonkeat/md-serve@1.2.3", "serve", "."},
			pkg: "@choonkeat/md-serve", ver: "1.2.3", rest: []string{"serve", "."}},
		{in: []string{"-y", "@choonkeat/agent-chat"},
			pkg: "@choonkeat/agent-chat", ver: "", rest: []string{}},
		{in: []string{}, expectError: true},
		{in: []string{"-y"}, expectError: true},
	}
	for _, c := range cases {
		pkg, ver, rest, err := parseArgs(c.in)
		if c.expectError {
			if err == nil {
				t.Errorf("parseArgs(%v): want error, got nil", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseArgs(%v): %v", c.in, err)
			continue
		}
		if pkg != c.pkg || ver != c.ver || fmt.Sprint(rest) != fmt.Sprint(c.rest) {
			t.Errorf("parseArgs(%v) = (%q,%q,%v), want (%q,%q,%v)", c.in, pkg, ver, rest, c.pkg, c.ver, c.rest)
		}
	}
}

// --- 2. explicit version: no dist-tags lookup, direct download ---

func TestExplicitVersionSkipsDistTags(t *testing.T) {
	binContent := []byte("#!fake md-serve 1.0.0")
	reg := newFakeRegistry(t, "@choonkeat%2Fmd-serve-linux-x64", "9.9.9", map[string][]byte{
		"1.0.0": tarGz(t, map[string][]byte{"package/bin/md-serve": binContent}),
	})
	opts := testOpts(t, reg.srv.URL)

	binPath, err := resolve(opts, "@choonkeat/md-serve", "1.0.0")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got := readFileT(t, binPath); !bytes.Equal(got, binContent) {
		t.Errorf("binary content mismatch: %q", got)
	}
	info, err := os.Stat(binPath)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm()&0111 == 0 {
		t.Errorf("binary not executable: %v", info.Mode())
	}
	// exactly 2 requests: version doc + tarball; no packument (dist-tags) hit
	if n := reg.requests.Load(); n != 2 {
		t.Errorf("expected 2 registry requests (version doc + tarball), got %d", n)
	}
}

// --- 3. latest: dist-tags consulted, memo written, TTL respected ---

func TestLatestMemoizedWithinTTL(t *testing.T) {
	binContent := []byte("#!fake md-serve 1.2.3")
	reg := newFakeRegistry(t, "@choonkeat%2Fmd-serve-linux-x64", "1.2.3", map[string][]byte{
		"1.2.3": tarGz(t, map[string][]byte{"package/bin/md-serve": binContent}),
	})
	opts := testOpts(t, reg.srv.URL)

	binPath, err := resolve(opts, "@choonkeat/md-serve", "latest")
	if err != nil {
		t.Fatalf("resolve latest: %v", err)
	}
	if got := readFileT(t, binPath); !bytes.Equal(got, binContent) {
		t.Errorf("binary content mismatch: %q", got)
	}
	firstCount := reg.requests.Load()
	if firstCount == 0 {
		t.Fatalf("expected registry to be consulted on cold latest lookup")
	}

	// memo file must exist somewhere under the cache dir
	memoFound := false
	filepath.Walk(opts.cacheDir, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.HasSuffix(path, ".latest") {
			memoFound = true
		}
		return nil
	})
	if !memoFound {
		t.Errorf("no .latest memo file written under cache dir")
	}

	// second call within TTL: zero additional network
	binPath2, err := resolve(opts, "@choonkeat/md-serve", "latest")
	if err != nil {
		t.Fatalf("resolve latest (warm): %v", err)
	}
	if binPath2 != binPath {
		t.Errorf("warm resolve returned %q, want %q", binPath2, binPath)
	}
	if n := reg.requests.Load(); n != firstCount {
		t.Errorf("warm latest resolve hit the registry: %d -> %d requests", firstCount, n)
	}

	// expired TTL: registry consulted again
	opts.ttl = 0
	if _, err := resolve(opts, "@choonkeat/md-serve", "latest"); err != nil {
		t.Fatalf("resolve latest (expired ttl): %v", err)
	}
	if n := reg.requests.Load(); n == firstCount {
		t.Errorf("expired-TTL latest resolve did not re-check the registry")
	}
}

// --- 4. integrity verification ---

func TestCorruptIntegrityFatal(t *testing.T) {
	binContent := []byte("#!fake md-serve 1.0.0")
	reg := newFakeRegistry(t, "@choonkeat%2Fmd-serve-linux-x64", "1.0.0", map[string][]byte{
		"1.0.0": tarGz(t, map[string][]byte{"package/bin/md-serve": binContent}),
	})
	reg.corruptIntegrity["1.0.0"] = true
	opts := testOpts(t, reg.srv.URL)

	_, err := resolve(opts, "@choonkeat/md-serve", "1.0.0")
	if err == nil {
		t.Fatalf("resolve with corrupt integrity: want error, got nil")
	}
	if !strings.Contains(err.Error(), "integrity") {
		t.Errorf("error should mention integrity: %v", err)
	}
	// nothing cached
	entries := []string{}
	filepath.Walk(opts.cacheDir, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			entries = append(entries, path)
		}
		return nil
	})
	if len(entries) != 0 {
		t.Errorf("corrupt download left files in cache: %v", entries)
	}
}

// --- 5. cache hit: zero HTTP ---

func TestCacheHitZeroRequests(t *testing.T) {
	reg := newFakeRegistry(t, "@choonkeat%2Fmd-serve-linux-x64", "1.0.0", map[string][]byte{})
	opts := testOpts(t, reg.srv.URL)

	binContent := []byte("#!cached md-serve")
	seedCache(t, opts.cacheDir, "@choonkeat/md-serve-linux-x64", "1.0.0", "md-serve", binContent)

	binPath, err := resolve(opts, "@choonkeat/md-serve", "1.0.0")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got := readFileT(t, binPath); !bytes.Equal(got, binContent) {
		t.Errorf("binary content mismatch: %q", got)
	}
	if n := reg.requests.Load(); n != 0 {
		t.Errorf("cache hit made %d registry requests, want 0", n)
	}
}

// --- 6. registry down + cache populated: newest cached version + stderr note ---

func TestRegistryDownFallsBackToNewestCached(t *testing.T) {
	opts := testOpts(t, "http://127.0.0.1:1") // nothing listens here
	opts.client = &http.Client{Timeout: 200 * time.Millisecond}
	stderr := &bytes.Buffer{}
	opts.stderr = stderr

	for _, v := range []string{"1.0.0", "1.10.0", "1.9.0"} {
		seedCache(t, opts.cacheDir, "@choonkeat/md-serve-linux-x64", v, "md-serve", []byte("v"+v))
	}

	binPath, err := resolve(opts, "@choonkeat/md-serve", "latest")
	if err != nil {
		t.Fatalf("resolve with registry down + cache: %v", err)
	}
	// numeric compare: 1.10.0 > 1.9.0
	if got := readFileT(t, binPath); string(got) != "v1.10.0" {
		t.Errorf("fallback picked %q, want v1.10.0", got)
	}
	if stderr.Len() == 0 {
		t.Errorf("expected a stderr note about registry fallback")
	}
}

// --- 7. registry down + empty cache: fatal ---

func TestRegistryDownEmptyCacheFatal(t *testing.T) {
	opts := testOpts(t, "http://127.0.0.1:1")
	opts.client = &http.Client{Timeout: 200 * time.Millisecond}

	_, err := resolve(opts, "@choonkeat/md-serve", "latest")
	if err == nil {
		t.Fatalf("want error with registry down and empty cache")
	}
}

// --- 8. 404 platform package: error mentions npx ---

func TestNotFoundMentionsNpx(t *testing.T) {
	reg := newFakeRegistry(t, "@choonkeat%2Fmd-serve-linux-x64", "1.0.0", map[string][]byte{})
	reg.notFound = true
	opts := testOpts(t, reg.srv.URL)

	_, err := resolve(opts, "@choonkeat/md-serve", "latest")
	if err == nil {
		t.Fatalf("want error on 404 platform package")
	}
	if !strings.Contains(err.Error(), "npx") {
		t.Errorf("404 error should point the operator at real npx: %v", err)
	}
}

// --- 9. concurrent-rename race: existing winner kept ---

func TestRenameRaceUsesExistingWinner(t *testing.T) {
	loserContent := []byte("#!loser md-serve")
	reg := newFakeRegistry(t, "@choonkeat%2Fmd-serve-linux-x64", "1.0.0", map[string][]byte{
		"1.0.0": tarGz(t, map[string][]byte{"package/bin/md-serve": loserContent}),
	})
	opts := testOpts(t, reg.srv.URL)

	// pre-create the final cache dir as if a concurrent process won the race
	winnerContent := []byte("#!winner md-serve")
	seedCache(t, opts.cacheDir, "@choonkeat/md-serve-linux-x64", "1.0.0", "md-serve", winnerContent)

	// force the download path despite the cache hit
	binPath, err := downloadAndCache(opts, "@choonkeat/md-serve-linux-x64", "1.0.0", "md-serve")
	if err != nil {
		t.Fatalf("downloadAndCache: %v", err)
	}
	if got := readFileT(t, binPath); !bytes.Equal(got, winnerContent) {
		t.Errorf("race loser overwrote winner: got %q", got)
	}
	// no leftover temp dirs under the cache root
	leftovers := []string{}
	filepath.Walk(opts.cacheDir, func(path string, info os.FileInfo, err error) error {
		if err == nil && info.IsDir() && strings.Contains(filepath.Base(path), "tmp") {
			leftovers = append(leftovers, path)
		}
		return nil
	})
	if len(leftovers) != 0 {
		t.Errorf("temp dirs left behind: %v", leftovers)
	}
}

// --- 11. (a) download failure for an unpinned request falls back to cache ---

func TestUnpinnedFallsBackWhenVersionUnavailable(t *testing.T) {
	// registry knows latest=1.2.0 but cannot serve it; cache holds 1.1.0
	reg := newFakeRegistry(t, "@choonkeat%2Fmd-serve-linux-x64", "1.2.0", map[string][]byte{})
	opts := testOpts(t, reg.srv.URL)
	stderr := &bytes.Buffer{}
	opts.stderr = stderr
	seedCache(t, opts.cacheDir, "@choonkeat/md-serve-linux-x64", "1.1.0", "md-serve", []byte("v1.1.0"))

	binPath, err := resolve(opts, "@choonkeat/md-serve", "latest")
	if err != nil {
		t.Fatalf("resolve should fall back to cached 1.1.0: %v", err)
	}
	if got := readFileT(t, binPath); string(got) != "v1.1.0" {
		t.Errorf("fallback binary = %q, want v1.1.0", got)
	}
	if !strings.Contains(stderr.String(), "1.1.0") {
		t.Errorf("expected a stderr note naming the fallback version: %q", stderr.String())
	}
}

func TestPinnedVersionNeverFallsBack(t *testing.T) {
	reg := newFakeRegistry(t, "@choonkeat%2Fmd-serve-linux-x64", "1.2.0", map[string][]byte{})
	opts := testOpts(t, reg.srv.URL)
	seedCache(t, opts.cacheDir, "@choonkeat/md-serve-linux-x64", "1.1.0", "md-serve", []byte("v1.1.0"))

	if _, err := resolve(opts, "@choonkeat/md-serve", "1.2.0"); err == nil {
		t.Fatalf("pinned 1.2.0 must not silently resolve to cached 1.1.0")
	}
}

func TestIntegrityFailureNeverFallsBack(t *testing.T) {
	binContent := []byte("#!fake md-serve 1.2.0")
	reg := newFakeRegistry(t, "@choonkeat%2Fmd-serve-linux-x64", "1.2.0", map[string][]byte{
		"1.2.0": tarGz(t, map[string][]byte{"package/bin/md-serve": binContent}),
	})
	reg.corruptIntegrity["1.2.0"] = true
	opts := testOpts(t, reg.srv.URL)
	seedCache(t, opts.cacheDir, "@choonkeat/md-serve-linux-x64", "1.1.0", "md-serve", []byte("v1.1.0"))

	_, err := resolve(opts, "@choonkeat/md-serve", "latest")
	if err == nil {
		t.Fatalf("integrity mismatch must be fatal, not a fallback trigger")
	}
	if !strings.Contains(err.Error(), "integrity") {
		t.Errorf("error should mention integrity: %v", err)
	}
}

// --- 12. (b) cache entries are re-verified before exec ---

func TestTamperedCacheEntryIsDiscarded(t *testing.T) {
	genuine := []byte("#!genuine md-serve")
	reg := newFakeRegistry(t, "@choonkeat%2Fmd-serve-linux-x64", "1.0.0", map[string][]byte{
		"1.0.0": tarGz(t, map[string][]byte{"package/bin/md-serve": genuine}),
	})
	opts := testOpts(t, reg.srv.URL)
	stderr := &bytes.Buffer{}
	opts.stderr = stderr

	binPath := seedCache(t, opts.cacheDir, "@choonkeat/md-serve-linux-x64", "1.0.0", "md-serve", genuine)
	// poison the cached binary after its digest was recorded
	if err := os.WriteFile(binPath, []byte("#!poisoned"), 0755); err != nil {
		t.Fatal(err)
	}

	got, err := resolve(opts, "@choonkeat/md-serve", "1.0.0")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if content := readFileT(t, got); !bytes.Equal(content, genuine) {
		t.Errorf("poisoned cache entry was exec'd: %q", content)
	}
	if !strings.Contains(stderr.String(), "untrusted") {
		t.Errorf("expected a stderr note about the discarded entry: %q", stderr.String())
	}
}

func TestUndigestedCacheEntryIsNotTrusted(t *testing.T) {
	// a cache dir with no digest record (e.g. hand-placed) must not be used
	// offline: no registry, no digest -> error rather than exec
	opts := testOpts(t, "http://127.0.0.1:1")
	opts.client = &http.Client{Timeout: 200 * time.Millisecond}
	dir := filepath.Join(opts.cacheDir, "@choonkeat", "md-serve-linux-x64@1.0.0", "bin")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "md-serve"), []byte("#!unverified"), 0755); err != nil {
		t.Fatal(err)
	}

	if _, err := resolve(opts, "@choonkeat/md-serve", "latest"); err == nil {
		t.Fatalf("undigested cache entry must not satisfy an offline resolve")
	}
}

func TestWorldWritableCacheEntryIsNotTrusted(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	binPath := filepath.Join(binDir, "md-serve")
	if err := os.WriteFile(binPath, []byte("#!x"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := writeCacheDigest(dir, "md-serve"); err != nil {
		t.Fatal(err)
	}
	if err := verifyCacheEntry(dir, "md-serve"); err != nil {
		t.Fatalf("baseline entry should verify: %v", err)
	}
	if err := os.Chmod(binPath, 0777); err != nil {
		t.Fatal(err)
	}
	if err := verifyCacheEntry(dir, "md-serve"); err == nil {
		t.Errorf("world-writable cached binary must not verify")
	}
}

// --- 13. missing checksum is rejected ---

func TestMissingIntegrityRejected(t *testing.T) {
	binContent := []byte("#!fake md-serve")
	reg := newFakeRegistry(t, "@choonkeat%2Fmd-serve-linux-x64", "1.0.0", map[string][]byte{
		"1.0.0": tarGz(t, map[string][]byte{"package/bin/md-serve": binContent}),
	})
	reg.noIntegrity["1.0.0"] = true
	opts := testOpts(t, reg.srv.URL)

	if _, err := resolve(opts, "@choonkeat/md-serve", "1.0.0"); err == nil {
		t.Fatalf("a version doc with no integrity and no shasum must be rejected")
	}
}

// --- 14. (c) registry URL validation ---

func TestValidateRegistry(t *testing.T) {
	ok := []string{
		"https://registry.npmjs.org",
		"https://registry.npmjs.org/",
		"http://localhost:4873",
		"http://127.0.0.1:4873",
	}
	for _, raw := range ok {
		if _, err := validateRegistry(raw); err != nil {
			t.Errorf("validateRegistry(%q) = %v, want ok", raw, err)
		}
	}
	bad := []string{
		"http://registry.example.com",
		"ftp://registry.example.com",
		"file:///tmp/registry",
		"registry.npmjs.org",
		"",
	}
	for _, raw := range bad {
		if _, err := validateRegistry(raw); err == nil {
			t.Errorf("validateRegistry(%q): want error, got nil", raw)
		}
	}
	got, err := validateRegistry("https://registry.npmjs.org/")
	if err != nil || got != "https://registry.npmjs.org" {
		t.Errorf("trailing slash not trimmed: %q, %v", got, err)
	}
}

func TestPlainHttpTarballRejected(t *testing.T) {
	opts := testOpts(t, "https://registry.npmjs.org")
	if err := checkTarballURL(opts, "http://evil.example.com/pkg.tgz"); err == nil {
		t.Errorf("plain-http non-loopback tarball URL must be rejected")
	}
	if err := checkTarballURL(opts, "https://registry.npmjs.org/pkg.tgz"); err != nil {
		t.Errorf("https tarball URL should be accepted: %v", err)
	}
	// different host is allowed (CDN) but must be surfaced
	stderr := &bytes.Buffer{}
	opts.stderr = stderr
	if err := checkTarballURL(opts, "https://cdn.example.com/pkg.tgz"); err != nil {
		t.Errorf("https CDN tarball URL should be accepted: %v", err)
	}
	if !strings.Contains(stderr.String(), "cdn.example.com") {
		t.Errorf("expected a note about the differing tarball host: %q", stderr.String())
	}
}

// --- 15. (d) size caps ---

func TestUnpackedSizeCapEnforced(t *testing.T) {
	// a highly compressible entry larger than the unpacked budget, streamed
	// so the test itself does not hold half a gigabyte in memory
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	size := int64(maxUnpackedBytes) + 1
	if err := tw.WriteHeader(&tar.Header{Name: "package/bin/md-serve", Mode: 0644, Size: size}); err != nil {
		t.Fatal(err)
	}
	if _, err := io.CopyN(tw, zeroReader{}, size); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	err := extractPackageTree(buf.Bytes(), t.TempDir())
	if err == nil {
		t.Fatalf("extraction past the unpacked cap must fail")
	}
	var secErr *securityError
	if !errors.As(err, &secErr) {
		t.Errorf("cap violation should be a securityError, got %T: %v", err, err)
	}
}

func TestReadCappedRejectsOverlongBody(t *testing.T) {
	if _, err := readCapped(bytes.NewReader(bytes.Repeat([]byte("x"), 11)), 10, "body"); err == nil {
		t.Errorf("readCapped must reject a body over the cap")
	}
	data, err := readCapped(bytes.NewReader([]byte("hello")), 10, "body")
	if err != nil || string(data) != "hello" {
		t.Errorf("readCapped under the cap: %q, %v", data, err)
	}
}

// --- exec stub: full run() flow ---

func TestRunExecsResolvedBinary(t *testing.T) {
	binContent := []byte("#!fake agent-chat")
	reg := newFakeRegistry(t, "@choonkeat%2Fagent-chat-linux-x64", "2.0.0", map[string][]byte{
		"2.0.0": tarGz(t, map[string][]byte{"package/bin/agent-chat": binContent}),
	})
	opts := testOpts(t, reg.srv.URL)

	var gotPath string
	var gotArgs []string
	oldExec := execFn
	execFn = func(path string, args []string, env []string) error {
		gotPath = path
		gotArgs = args
		return nil
	}
	defer func() { execFn = oldExec }()

	err := run(opts, []string{"-y", "@choonkeat/agent-chat@latest", "--port", "9000"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if gotPath == "" {
		t.Fatalf("execFn not called")
	}
	if got := readFileT(t, gotPath); !bytes.Equal(got, binContent) {
		t.Errorf("exec'd wrong binary: %q", got)
	}
	want := []string{gotPath, "--port", "9000"}
	if fmt.Sprint(gotArgs) != fmt.Sprint(want) {
		t.Errorf("exec args = %v, want %v", gotArgs, want)
	}
}
