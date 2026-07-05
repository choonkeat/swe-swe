// mcp is the agent-facing client for swe-swe's MCP-less mode. It calls MCP
// server tools over unix sockets served by mcp-cli-proxy, so an agent whose
// native MCP is gated can still reach every tool through its normal shell.
//
// The socket directory IS the registry: each mcp-cli-proxy drops one
// <server>.sock, and `mcp` discovers servers by listing that directory.
//
// The command surface mirrors the canonical MCP tool id `mcp__<server>__<tool>`:
//
//	mcp                         list servers
//	mcp <server>                full docs for every tool (what native MCP injects)
//	mcp <server> <tool> -h      full docs for one tool (from its JSON Schema)
//	mcp <server> <tool> [flags] call the tool; print its result
//
// Flags are synthesized from each tool's inputSchema. Results render as:
// text -> stdout, image -> written to a file whose path is printed, structured
// -> JSON. Blocking tools (e.g. send_message, which waits for the user's reply)
// simply block until the proxy returns -- there is no client-side timeout.
//
// Each call starts by printing a one-line <mcp>tip</mcp> reminder to stderr
// pointing at the tool's -h docs: MCP-less agents never get tool docs
// injected into context and lose whatever they read at context compaction.
// It prints BEFORE the call runs (as soon as the tool name resolves) so
// failures -- which need the docs most -- and long-blocking or killed calls
// still carry it. Throttled per (server, tool) via marker files in
// <socket dir>/.remind so a hot loop is not spammed; tune with
// --remind-help-text-throttle (default 30m, 0 = every call).
package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const defaultSocketDir = "/workspace/.swe-swe/run/mcp"

func socketDir() string {
	if d := os.Getenv("SWE_MCP_DIR"); d != "" {
		return d
	}
	return defaultSocketDir
}

// --- MCP wire helpers ---------------------------------------------------------

type tool struct {
	Name        string          `json:"name"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	Annotations toolAnnotations `json:"annotations"`
}

// toolAnnotations are the MCP tool annotations a native client would surface
// (title + behavior hints). Pointers distinguish "absent" from "false".
type toolAnnotations struct {
	Title           string `json:"title"`
	ReadOnlyHint    *bool  `json:"readOnlyHint"`
	DestructiveHint *bool  `json:"destructiveHint"`
}

type rpcResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) String() string { return fmt.Sprintf("%s (code %d)", e.Message, e.Code) }

// dial connects to a server's socket, mapping "not found" to a friendly error.
func dial(server string) (net.Conn, error) {
	sock := filepath.Join(socketDir(), server+".sock")
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return nil, fmt.Errorf("mcp server %q unavailable (socket %s): %w", server, sock, err)
	}
	return conn, nil
}

// rpc sends one request over conn and reads exactly one response line. There is
// deliberately no read deadline: blocking tools may take arbitrarily long.
func rpc(conn net.Conn, method string, params any) (json.RawMessage, error) {
	req := map[string]any{"jsonrpc": "2.0", "id": 1, "method": method}
	if params != nil {
		req["params"] = params
	}
	line, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if _, err := conn.Write(append(line, '\n')); err != nil {
		return nil, err
	}
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	if !sc.Scan() {
		if err := sc.Err(); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("no response from server")
	}
	var resp rpcResponse
	if err := json.Unmarshal(sc.Bytes(), &resp); err != nil {
		return nil, fmt.Errorf("bad response: %w", err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("%s", resp.Error.String())
	}
	return resp.Result, nil
}

func listTools(server string) ([]tool, error) {
	conn, err := dial(server)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	result, err := rpc(conn, "tools/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var payload struct {
		Tools []tool `json:"tools"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		return nil, fmt.Errorf("parsing tools/list: %w", err)
	}
	return payload.Tools, nil
}

