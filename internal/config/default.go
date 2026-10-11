package config

// DefaultYAML is written by `council init` and used when no config exists.
const DefaultYAML = `# council configuration
# Location: ~/.config/council/config.yaml (override with --config or $COUNCIL_CONFIG)
#
# council never stores credentials. Each harness uses its own CLI's login
# (run "council login <agent>"); check with "council doctor".

defaults:
  # Agents used by "council ask" when --agents is not given.
  agents: [codex, claude, agy]
  # Maximum discussion rounds (round 1 = independent answers).
  rounds: 3
  # "majority" for a plain vote, or the name of an agent to act as moderator.
  moderator: majority
  # Per-turn timeout for each agent.
  timeout: 10m
  # After round 1, require every agent to explicitly set "agree": true.
  require_explicit_agreement: true
  # Truncate each earlier message shown to agents (0 = unlimited).
  max_transcript_chars: 0

agents:
  # Built-in harnesses. Any field below overrides the preset, e.g. model,
  # extra_args, timeout, env, command (path to the binary).
  codex:
    harness: codex          # codex exec --skip-git-repo-check --ephemeral --sandbox read-only -o FILE -
    # model: gpt-5-codex
  claude:
    harness: claude         # claude -p --output-format json --no-session-persistence  (prompt on stdin)
    # model: sonnet
    # extra_args: ["--permission-mode", "plan"]
  agy:
    harness: antigravity    # agy --input-format stream-json --output-format stream-json
    # model: gemini-3.1-pro-high   (see "agy models")
  opencode:
    harness: opencode       # opencode run --format json --agent plan  (prompt on stdin)
    # model: anthropic/claude-sonnet-4-5   (provider/model, see "opencode models")

  # Generic harness example: any CLI that reads a prompt and prints an answer.
  # ollama:
  #   harness: generic
  #   command: ollama
  #   args: ["run", "llama3.1"]
  #   prompt_mode: stdin        # arg | stdin | file
  #   output: text              # text | json | jsonl
  #   timeout: 5m
  #
  # Placeholders usable in args: {{prompt}} {{prompt_file}} {{output_file}}
  # {{model}} {{workdir}}, plus whole-argument {{model_args}} / {{extra_args}}.
  # my-json-tool:
  #   harness: generic
  #   command: my-llm
  #   args: ["--input", "{{prompt_file}}", "--format", "json"]
  #   prompt_mode: file
  #   output: json
  #   json_path: choices.0.message.content
  #   error_path: error
  #   login_args: ["login"]
  #   status_args: ["whoami"]
`
