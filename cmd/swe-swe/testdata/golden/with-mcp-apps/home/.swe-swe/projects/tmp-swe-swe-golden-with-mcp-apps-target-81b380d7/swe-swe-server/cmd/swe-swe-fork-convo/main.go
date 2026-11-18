// swe-swe-fork-convo forks a coding-agent conversation at an arbitrary
// message and prints the new session id and file path.
//
// Usage:
//
//	swe-swe-fork-convo claude <session-uuid> <message-uuid>
//	swe-swe-fork-convo claude <session-uuid> last-chat-reply [--tool send_message]
//	swe-swe-fork-convo codex  <session-uuid> <call-id>
//	swe-swe-fork-convo codex  <session-uuid> last-chat-reply [--tool send_message]
//	swe-swe-fork-convo pi     <session-uuid> <entry-id>
//
// This CLI is intentionally a thin wrapper around the forkconvo package so
// the same fork primitives can be invoked from the swe-swe-server /api/fork
// handler with identical behaviour.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/choonkeat/swe-swe/forkconvo"
)

func main() {
	if len(os.Args) < 4 {
		usage()
		os.Exit(2)
	}
	agentName := os.Args[1]
	sessionID := os.Args[2]
	anchor := os.Args[3]

	fs := flag.NewFlagSet("swe-swe-fork-convo", flag.ExitOnError)
	tool := fs.String("tool", forkconvo.DefaultChatTool,
		"agent-chat MCP tool name to anchor on when anchor=last-chat-reply")
	if err := fs.Parse(os.Args[4:]); err != nil {
		die(err)
	}

	agent, err := forkconvo.ParseAgent(agentName)
	if err != nil {
		die(err)
	}

	res, err := forkconvo.Fork(forkconvo.Opts{
		Agent:           agent,
		SourceSessionID: sessionID,
		Anchor:          anchor,
		Tool:            *tool,
	})
	if err != nil {
		die(err)
	}

	if anchor == forkconvo.AnchorLastChatReply {
		fmt.Fprintf(os.Stderr, "# anchor resolved to: %s\n", res.AnchorUUID)
	}
	fmt.Printf("forked: %s\n", res.NewSourcePath)
	fmt.Printf("resume: %s\n", resumeHint(agent, res.NewSessionID))
}

func resumeHint(a forkconvo.Agent, id string) string {
	switch a {
	case forkconvo.AgentClaude:
		return fmt.Sprintf("claude --resume %s", id)
	case forkconvo.AgentCodex:
		return fmt.Sprintf("codex resume %s", id)
	case forkconvo.AgentPi:
		return fmt.Sprintf("pi resume %s   (or use pi's /fork interactively)", id)
	}
	return id
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  swe-swe-fork-convo claude <session-uuid> <message-uuid|last-chat-reply> [--tool send_message]
  swe-swe-fork-convo codex  <session-uuid> <call-id|last-chat-reply>      [--tool send_message]
  swe-swe-fork-convo pi     <session-uuid> <entry-id>`)
}

func die(err error) {
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	os.Exit(1)
}