// listServers returns the server names discovered in the socket directory.
func listServers() []string {
	entries, err := os.ReadDir(socketDir())
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if n := strings.TrimSuffix(e.Name(), ".sock"); n != e.Name() {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names
}

// --- JSON Schema -> flags -----------------------------------------------------

type schema struct {
	Properties map[string]property `json:"properties"`
	Required   []string            `json:"required"`
	// RawProperties keeps each property's original JSON so help can surface
	// schema keywords the typed renderer doesn't model (nested objects, oneOf,
	// minimum, format, ...) instead of silently dropping them.
	RawProperties map[string]json.RawMessage `json:"-"`
}

type property struct {
	Type        typeName        `json:"type"`
	Description string          `json:"description"`
	Enum        []any           `json:"enum"`
	Items       *itemDef        `json:"items"`
	Default     json.RawMessage `json:"default"`
}

type itemDef struct {
	Type string `json:"type"`
}

// typeName holds a JSON Schema "type", which may be a single string
// (`"array"`) or a union expressed as an array (`["null","array"]`). Real MCP
// schemas use the union form for nullable/optional fields, so a plain `string`
// field silently dropped it -- the field became "", defaulted to string, and
// the value was forwarded uncoerced.
type typeName struct{ names []string }

func (t *typeName) UnmarshalJSON(b []byte) error {
	var single string
	if json.Unmarshal(b, &single) == nil {
		t.names = []string{single}
		return nil
	}
	var arr []string
	if json.Unmarshal(b, &arr) == nil {
		t.names = arr
		return nil
	}
	// Unrecognized shape: leave empty so kind() falls back to "string".
	return nil
}

// kind returns the canonical type used for coercion and help, ignoring "null"
// so nullable unions resolve to their real type (e.g. ["null","array"] -> array).
func (p property) kind() string {
	for _, n := range p.Type.names {
		if n != "null" && n != "" {
			return n
		}
	}
	return "string"
}

func parseSchema(raw json.RawMessage) schema {
	var s schema
	if len(raw) > 0 {
		json.Unmarshal(raw, &s)
		var rawView struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if json.Unmarshal(raw, &rawView) == nil {
			s.RawProperties = rawView.Properties
		}
	}
	return s
}

// knownPropKeys are the schema keywords printToolHelp renders natively; any
// other keyword on a property is exotic and gets echoed as raw JSON.
var knownPropKeys = map[string]bool{
	"type": true, "description": true, "enum": true, "items": true, "default": true,
}

// exoticSubset returns the compact JSON of a property's schema keywords that
// the help renderer does not model, or "" when everything was rendered. This
// guarantees -h never silently drops schema information. "items" counts as
// rendered only in its simple {"type": ...} form.
func exoticSubset(raw json.RawMessage) string {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return ""
	}
	extra := map[string]json.RawMessage{}
	for k, v := range m {
		if !knownPropKeys[k] {
			extra[k] = v
			continue
		}
		if k == "items" {
			var im map[string]json.RawMessage
			if json.Unmarshal(v, &im) != nil {
				extra[k] = v // e.g. tuple form: items as an array of schemas
				continue
			}
			delete(im, "type")
			if len(im) > 0 {
				extra[k] = v
			}
		}
	}
	if len(extra) == 0 {
		return ""
	}
	b, _ := json.Marshal(extra) // map keys marshal sorted: deterministic output
	return string(b)
}

// coerce converts a flag's string value to the Go type the schema calls for.
func coerce(p property, raw string) (any, error) {
	switch p.kind() {
	case "boolean":
		return strconv.ParseBool(raw)
	case "integer":
		return strconv.ParseInt(raw, 10, 64)
	case "number":
		return strconv.ParseFloat(raw, 64)
	case "array", "object":
		var v any
		if err := json.Unmarshal([]byte(raw), &v); err != nil {
			return nil, fmt.Errorf("value must be JSON: %w", err)
		}
		return v, nil
	default: // string or unspecified
		return raw, nil
	}
}

// parseToolArgs turns CLI flags into the tool's arguments object, validated
// against the schema (types coerced, required flags enforced, enums checked).
func parseToolArgs(s schema, args []string) (map[string]any, error) {
	out := map[string]any{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "--") {
			return nil, fmt.Errorf("unexpected argument %q (flags must start with --)", a)
		}
		name := strings.TrimPrefix(a, "--")
		var valStr string
		var haveVal bool
		if eq := strings.IndexByte(name, '='); eq >= 0 {
			valStr, haveVal = name[eq+1:], true
			name = name[:eq]
		}
		p, ok := s.Properties[name]
		if !ok {
			return nil, fmt.Errorf("unknown flag --%s", name)
		}
		if !haveVal {
			if p.kind() == "boolean" {
				valStr = "true" // bare --flag means true
			} else {
				i++
				if i >= len(args) {
					return nil, fmt.Errorf("flag --%s requires a value", name)
				}
				valStr = args[i]
			}
		}
		v, err := coerce(p, valStr)
		if err != nil {
			return nil, fmt.Errorf("--%s: %w", name, err)
		}
		if len(p.Enum) > 0 && !enumContains(p.Enum, v) {
			return nil, fmt.Errorf("--%s: must be one of %v", name, p.Enum)
		}
		out[name] = v
	}
	for _, req := range s.Required {
		if _, ok := out[req]; !ok {
			return nil, fmt.Errorf("missing required flag --%s", req)
		}
	}
	return out, nil
}

