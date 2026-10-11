// Command council drives several LLM coding CLIs (OpenAI Codex, Claude Code,
// Gemini CLI, Google Antigravity, or any custom "generic" harness) as
// participants in a group chat and prints the group's consensus answer.
//
// Every agent answers the user's request independently in round 1. In later
// rounds each agent reads the shared transcript, critiques the others and
// revises its answer. Every message ends with a fenced JSON vote. The chat
// stops as soon as the votes agree (or after the round limit), and the final
// answer comes from that consensus, a majority vote, or a moderator agent's
// synthesis.
//
// Usage:
//
//	council ask [flags] "prompt"       ask the council (prompt may also come from --file or stdin)
//	council init [--force]             write a default config to ~/.config/council/config.yaml
//	council doctor [--probe] [agents]  check each harness is installed and signed in (alias: status)
//	council login <agent> [-- args]    run the harness CLI's own login flow
//	council agents                     list configured agents and how they are invoked
//	council version
//
// Run "council <command> -h" for the flags of a command. The README and the
// docs/ directory of the repository describe configuration, harnesses and
// the consensus algorithm in detail.
package main
