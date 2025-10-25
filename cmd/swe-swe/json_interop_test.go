package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestJSONInteropData represents test cases for JSON interchange
type TestJSONInteropData struct {
	Description string          `json:"description"`
	GoStruct    interface{}     `json:"goStruct"`
	JSONString  json.RawMessage `json:"jsonString"`
}

// TestGenerateJSONTestCases generates JSON test cases for Elm to validate
func TestGenerateJSONTestCases(t *testing.T) {
	testCases := []TestJSONInteropData{
		// ChatItem test cases
		{
			Description: "ChatItem with type and sender",
			GoStruct: ChatItem{
				Type:   "user",
				Sender: "USER",
			},
			JSONString: json.RawMessage(`{"type":"user","sender":"USER"}`),
		},
		{
			Description: "ChatItem with content",
			GoStruct: ChatItem{
				Type:    "content",
				Content: "Hello, world!",
			},
			JSONString: json.RawMessage(`{"type":"content","content":"Hello, world!"}`),
		},
		{
			Description: "ChatItem permission request",
			GoStruct: ChatItem{
				Type:      "permission_request",
				Content:   "Permission required for Write tool",
				Sender:    "Write",
				ToolInput: `{"file_path": "/test.txt", "content": "test"}`,
			},
			JSONString: json.RawMessage(`{"type":"permission_request","sender":"Write","content":"Permission required for Write tool","toolInput":"{\"file_path\": \"/test.txt\", \"content\": \"test\"}"}`),
		},
		// ClaudeMessage test cases
		{
			Description: "ClaudeMessage with session ID",
			GoStruct: ClaudeMessage{
				Type:      "session",
				SessionID: "test-session-123",
			},
			JSONString: json.RawMessage(`{"type":"session","session_id":"test-session-123"}`),
		},
		{
			Description: "ClaudeMessage with assistant text",
			GoStruct: ClaudeMessage{
				Type: "assistant",
				Message: &ClaudeMessageContent{
					Role: "assistant",
					Content: []ClaudeContent{
						{
							Type: "text",
							Text: "I'll help you with that.",
						},
					},
				},
			},
			JSONString: json.RawMessage(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"I'll help you with that."}]}}`),
		},
		{
			Description: "ClaudeMessage with tool use",
			GoStruct: ClaudeMessage{
				Type: "assistant",
				Message: &ClaudeMessageContent{
					Role: "assistant",
					Content: []ClaudeContent{
						{
							Type:  "tool_use",
							Name:  "Read",
							ID:    "tool-use-123",
							Input: json.RawMessage(`{"file_path":"/test/file.go","limit":100}`),
						},
					},
				},
			},
			JSONString: json.RawMessage(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Read","input":{"file_path":"/test/file.go","limit":100},"id":"tool-use-123"}]}}`),
		},
		{
			Description: "ClaudeMessage with tool result",
			GoStruct: ClaudeMessage{
				Type: "user",
				Message: &ClaudeMessageContent{
					Role: "user",
					Content: []ClaudeContent{
						{
							Type:      "tool_result",
							Content:   "File contents here",
							ToolUseID: "tool-use-123",
							IsError:   false,
						},
					},
				},
			},
			JSONString: json.RawMessage(`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"File contents here","tool_use_id":"tool-use-123"}]}}`),
		},
		{
			Description: "ClaudeMessage with error tool result",
			GoStruct: ClaudeMessage{
				Type: "user",
				Message: &ClaudeMessageContent{
					Role: "user",
					Content: []ClaudeContent{
						{
							Type:      "tool_result",
							Content:   "This command requires approval: Write",
							ToolUseID: "tool-use-456",
							IsError:   true,
						},
					},
				},
			},
			JSONString: json.RawMessage(`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"This command requires approval: Write","tool_use_id":"tool-use-456","is_error":true}]}}`),
		},
		// ClientMessage test cases
		{
			Description: "ClientMessage basic",
			GoStruct: ClientMessage{
				Type:    "message",
				Sender:  "USER",
				Content: "Hello",
			},
			JSONString: json.RawMessage(`{"type":"message","sender":"USER","content":"Hello"}`),
		},
		{
			Description: "ClientMessage with session IDs",
			GoStruct: ClientMessage{
				Type:            "message",
				Sender:          "USER",
				Content:         "Test message",
				SessionID:       "browser-session-123",
				ClaudeSessionID: "claude-session-456",
			},
			JSONString: json.RawMessage(`{"type":"message","sender":"USER","content":"Test message","sessionID":"browser-session-123","claudeSessionID":"claude-session-456"}`),
		},
		{
			Description: "ClientMessage permission response",
			GoStruct: ClientMessage{
				Type:            "permission_response",
				AllowedTools:    []string{"Read", "Write", "Edit"},
				SkipPermissions: false,
			},
			JSONString: json.RawMessage(`{"type":"permission_response","allowedTools":["Read","Write","Edit"]}`),
		},
		{
			Description: "ClientMessage skip all permissions",
			GoStruct: ClientMessage{
				Type:            "permission_response",
				SkipPermissions: true,
			},
			JSONString: json.RawMessage(`{"type":"permission_response","skipPermissions":true}`),
		},
		{
			Description: "ClientMessage fuzzy search",
			GoStruct: ClientMessage{
				Type:       "fuzzy_search",
				Query:      "test.go",
				MaxResults: 10,
			},
			JSONString: json.RawMessage(`{"type":"fuzzy_search","query":"test.go","maxResults":10}`),
		},
		// ToolUseInfo test cases
		{
			Description: "ToolUseInfo for Read",
			GoStruct: ToolUseInfo{
				Name:  "Read",
				Input: `{"file_path": "/src/main.go"}`,
			},
			JSONString: json.RawMessage(`{"name":"Read","input":"{\"file_path\": \"/src/main.go\"}"}`),
		},
		{
			Description: "ToolUseInfo for TodoWrite",
			GoStruct: ToolUseInfo{
				Name:  "TodoWrite",
				Input: `{"todos": [{"content": "Task 1", "status": "pending", "activeForm": "Working on Task 1"}]}`,
			},
			JSONString: json.RawMessage(`{"name":"TodoWrite","input":"{\"todos\": [{\"content\": \"Task 1\", \"status\": \"pending\", \"activeForm\": \"Working on Task 1\"}]}"}`),
		},
	}

	// Create output directory for test files
	outputDir := filepath.Join(".", "testdata")
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		t.Fatalf("Failed to create output directory: %v", err)
	}

	// Test encoding each struct and compare with expected JSON
	for _, tc := range testCases {
		t.Run(tc.Description, func(t *testing.T) {
			// Marshal the Go struct
			encoded, err := json.Marshal(tc.GoStruct)
			if err != nil {
				t.Errorf("Failed to marshal Go struct: %v", err)
				return
			}

			// Compare with expected JSON (unmarshal both and compare)
			var encodedData, expectedData interface{}
			if err := json.Unmarshal(encoded, &encodedData); err != nil {
				t.Errorf("Failed to unmarshal encoded data: %v", err)
				return
			}
			if err := json.Unmarshal(tc.JSONString, &expectedData); err != nil {
				t.Errorf("Failed to unmarshal expected data: %v", err)
				return
			}

			// Deep equal comparison
			encodedJSON, _ := json.Marshal(encodedData)
			expectedJSON, _ := json.Marshal(expectedData)
			if string(encodedJSON) != string(expectedJSON) {
				t.Errorf("JSON mismatch\nGot:      %s\nExpected: %s", string(encodedJSON), string(expectedJSON))
			}
		})
	}

	// Generate a combined test file for Elm
	allTestCases := make([]map[string]interface{}, 0, len(testCases))
	for _, tc := range testCases {
		encoded, _ := json.Marshal(tc.GoStruct)
		allTestCases = append(allTestCases, map[string]interface{}{
			"description": tc.Description,
			"json":        json.RawMessage(encoded),
		})
	}

	testFile := map[string]interface{}{
		"generated_by": "Go json_interop_test.go",
		"test_cases":   allTestCases,
	}

	jsonData, err := json.MarshalIndent(testFile, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal test file: %v", err)
	}

	outputPath := filepath.Join(outputDir, "go_json_test_cases.json")
	if err := os.WriteFile(outputPath, jsonData, 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	t.Logf("Generated test cases file: %s", outputPath)
}