func enumContains(enum []any, v any) bool {
	for _, e := range enum {
		if fmt.Sprint(e) == fmt.Sprint(v) {
			return true
		}
	}
	return false
}

// --- result rendering ---------------------------------------------------------

type callResult struct {
	Content           []contentBlock  `json:"content"`
	StructuredContent json.RawMessage `json:"structuredContent"`
	IsError           bool            `json:"isError"`
}

type contentBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text"`
	Data     string `json:"data"`     // base64, for image/audio
	MimeType string `json:"mimeType"` // for image/audio
}

func extFor(mime string) string {
	switch mime {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	default:
		return ".bin"
	}
}

// render prints a tool result and returns the process exit code.
func render(server, toolName string, raw json.RawMessage, out, errOut *os.File) int {
	var r callResult
	if err := json.Unmarshal(raw, &r); err != nil {
		// Not the standard shape; emit as-is.
		fmt.Fprintln(out, string(raw))
		return 0
	}
	for _, b := range r.Content {
		switch b.Type {
		case "text":
			fmt.Fprintln(out, b.Text)
		case "image", "audio":
			data, err := base64.StdEncoding.DecodeString(b.Data)
			if err != nil {
				fmt.Fprintf(errOut, "mcp: failed to decode %s data: %v\n", b.Type, err)
				continue
			}
			f, err := os.CreateTemp(imageDir(), fmt.Sprintf("%s-%s-*%s", server, toolName, extFor(b.MimeType)))
			if err != nil {
				fmt.Fprintf(errOut, "mcp: failed to create file for %s: %v\n", b.Type, err)
				continue
			}
			f.Write(data)
			f.Close()
			fmt.Fprintf(out, "[%s %s saved to %s]\n", b.Type, b.MimeType, f.Name())
		default:
			// Unknown block: print its JSON.
			j, _ := json.Marshal(b)
			fmt.Fprintln(out, string(j))
		}
	}
	if len(r.StructuredContent) > 0 {
		var pretty any
		if json.Unmarshal(r.StructuredContent, &pretty) == nil {
			j, _ := json.MarshalIndent(pretty, "", "  ")
			fmt.Fprintln(out, string(j))
		}
	}
	if r.IsError {
		return 1
	}
	return 0
}

func imageDir() string {
	if d := os.Getenv("SWE_MCP_IMAGE_DIR"); d != "" {
		return d
	}
	return os.TempDir()
}

// --- help ---------------------------------------------------------------------

func printTopHelp(out *os.File) {
	fmt.Fprint(out, `mcp - call MCP server tools over unix sockets (swe-swe MCP-less mode)

Usage:
  mcp <server> <tool> [flags]   call a tool
  mcp -h                        full docs for every server and tool (below)
  mcp <server>                  full docs for one server's tools
  mcp <server> <tool> -h        full docs for one tool

Global flags:
  --remind-help-text-throttle duration
      how often the pre-call <mcp>tip</mcp> docs reminder re-prints per tool
      (default 30m; 0 = every call)

The command mirrors the tool id mcp__<server>__<tool>:
  mcp__swe-swe-agent-chat__send_message  ->  mcp swe-swe-agent-chat send_message --text "..."
`)
	servers := listServers()
	if len(servers) == 0 {
		fmt.Fprintf(out, "\nNo servers found in %s\n", socketDir())
		return
	}
	fmt.Fprintf(out, "\nServers (from %s):\n", socketDir())
	for _, s := range servers {
		fmt.Fprintf(out, "  %s\n", s)
	}
	// Full docs for every server follow -- bare `mcp -h` is the one command
	// steering points agents at, so it must carry the entire registry (the
	// byte-for-byte equivalent of what a native MCP client would inject).
	// A server whose socket won't answer degrades to an inline note instead
	// of killing the dump.
	for _, s := range servers {
		fmt.Fprintf(out, "\n===== %s =====\n\n", s)
		if err := printServerHelp(s, out); err != nil {
			fmt.Fprintf(out, "(failed to load tools: %v)\n", err)
		}
	}
}

