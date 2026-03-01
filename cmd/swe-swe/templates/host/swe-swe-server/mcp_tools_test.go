package main

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestRegisterOrchestrationTools(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "test",
		Version: "0.0.0",
	}, nil)
	if err := registerOrchestrationTools(server); err != nil {
		t.Fatalf("registerOrchestrationTools failed: %v", err)
	}
}
