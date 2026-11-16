package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const pageURI = "ui://calendar/view.html"

// readResourceServer serves resources/read with the given result; any other
// method fails the test, proving --read-resource never consults tools/list.
func readResourceServer(t *testing.T, result string, gotURI *string) string {
	t.Helper()
	return fakeSocketServerRPC(t, "svc", func(method string, params json.RawMessage) (json.RawMessage, *rpcError) {
		if method != "resources/read" {
			t.Errorf("unexpected method %q", method)
			return nil, &rpcError{Code: rpcMethodNotFound, Message: "Method not found"}
		}
		var p struct {
			URI string `json:"uri"`
		}
		json.Unmarshal(params, &p)
		if gotURI != nil {
			*gotURI = p.URI
		}
		return json.RawMessage(result), nil
	})
}

func TestReadResourceTextSavedToFile(t *testing.T) {
	var got string
	dir := readResourceServer(t, `{"contents":[{"uri":"`+pageURI+`","mimeType":"text/html;profile=mcp-app","text":"<html>hi</html>"}]}`, &got)
	outD := t.TempDir()
	t.Setenv("SWE_MCP_IMAGE_DIR", outD)

	out, errStr, code := runCapture(t, dir, "svc", "--read-resource", pageURI)
	if code != 0 {
		t.Fatalf("exit %d stderr=%s", code, errStr)
	}
	if got != pageURI {
		t.Errorf("server got uri %q", got)
	}
	if !strings.HasPrefix(out, "[resource "+pageURI+" text/html;profile=mcp-app saved to ") {
		t.Fatalf("out=%q", out)
	}
	path := strings.TrimSuffix(strings.TrimSpace(strings.SplitN(out, " saved to ", 2)[1]), "]")
	if filepath.Dir(path) != outD {
		t.Errorf("file %q not under %q", path, outD)
	}
	if filepath.Ext(path) != ".html" {
		t.Errorf("want .html extension, got %q", path)
	}
	if strings.Contains(out, "resource-meta") {
		t.Errorf("no _meta given, but meta line printed: %q", out)
	}
	b, err := os.ReadFile(path)
	if err != nil || string(b) != "<html>hi</html>" {
		t.Errorf("file contents %q err=%v", b, err)
	}
}

func TestReadResourceBlobDecoded(t *testing.T) {
	// base64 of "PNGDATA"
	dir := readResourceServer(t, `{"contents":[{"uri":"ui://x/logo","mimeType":"image/png","blob":"UE5HREFUQQ=="}]}`, nil)
	outD := t.TempDir()
	t.Setenv("SWE_MCP_IMAGE_DIR", outD)

	out, errStr, code := runCapture(t, dir, "svc", "--read-resource", "ui://x/logo")
	if code != 0 {
		t.Fatalf("exit %d stderr=%s", code, errStr)
	}
	entries, _ := os.ReadDir(outD)
	if len(entries) != 1 || filepath.Ext(entries[0].Name()) != ".png" {
		t.Fatalf("entries=%v out=%q", entries, out)
	}
	b, _ := os.ReadFile(filepath.Join(outD, entries[0].Name()))
	if string(b) != "PNGDATA" {
		t.Errorf("blob not decoded: %q", b)
	}
}

func TestReadResourceMetaPassthrough(t *testing.T) {
	result := `{"contents":[{"uri":"` + pageURI + `","mimeType":"text/html;profile=mcp-app","text":"<p>x</p>","_meta":{"ui":{"csp":{"connectDomains":["https://api.example.com"]},"prefersBorder":true}}}]}`
	dir := readResourceServer(t, result, nil)
	t.Setenv("SWE_MCP_IMAGE_DIR", t.TempDir())

	out, _, code := runCapture(t, dir, "svc", "--read-resource", pageURI)
	if code != 0 {
		t.Fatal(code)
	}
	want := `[resource-meta ` + pageURI + `] {"ui":{"csp":{"connectDomains":["https://api.example.com"]},"prefersBorder":true}}`
	if !strings.Contains(out, want) {
		t.Errorf("want %q in %q", want, out)
	}

	// --stdout: content on stdout, meta moves to stderr so stdout stays pipeable.
	out, errStr, code := runCapture(t, dir, "svc", "--read-resource", pageURI, "--stdout")
	if code != 0 {
		t.Fatal(code)
	}
	if out != "<p>x</p>\n" {
		t.Errorf("stdout=%q", out)
	}
	if !strings.Contains(errStr, want) {
		t.Errorf("want meta on stderr, got %q", errStr)
	}
}

