package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"fmt"
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
	return map[string]any{
		"tarball":   f.srv.URL + "/tarballs/" + version + ".tgz",
		"integrity": integrity,
	}
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
	cached := filepath.Join(opts.cacheDir, "@choonkeat", "md-serve-linux-x64@1.0.0", "bin")
	if err := os.MkdirAll(cached, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cached, "md-serve"), binContent, 0755); err != nil {
		t.Fatal(err)
	}

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
		dir := filepath.Join(opts.cacheDir, "@choonkeat", "md-serve-linux-x64@"+v, "bin")
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "md-serve"), []byte("v"+v), 0755); err != nil {
			t.Fatal(err)
		}
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
	winnerDir := filepath.Join(opts.cacheDir, "@choonkeat", "md-serve-linux-x64@1.0.0", "bin")
	if err := os.MkdirAll(winnerDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(winnerDir, "md-serve"), winnerContent, 0755); err != nil {
		t.Fatal(err)
	}

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