// TestDecodeElmGeneratedJSON tests that Go can decode JSON generated by Elm
func TestDecodeElmGeneratedJSON(t *testing.T) {
	// This test will read Elm-generated JSON test cases if they exist
	elmTestFile := filepath.Join(".", "testdata", "elm_json_test_cases.json")
	
	// Check if Elm test file exists
	if _, err := os.Stat(elmTestFile); os.IsNotExist(err) {
		t.Skip("Elm test file not found, skipping Elm->Go validation")
		return
	}

	data, err := os.ReadFile(elmTestFile)
	if err != nil {
		t.Fatalf("Failed to read Elm test file: %v", err)
		return
	}

	var testData struct {
		TestCases []struct {
			Description string          `json:"description"`
			Type        string          `json:"type"`
			JSON        json.RawMessage `json:"json"`
		} `json:"test_cases"`
	}

	if err := json.Unmarshal(data, &testData); err != nil {
		t.Fatalf("Failed to unmarshal Elm test data: %v", err)
	}

	for _, tc := range testData.TestCases {
		t.Run(tc.Description, func(t *testing.T) {
			switch tc.Type {
			case "ChatItem":
				var item ChatItem
				if err := json.Unmarshal(tc.JSON, &item); err != nil {
					t.Errorf("Failed to unmarshal ChatItem: %v", err)
				}
			case "ClaudeMessage":
				var msg ClaudeMessage
				if err := json.Unmarshal(tc.JSON, &msg); err != nil {
					t.Errorf("Failed to unmarshal ClaudeMessage: %v", err)
				}
			case "ClientMessage":
				var msg ClientMessage
				if err := json.Unmarshal(tc.JSON, &msg); err != nil {
					t.Errorf("Failed to unmarshal ClientMessage: %v", err)
				}
			case "ToolUseInfo":
				var info ToolUseInfo
				if err := json.Unmarshal(tc.JSON, &info); err != nil {
					t.Errorf("Failed to unmarshal ToolUseInfo: %v", err)
				}
			default:
				t.Errorf("Unknown type: %s", tc.Type)
			}
		})
	}
}