func TestReadResourceStdoutRefusesBlob(t *testing.T) {
	dir := readResourceServer(t, `{"contents":[{"uri":"ui://x/logo","mimeType":"image/png","blob":"UE5HREFUQQ=="}]}`, nil)
	out, errStr, code := runCapture(t, dir, "svc", "--read-resource", "ui://x/logo", "--stdout")
	if code == 0 || out != "" {
		t.Errorf("want refusal, got code=%d out=%q", code, out)
	}
	if !strings.Contains(errStr, "omit --stdout") {
		t.Errorf("stderr=%q", errStr)
	}
}

func TestReadResourceServerWithoutResources(t *testing.T) {
	dir := fakeSocketServerRPC(t, "svc", func(method string, params json.RawMessage) (json.RawMessage, *rpcError) {
		return nil, &rpcError{Code: rpcMethodNotFound, Message: "Method not found"}
	})
	_, errStr, code := runCapture(t, dir, "svc", "--read-resource", pageURI)
	if code != 1 {
		t.Errorf("exit %d", code)
	}
	if strings.TrimSpace(errStr) != "mcp: svc does not serve resources" {
		t.Errorf("stderr=%q", errStr)
	}
}

func TestReadResourceUnknownURI(t *testing.T) {
	dir := fakeSocketServerRPC(t, "svc", func(method string, params json.RawMessage) (json.RawMessage, *rpcError) {
		return nil, &rpcError{Code: -32002, Message: "Resource not found"}
	})
	_, errStr, code := runCapture(t, dir, "svc", "--read-resource", "ui://nope")
	if code != 1 || !strings.Contains(errStr, "Resource not found") {
		t.Errorf("code=%d stderr=%q", code, errStr)
	}
}

func TestReadResourceUsageErrors(t *testing.T) {
	dir := readResourceServer(t, `{}`, nil)
	for _, args := range [][]string{
		{"svc", "--read-resource"},
		{"svc", "--read-resource", "ui://a", "ui://b"},
		{"svc", "--read-resource", "ui://a", "--bogus"},
	} {
		_, errStr, code := runCapture(t, dir, args...)
		if code != 1 || !strings.Contains(errStr, "--read-resource <uri>") {
			t.Errorf("%v: code=%d stderr=%q", args, code, errStr)
		}
	}
}

// TestDashOptionsRouteBeforeToolLookup proves dash-prefixed args after
// <server> never reach tools/list -- they can't collide with a tool name.
func TestDashOptionsRouteBeforeToolLookup(t *testing.T) {
	dir := fakeSocketServerRPC(t, "svc", func(method string, params json.RawMessage) (json.RawMessage, *rpcError) {
		t.Errorf("unexpected server call %q", method)
		return json.RawMessage(`{}`), nil
	})
	_, errStr, code := runCapture(t, dir, "svc", "--no-such-option")
	if code != 1 || !strings.Contains(errStr, `unknown option "--no-such-option"`) {
		t.Errorf("code=%d stderr=%q", code, errStr)
	}
}