// printServerHelp dumps every tool's full help -- the byte-for-byte equivalent
// of what a native MCP client would inject into the agent's context.
func printServerHelp(server string, out *os.File) error {
	tools, err := listTools(server)
	if err != nil {
		return err
	}
	for i := range tools {
		if i > 0 {
			fmt.Fprintf(out, "\n---\n\n")
		}
		printToolHelp(server, tools[i], out)
	}
	return nil
}

// annotationLine renders the tool's title and behavior hints, mirroring what a
// native MCP client would surface from `title` / `annotations`.
func annotationLine(t tool) string {
	title := t.Annotations.Title
	if title == "" {
		title = t.Title
	}
	var marks []string
	if t.Annotations.ReadOnlyHint != nil && *t.Annotations.ReadOnlyHint {
		marks = append(marks, "[read-only]")
	}
	if t.Annotations.DestructiveHint != nil && *t.Annotations.DestructiveHint {
		marks = append(marks, "[destructive]")
	}
	parts := title
	if len(marks) > 0 {
		if parts != "" {
			parts += "  "
		}
		parts += strings.Join(marks, " ")
	}
	return parts
}

// typeLabel names a flag's value type, expanding simple array item types
// ("array of string") so the agent knows what JSON to pass.
func typeLabel(p property) string {
	k := p.kind()
	if k == "array" && p.Items != nil && p.Items.Type != "" {
		return "array of " + p.Items.Type
	}
	return k
}

// defaultLabel renders a schema default: bare for strings, JSON otherwise.
func defaultLabel(p property) string {
	if len(p.Default) == 0 || string(p.Default) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(p.Default, &s) == nil {
		return s
	}
	return string(p.Default)
}

func enumLabel(enum []any) string {
	vals := make([]string, len(enum))
	for i, e := range enum {
		vals[i] = fmt.Sprint(e)
	}
	return strings.Join(vals, ", ")
}

func printToolHelp(server string, t tool, out *os.File) {
	fmt.Fprintf(out, "mcp %s %s [flags]\n", server, t.Name)
	if line := annotationLine(t); line != "" {
		fmt.Fprintf(out, "%s\n", line)
	}
	if t.Description != "" {
		fmt.Fprintf(out, "\n%s\n", t.Description)
	}
	s := parseSchema(t.InputSchema)
	if len(s.Properties) == 0 {
		fmt.Fprintf(out, "\n(no flags)\n")
		return
	}
	required := map[string]bool{}
	for _, r := range s.Required {
		required[r] = true
	}
	// Required flags first, then optional; alphabetical within each group.
	var reqNames, optNames []string
	for n := range s.Properties {
		if required[n] {
			reqNames = append(reqNames, n)
		} else {
			optNames = append(optNames, n)
		}
	}
	sort.Strings(reqNames)
	sort.Strings(optNames)
	fmt.Fprintf(out, "\nFlags:\n")
	for _, n := range append(reqNames, optNames...) {
		p := s.Properties[n]
		var attrs []string
		if required[n] {
			attrs = append(attrs, "required")
		}
		if d := defaultLabel(p); d != "" {
			attrs = append(attrs, "default: "+d)
		}
		head := fmt.Sprintf("--%s %s", n, typeLabel(p))
		if len(attrs) > 0 {
			head += " (" + strings.Join(attrs, ", ") + ")"
		}
		fmt.Fprintf(out, "  %s\n", head)
		if p.Description != "" {
			for _, line := range strings.Split(strings.TrimRight(p.Description, "\n"), "\n") {
				fmt.Fprintf(out, "      %s\n", line)
			}
		}
		if len(p.Enum) > 0 {
			fmt.Fprintf(out, "      one of: %s\n", enumLabel(p.Enum))
		}
		if ex := exoticSubset(s.RawProperties[n]); ex != "" {
			fmt.Fprintf(out, "      schema: %s\n", ex)
		}
	}
}

// --- post-call -h reminder ------------------------------------------------

