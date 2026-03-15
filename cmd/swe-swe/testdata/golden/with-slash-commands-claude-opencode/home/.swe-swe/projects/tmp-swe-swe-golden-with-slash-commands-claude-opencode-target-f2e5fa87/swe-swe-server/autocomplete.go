package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// autocompleteItem is a single autocomplete result with a value and optional hint.
type autocompleteItem struct {
	V string `json:"v"`
	H string `json:"h,omitempty"`
}

// autocompleteResponse is the structured response matching the agent-chat
// autocomplete API spec (see docs/autocomplete-api.md).
type autocompleteResponse struct {
	Results []autocompleteItem `json:"results"`
	HasMore bool               `json:"has_more,omitempty"`
}

// handleAutocompleteAPI handles POST /api/autocomplete/{sessionUUID}
// It returns slash command completions for the given session's agent.
func handleAutocompleteAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// API key authentication (shared with MCP endpoint)
	if r.URL.Query().Get("key") != mcpAuthKey {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse session UUID from path: /api/autocomplete/{uuid}
	sessionUUID := strings.TrimPrefix(r.URL.Path, "/api/autocomplete/")
	if sessionUUID == "" {
		http.Error(w, "missing session UUID", http.StatusBadRequest)
		return
	}

	sess, ok := sessions[sessionUUID]
	if !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 4096))
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}

	var req struct {
		Type  string `json:"type"`
		Query string `json:"query"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	systemDir, ext := slashCommandDirForAgent(sess.Assistant, sess.AssistantConfig.SlashCmdFormat)
	if systemDir == "" && ext == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(autocompleteResponse{Results: []autocompleteItem{}})
		return
	}

	// Collect commands from system-level and project-level directories
	type sourced struct {
		item autocompleteItem
		dir  string
	}
	var all []sourced
	if systemDir != "" {
		for _, item := range discoverSlashCommands(systemDir, ext) {
			all = append(all, sourced{item, systemDir})
		}
	}
	projectDir := projectCommandDir(sess.Assistant, sess.WorkDir)
	if projectDir != "" && projectDir != systemDir {
		for _, item := range discoverSlashCommands(projectDir, ext) {
			all = append(all, sourced{item, projectDir})
		}
	}

	// Detect duplicate command names and annotate with source path
	counts := make(map[string]int)
	for _, s := range all {
		counts[s.item.V]++
	}
	home := os.Getenv("HOME")
	var items []autocompleteItem
	for _, s := range all {
		item := s.item
		if counts[item.V] > 1 {
			// Disambiguate: append source path to hint
			displayDir := s.dir
			if home != "" && strings.HasPrefix(displayDir, home) {
				displayDir = "~" + displayDir[len(home):]
			} else if sess.WorkDir != "" && strings.HasPrefix(displayDir, sess.WorkDir+"/") {
				displayDir = displayDir[len(sess.WorkDir)+1:]
			}
			if item.H != "" {
				item.H = item.H + " — " + displayDir
			} else {
				item.H = displayDir
			}
		}
		items = append(items, item)
	}

	if req.Query != "" {
		items = filterAutocomplete(items, req.Query)
	}
	if items == nil {
		items = []autocompleteItem{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(autocompleteResponse{Results: items})
}

// slashCommandDirForAgent returns the system-level slash command directory and
// file extension for the given agent type.
func slashCommandDirForAgent(assistant string, format SlashCommandFormat) (dir string, ext string) {
	switch assistant {
	case "claude":
		return "/home/app/.claude/commands", "md"
	case "codex":
		return "/home/app/.codex/prompts", "md"
	case "opencode":
		return "/home/app/.config/opencode/command", "md"
	case "gemini":
		return "/home/app/.gemini/commands", "toml"
	default:
		if format == SlashCmdMD {
			return "", "md"
		}
		if format == SlashCmdTOML {
			return "", "toml"
		}
		return "", ""
	}
}

// projectCommandDir returns the project-level slash command directory for the
// given agent, based on the session's working directory. Returns "" if the
// agent has no project-level command convention or workDir is empty.
func projectCommandDir(assistant string, workDir string) string {
	if workDir == "" {
		return ""
	}
	switch assistant {
	case "claude":
		return filepath.Join(workDir, ".claude", "commands")
	case "codex":
		return filepath.Join(workDir, ".codex", "prompts")
	case "opencode":
		return filepath.Join(workDir, ".opencode", "command")
	case "gemini":
		return filepath.Join(workDir, ".gemini", "commands")
	default:
		return ""
	}
}

// discoverSlashCommands scans a slash command directory and returns all
// commands with their descriptions. Supports two layouts:
//
//	commands/
//	  command.md            → V="command"           (flat, no namespace)
//	  namespace/
//	    command.md          → V="namespace:command"  (namespaced)
//
// Returns items with H=description extracted from file content.
func discoverSlashCommands(dir string, ext string) []autocompleteItem {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var items []autocompleteItem
	for _, entry := range entries {
		if entry.IsDir() {
			// Namespaced: namespace/command.ext
			nsName := entry.Name()
			nsPath := filepath.Join(dir, nsName)
			subEntries, err := os.ReadDir(nsPath)
			if err != nil {
				continue
			}
			for _, sub := range subEntries {
				if sub.IsDir() {
					continue
				}
				name := sub.Name()
				if !strings.HasSuffix(name, "."+ext) {
					continue
				}
				cmdName := strings.TrimSuffix(name, "."+ext)
				fullName := nsName + ":" + cmdName

				content, err := os.ReadFile(filepath.Join(nsPath, name))
				hint := ""
				if err == nil {
					hint = extractDescription(string(content), ext)
				}
				items = append(items, autocompleteItem{V: fullName, H: hint})
			}
		} else {
			// Flat: command.ext (no namespace)
			name := entry.Name()
			if !strings.HasSuffix(name, "."+ext) {
				continue
			}
			cmdName := strings.TrimSuffix(name, "."+ext)

			content, err := os.ReadFile(filepath.Join(dir, name))
			hint := ""
			if err == nil {
				hint = extractDescription(string(content), ext)
			}
			items = append(items, autocompleteItem{V: cmdName, H: hint})
		}
	}
	return items
}

// extractDescription extracts the description from a slash command file.
// For .md files, it looks for YAML frontmatter: ---\ndescription: ...\n---
// For .toml files, it looks for: description = "..."
func extractDescription(content string, ext string) string {
	switch ext {
	case "md":
		return extractMDDescription(content)
	case "toml":
		return extractTOMLDescription(content)
	}
	return ""
}

func extractMDDescription(content string) string {
	if !strings.HasPrefix(content, "---\n") {
		return ""
	}
	end := strings.Index(content[4:], "\n---")
	if end < 0 {
		return ""
	}
	frontmatter := content[4 : 4+end]
	for _, line := range strings.Split(frontmatter, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "description:") {
			desc := strings.TrimPrefix(line, "description:")
			desc = strings.TrimSpace(desc)
			// Strip surrounding quotes if present
			desc = strings.Trim(desc, "\"'")
			return desc
		}
	}
	return ""
}

func extractTOMLDescription(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "description") {
			// description = "..."
			parts := strings.SplitN(line, "=", 2)
			if len(parts) != 2 {
				continue
			}
			val := strings.TrimSpace(parts[1])
			val = strings.Trim(val, "\"'")
			return val
		}
	}
	return ""
}

// filterAutocomplete filters autocomplete items by fuzzy matching the query
// against the value (case-insensitive, greedy left-to-right).
func filterAutocomplete(items []autocompleteItem, query string) []autocompleteItem {
	if query == "" {
		return items
	}
	query = strings.ToLower(query)
	var result []autocompleteItem
	for _, item := range items {
		if fuzzyMatch(strings.ToLower(item.V), query) {
			result = append(result, item)
		}
	}
	return result
}

// fuzzyMatch returns true if all characters in query appear in s in order
// (greedy left-to-right, case already lowered by caller).
func fuzzyMatch(s, query string) bool {
	qi := 0
	for i := 0; i < len(s) && qi < len(query); i++ {
		if s[i] == query[qi] {
			qi++
		}
	}
	return qi == len(query)
}