func TestFileSafeAndExt(t *testing.T) {
	if got := extFor("text/html;profile=mcp-app"); got != ".html" {
		t.Errorf("extFor=%q", got)
	}
	if got := extFor("TEXT/HTML; profile=mcp-app"); got != ".html" {
		t.Errorf("extFor upper=%q", got)
	}
	if got := fileSafe(pageURI, ".html"); got != "ui___calendar_view" {
		t.Errorf("fileSafe=%q", got)
	}
	if strings.ContainsAny(fileSafe("ui://a/b\\c", ".bin"), `/\`) {
		t.Error("separator survived")
	}
}

const appToolsList = `{"tools":[
 {"name":"show_calendar","description":"Show a calendar.","inputSchema":{"type":"object"},"_meta":{"ui":{"resourceUri":"ui://calendar/view.html"}}},
 {"name":"legacy","description":"Old key.","inputSchema":{"type":"object"},"_meta":{"ui/resourceUri":"ui://legacy/page.html"}},
 {"name":"plain","description":"No page.","inputSchema":{"type":"object"}}]}`

// TestServerHelpShowsPageLines proves tools declaring an MCP Apps page (both
// the current and the deprecated _meta key) get a `page:` line with the exact
// --read-resource command, and the standalone server help advertises the
// resource options.
func TestServerHelpShowsPageLines(t *testing.T) {
	dir := fakeSocketServer(t, "svc", func(method string, params json.RawMessage) json.RawMessage {
		return json.RawMessage(appToolsList)
	})
	out, _, code := runCapture(t, dir, "svc", "-h")
	if code != 0 {
		t.Fatal(code)
	}
	for _, want := range []string{
		"page: ui://calendar/view.html  (mcp svc --read-resource ui://calendar/view.html)",
		"page: ui://legacy/page.html  (mcp svc --read-resource ui://legacy/page.html)",
		"mcp svc --list-resources | --read-resource <uri>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Count(out, "page: ") != 2 {
		t.Errorf("want exactly 2 page lines:\n%s", out)
	}
}

// TestTopHelpDocumentsResourceFlags is the discoverability contract:
// agent-chat's instructions say "if `mcp -h` lists --read-resource, use it".
func TestTopHelpDocumentsResourceFlags(t *testing.T) {
	out, _, code := runCapture(t, t.TempDir(), "-h")
	if code != 0 {
		t.Fatal(code)
	}
	for _, want := range []string{"--read-resource <uri>", "--list-resources", "[resource-meta <uri>]", `"page: <uri>"`} {
		if !strings.Contains(out, want) {
			t.Errorf("top help missing %q", want)
		}
	}
}

func TestListResources(t *testing.T) {
	var cursors []string
	dir := fakeSocketServerRPC(t, "svc", func(method string, params json.RawMessage) (json.RawMessage, *rpcError) {
		switch method {
		case "resources/list":
			var p struct {
				Cursor string `json:"cursor"`
			}
			json.Unmarshal(params, &p)
			cursors = append(cursors, p.Cursor)
			if p.Cursor == "" {
				return json.RawMessage(`{"resources":[{"uri":"ui://calendar/view.html","name":"calendar","mimeType":"text/html;profile=mcp-app","description":"Calendar view","_meta":{"ui":{"prefersBorder":true}}}],"nextCursor":"p2"}`), nil
			}
			return json.RawMessage(`{"resources":[{"uri":"file:///notes.txt","name":"notes","mimeType":"text/plain"}]}`), nil
		case "resources/templates/list":
			return json.RawMessage(`{"resourceTemplates":[{"uriTemplate":"file:///{path}","name":"files"}]}`), nil
		}
		return nil, &rpcError{Code: rpcMethodNotFound, Message: "Method not found"}
	})
	out, errStr, code := runCapture(t, dir, "svc", "--list-resources")
	if code != 0 {
		t.Fatalf("exit %d stderr=%s", code, errStr)
	}
	if len(cursors) != 2 || cursors[1] != "p2" {
		t.Errorf("pagination cursors=%v", cursors)
	}
	for _, want := range []string{
		"ui://calendar/view.html  text/html;profile=mcp-app  calendar",
		"    Calendar view",
		`    meta: {"ui":{"prefersBorder":true}}`,
		"file:///notes.txt  text/plain  notes",
		"Templates (",
		"file:///{path}  files",
		"Read one: mcp svc --read-resource <uri>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestListResourcesWithoutTemplates(t *testing.T) {
	dir := fakeSocketServerRPC(t, "svc", func(method string, params json.RawMessage) (json.RawMessage, *rpcError) {
		if method == "resources/list" {
			return json.RawMessage(`{"resources":[]}`), nil
		}
		return nil, &rpcError{Code: rpcMethodNotFound, Message: "Method not found"}
	})
	out, errStr, code := runCapture(t, dir, "svc", "--list-resources")
	if code != 0 {
		t.Fatalf("exit %d stderr=%s", code, errStr)
	}
	if !strings.Contains(out, "(no resources)") || !strings.Contains(out, "does not list resource templates") {
		t.Errorf("out=%q", out)
	}
}

func TestListResourcesServerWithoutResources(t *testing.T) {
	dir := fakeSocketServerRPC(t, "svc", func(method string, params json.RawMessage) (json.RawMessage, *rpcError) {
		return nil, &rpcError{Code: rpcMethodNotFound, Message: "Method not found"}
	})
	_, errStr, code := runCapture(t, dir, "svc", "--list-resources")
	if code != 1 || strings.TrimSpace(errStr) != "mcp: svc does not serve resources" {
		t.Errorf("code=%d stderr=%q", code, errStr)
	}
}
