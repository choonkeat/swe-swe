package main

import (
	"bufio"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// normalizeEnvName maps a service name to the suffix of its discovery env var:
// uppercased, with every non-alphanumeric character replaced by '_'. E.g.
// "web-1" -> "WEB_1", so its port is published as PORT_WEB_1.
func normalizeEnvName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - ('a' - 'A'))
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// parseEnvFile reads foreman-style `KEY=value` lines. Blank lines and `#`
// comments are ignored. Whitespace around the key and around the value is
// trimmed; a single layer of matching surrounding single or double quotes is
// stripped from the value. Returns an error on a non-comment line with no '='.
func parseEnvFile(r io.Reader) (map[string]string, error) {
	out := map[string]string{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx < 0 {
			return nil, fmt.Errorf("line %d: not a KEY=value assignment: %q", lineNo, line)
		}
		key := strings.TrimSpace(line[:idx])
		if key == "" {
			return nil, fmt.Errorf("line %d: empty key: %q", lineNo, line)
		}
		val := strings.TrimSpace(line[idx+1:])
		val = stripQuotes(val)
		out[key] = val
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// stripQuotes removes one layer of matching surrounding single or double quotes.
func stripQuotes(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// buildServiceEnv computes the environment for one service, applying the spec
// 4.5 precedence (later wins):
//
//  1. inherited session environment
//  2. .swe-swe/env (sweEnv)
//  3. .env (dotEnv)
//  4. runner-assigned discovery values: PORT_<NAME> for every service, plus this
//     service's own PORT -- these always win so discovery is authoritative.
//
// The result is a deterministically sorted []string of "K=V" entries suitable
// for exec.Cmd.Env.
func buildServiceEnv(inherited []string, sweEnv, dotEnv map[string]string, ports map[string]int, serviceName string) []string {
	merged := map[string]string{}

	// 1. inherited
	for _, kv := range inherited {
		if i := strings.IndexByte(kv, '='); i >= 0 {
			merged[kv[:i]] = kv[i+1:]
		}
	}
	// 2. .swe-swe/env
	for k, v := range sweEnv {
		merged[k] = v
	}
	// 3. .env
	for k, v := range dotEnv {
		merged[k] = v
	}
	// 4. discovery -- always wins.
	for name, p := range ports {
		merged["PORT_"+normalizeEnvName(name)] = strconv.Itoa(p)
	}
	merged["PORT"] = strconv.Itoa(ports[serviceName])

	keys := make([]string, 0, len(merged))
	for k := range merged {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	env := make([]string, 0, len(keys))
	for _, k := range keys {
		env = append(env, k+"="+merged[k])
	}
	return env
}
