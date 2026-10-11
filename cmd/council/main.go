// Command council drives several LLM coding CLIs (Codex, Claude Code, Gemini
// CLI, Antigravity, or any custom harness) as participants in a group chat
// and prints the group's consensus answer.
package main

import (
	"os"

	"council/internal/cli"
)

func main() {
	st, err := os.Stdin.Stat()
	tty := err == nil && st.Mode()&os.ModeCharDevice != 0
	os.Exit(cli.Main(os.Args[1:], cli.Env{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, StdinIsTTY: tty}))
}
