package main

import (
	"strings"
	"testing"
)

// Codex's shared app-server daemon launches every session's MCP servers with
// the env of the session that started it, so a second session gets the first
// one's AGENT_CHAT_PORT and AGENT_CHAT_EXPORT_DIR (and no working chat).
// Every Codex launch, including resume, must opt out of the shared daemon.
func TestCodexCommandsDoNotShareTheDaemon(t *testing.T) {
	for _, cfg := range assistantConfigs {
		if cfg.Binary != "codex" {
			continue
		}
		for _, cmd := range []string{cfg.ShellCmd, cfg.ShellRestartCmd, cfg.YoloShellCmd, cfg.YoloRestartCmd} {
			if !strings.Contains(cmd, " --no-daemon") {
				t.Errorf("codex command %q lacks --no-daemon", cmd)
			}
		}
		return
	}
	t.Fatal("no codex assistant config")
}
