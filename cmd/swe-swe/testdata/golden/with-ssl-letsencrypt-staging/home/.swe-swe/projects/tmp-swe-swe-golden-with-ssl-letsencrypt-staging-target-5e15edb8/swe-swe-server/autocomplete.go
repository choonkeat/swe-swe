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

	dir, ext := slashCommandDirForAgent(sess.Assistant, sess.AssistantConfig.SlashCmdFormat)
	if dir == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(autocompleteResponse{Results: []autocompleteItem{}})
		return
	}

	items := discoverSlashCommands(dir, ext)
	if req.Query != "" {
		items = filterAutocomplete(items, req.Query)
	}
	if items == nil {
		items = []autocompleteItem{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(autocompleteResponse{Results: items})
}

// slashCommandDirForAgent returns the slash command directory and file extension
// for the given agent type.
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

// discoverSlashCommands scans a slash command directory and returns all
// commands with their descriptions. Directory structure is:
//
//	commands/
//	  namespace/
//	    command.md (or .toml)
//
// Returns items with V="namespace:command" and H=description.
func discoverSlashCommands(dir string, ext string) []autocompleteItem {
	namespaces, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var items []autocompleteItem
	for _, ns := range namespaces {
		if !ns.IsDir() {
			continue
		}
		nsName := ns.Name()
		nsPath := filepath.Join(dir, nsName)
		entries, err := os.ReadDir(nsPath)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if !strings.HasSuffix(name, "."+ext) {
				continue
			}
			cmdName := strings.TrimSuffix(name, "."+ext)
			fullName := nsName + ":" + cmdName

			// Read file to extract description
			content, err := os.ReadFile(filepath.Join(nsPath, name))
			hint := ""
			if err == nil {
				hint = extractDescription(string(content), ext)
			}

			items = append(items, autocompleteItem{V: fullName, H: hint})
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