// maybeRemindHelp prints a one-line <mcp>tip</mcp> to errOut ahead of a tool
// call, pointing at the tool's own -h docs. MCP-less agents never get tool
// docs injected into context and lose whatever they did read at context
// compaction -- and compaction is invisible to them, so the nudge must come
// from outside. Throttled per (server, tool) via a marker file's mtime under
// <socket dir>/.remind (never listed as a server: no .sock suffix). throttle
// 0 prints on every call. State errors fail open: the reminder matters more
// than the bookkeeping.
func maybeRemindHelp(server, tool string, throttle time.Duration, errOut *os.File) {
	dir := filepath.Join(socketDir(), ".remind")
	marker := filepath.Join(dir, server+"__"+tool)
	if throttle > 0 {
		if fi, err := os.Stat(marker); err == nil && time.Since(fi.ModTime()) < throttle {
			return
		}
	}
	fmt.Fprintf(errOut, "<mcp>tip: this tool's docs are not in your context and fade after compaction; refresh: mcp %s %s -h (all tools: mcp %s -h)</mcp>\n", server, tool, server)
	os.MkdirAll(dir, 0o755)
	os.WriteFile(marker, nil, 0o644)
}

// remindFlag is the global flag controlling the reminder throttle.
const remindFlag = "--remind-help-text-throttle"

// extractThrottleFlag strips --remind-help-text-throttle[=]<duration> from
// args (any position) and returns the remaining args plus the throttle to
// use. Accepts Go durations ("30m", "1h"); "0" prints the tip on every call.
func extractThrottleFlag(args []string) ([]string, time.Duration, error) {
	throttle := 30 * time.Minute
	var rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		var raw string
		switch {
		case a == remindFlag:
			i++
			if i >= len(args) {
				return nil, 0, fmt.Errorf("flag %s requires a value", remindFlag)
			}
			raw = args[i]
		case strings.HasPrefix(a, remindFlag+"="):
			raw = a[len(remindFlag)+1:]
		default:
			rest = append(rest, a)
			continue
		}
		d, err := time.ParseDuration(raw)
		if err != nil {
			return nil, 0, fmt.Errorf("%s: %w", remindFlag, err)
		}
		throttle = d
	}
	return rest, throttle, nil
}

func isHelp(a string) bool { return a == "-h" || a == "--help" || a == "help" }

// --- main ---------------------------------------------------------------------

func run(args []string, out, errOut *os.File) int {
	args, throttle, err := extractThrottleFlag(args)
	if err != nil {
		fmt.Fprintf(errOut, "mcp: %v\n", err)
		return 1
	}
	if len(args) == 0 || isHelp(args[0]) {
		printTopHelp(out)
		return 0
	}
	server := args[0]
	if len(args) == 1 || isHelp(args[1]) {
		if err := printServerHelp(server, out); err != nil {
			fmt.Fprintf(errOut, "mcp: %v\n", err)
			return 1
		}
		return 0
	}
	toolName := args[1]
	rest := args[2:]

	tools, err := listTools(server)
	if err != nil {
		fmt.Fprintf(errOut, "mcp: %v\n", err)
		return 1
	}
	var t *tool
	for i := range tools {
		if tools[i].Name == toolName {
			t = &tools[i]
			break
		}
	}
	if t == nil {
		fmt.Fprintf(errOut, "mcp: %s has no tool %q (run: mcp %s -h)\n", server, toolName, server)
		return 1
	}

	if len(rest) > 0 && isHelp(rest[0]) {
		printToolHelp(server, *t, out)
		return 0
	}

	// Tip goes out BEFORE the call: failed calls (bad flags, tool errors) need
	// the docs pointer most, and a blocking call killed mid-wait still carried
	// it. Printed only once the tool name resolved, so the refresh command it
	// suggests is guaranteed to work.
	maybeRemindHelp(server, toolName, throttle, errOut)

	s := parseSchema(t.InputSchema)
	arguments, err := parseToolArgs(s, rest)
	if err != nil {
		fmt.Fprintf(errOut, "mcp: %v\n", err)
		return 1
	}

	conn, err := dial(server)
	if err != nil {
		fmt.Fprintf(errOut, "mcp: %v\n", err)
		return 1
	}
	defer conn.Close()
	result, err := rpc(conn, "tools/call", map[string]any{"name": toolName, "arguments": arguments})
	if err != nil {
		fmt.Fprintf(errOut, "mcp: %v\n", err)
		return 1
	}
	return render(server, toolName, result, out, errOut)
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
