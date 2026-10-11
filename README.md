# council

`council` turns several LLM coding CLIs ("harnesses") into participants in a
group chat and returns the group's **consensus answer**.

Every agent sees the shared transcript. Round 1 is independent answers; in
later rounds each agent reads the others' answers, critiques them and revises.
Each turn ends with a structured JSON vote. The chat stops as soon as everyone
agrees (or after `--rounds`), then a moderator agent or a majority vote
produces the final answer.

Built-in harnesses:

| Agent name      | CLI                    | Headless invocation used by council |
|-----------------|------------------------|-------------------------------------|
| `codex`         | OpenAI Codex CLI       | `codex exec --skip-git-repo-check --ephemeral --sandbox read-only --color never --output-last-message <tmp> -` (prompt on stdin) |
| `claude`        | Claude Code            | `claude -p --output-format json --no-session-persistence` (prompt on stdin, answer = `.result`, failure = `.is_error`) |
| `gemini`        | Gemini CLI             | `gemini --output-format json -p "<short instruction>"` (prompt on stdin, answer = `.response`, failure = `.error`) |
| `agy`           | Google Antigravity CLI | `agy --input-format stream-json --output-format stream-json` (prompt sent as a `{"event":"user",...}` line on stdin, answer = `result.response` of the final `result` event) |
| *anything else* | `generic`              | configurable command template, prompt via arg / stdin / temp file, text / JSON / JSONL output with a JSON path, timeout |

council never stores credentials. It only calls each CLI's own login and
status commands.

## Install

```sh
# from source (Go 1.24+)
go install ./cmd/council          # or: make build

# or pick a prebuilt binary from dist/
#   council-linux-amd64  council-linux-arm64  council-darwin-amd64
#   council-darwin-arm64 council-windows-amd64.exe
```

## Quick start

```sh
council init                    # writes ~/.config/council/config.yaml
council doctor                  # which harnesses are installed / signed in?
council login claude            # passes through to `claude auth login`
council login codex             # passes through to `codex login`
council login codex -- --device-auth   # extra args go to the CLI's login command

council ask "Is a sync.RWMutex faster than a sync.Mutex for a read-heavy map in Go?"
```

### Asking

```sh
council ask "prompt"                                 # prompt as argument
echo "prompt" | council ask                          # prompt on stdin
council ask -f question.md                           # prompt from a file
git diff | council ask "Review this diff" -          # argument + stdin ("-")
council ask -f spec.md "Which design is simplest?"   # argument + file

council ask --agents codex,claude,gemini --rounds 4 "…"
council ask --moderator claude "…"        # claude writes the final synthesis
council ask --moderator majority "…"      # plain majority vote (default)
council ask --json "…"                    # full result + transcript as JSON
council ask --transcript chat.md "…"      # save the group chat as Markdown
council ask --verbose "…"                 # stream the group chat live (stderr)
council ask --timeout 5m --loose "…"      # per-turn timeout; accept identical answers without explicit agree=true
```

Flags may appear before or after the prompt. The answer goes to stdout and a
one-line status (`── consensus after 2 round(s) · method: unanimous · for: …`)
goes to stderr, so `council ask … > answer.txt` captures just the answer.

### Other commands

```sh
council doctor [--probe] [--json] [agent…]   # alias: council status
council agents                               # show every agent's resolved command line
council init [--force] [--config path]
council version
```

`doctor` reports, for each agent: installed (on `PATH`), version, and auth state:

* **codex**: `codex login status` (exit 0 means signed in)
* **claude**: `claude auth status` (exit 0 means signed in)
* **gemini**: no status subcommand exists, so it checks `GEMINI_API_KEY` / `GOOGLE_API_KEY`,
  `GOOGLE_GENAI_USE_VERTEXAI=true`, or a cached login in `~/.gemini/oauth_creds.json`
* **agy**: sessions live in the OS keyring and there is no documented status command, so it
  reports `unknown` unless `modelProvider: gemini` + `GEMINI_API_KEY` or `AGY_ADC_AUTH=true` is set

`--probe` sends a one-line prompt through each installed harness to prove it
really works end to end. `doctor` exits 1 when any checked harness is missing or
signed out.

## Try it without any LLM CLI

`examples/fake-agent.sh` is a deterministic fake harness, and
`examples/fake-council.yaml` wires three of them up through the generic adapter:

```console
$ council ask --config examples/fake-council.yaml --verbose "What is 6 * 7?"

━━━━━━━━ Round 1 ━━━━━━━━
▶ alpha (3ms)
  [alpha] Working independently: my answer is 42.
  ```json
  {"final_answer": "42", "agree": false, "confidence": 0.6}
  ```
▶ gamma (205ms)
  [gamma] Working independently: my answer is 41.
  …
━━━━━━━━ Round 2 ━━━━━━━━
▶ gamma (207ms)
  [gamma] I reviewed the other answers; the majority answer 42 is right and my 41 was a mistake. Revising.
  …
✔ consensus reached in round 2: 42
[beta] The others now agree with my answer 42. Agreed.

Final answer: 42
── consensus after 2 round(s) · method: unanimous · for: alpha, beta, gamma
```

`make demo` does the same.

## Configuration

`~/.config/council/config.yaml` (or `$XDG_CONFIG_HOME/council/config.yaml`,
`$COUNCIL_CONFIG`, `--config`). `council init` writes a commented default:

```yaml
defaults:
  agents: [codex, claude, gemini]   # used when --agents is omitted
  rounds: 3
  moderator: majority               # or an agent name, e.g. claude
  timeout: 10m                      # per agent turn
  require_explicit_agreement: true  # after round 1, every agent must vote agree=true
  max_transcript_chars: 0           # truncate each earlier message shown to agents

agents:
  codex:  {harness: codex}
  claude: {harness: claude, model: sonnet}
  gemini: {harness: gemini}
  agy:    {harness: antigravity}

  # Two Claude agents with different models can sit at the same table:
  claude-opus:
    harness: claude
    model: opus
    extra_args: ["--permission-mode", "plan"]
    timeout: 15m
```

Fields for any agent (built-ins use them as overrides):

| field | meaning |
|-------|---------|
| `harness` | `codex`, `claude`, `gemini`, `antigravity` (alias `agy`) or `generic` |
| `command` | binary to run (default: the CLI's name; `~` and `$VARS` expanded) |
| `args` | full argument template (replaces the preset's) |
| `extra_args` | appended where the template has `{{extra_args}}` (or at the end) |
| `model`, `model_flag` | `{{model_args}}` expands to `model_flag model` when a model is set |
| `prompt_mode` | `arg` (`{{prompt}}` or last argument), `stdin`, or `file` (`{{prompt_file}}`) |
| `stdin_template` | text written to stdin instead of the raw prompt; supports `{{prompt}}` and `{{prompt_json}}` |
| `output` | `text`, `json` (one document) or `jsonl` (NDJSON; the last line that has `json_path` wins) |
| `output_source` | `stdout` (default) or `file` (read `{{output_file}}` after the run) |
| `json_path` | dot path to the answer, e.g. `result`, `choices.0.message.content` |
| `error_path` | dot path; a truthy value marks the turn as failed |
| `timeout` | per-turn timeout (`90s`, `10m`) |
| `env` | extra environment variables (values may reference `$VARS`) |
| `workdir` | working directory for the CLI |
| `login_args`, `status_args` | appended to `command` for `council login` / `council doctor` |

Generic example:

```yaml
agents:
  ollama:
    harness: generic
    command: ollama
    args: ["run", "llama3.1"]
    prompt_mode: stdin
    output: text
    timeout: 5m
  my-api-wrapper:
    harness: generic
    command: my-llm
    args: ["--input", "{{prompt_file}}", "--format", "json", "{{model_args}}"]
    model: big-model
    model_flag: --model
    prompt_mode: file
    output: json
    json_path: choices.0.message.content
    error_path: error
    login_args: ["login"]
    status_args: ["whoami"]
```

If you prefer Antigravity's plain print mode over its stream-json session mode:

```yaml
  agy-print:
    harness: antigravity
    args: ["-p", "{{prompt}}", "--output-format", "json", "{{model_args}}", "{{extra_args}}"]
    prompt_mode: arg
    stdin_template: ""
    output: json
    json_path: response
    error_path: error
```

## How the group chat works

1. **Round 1:** every agent gets the user request and answers independently.
   All agents run concurrently (one goroutine each, each with its own context timeout).
2. **Round N > 1:** every agent gets the request plus the shared transcript of all
   earlier rounds (each agent's full message), is told to critique, revise and try
   to converge, and is told when it is in the final round.
3. **Votes:** every message must end with a fenced JSON block:

   ````
   ```json
   {"final_answer": "…", "agree": true, "agrees_with": ["claude"], "confidence": 0.8}
   ```
   ````

   The parser takes the last such block. It falls back to a bare `{…}` object and
   accepts the `answer`/`agreement` aliases. A message without a vote counts as an abstention.
4. **Consensus:** a round has consensus when every responding agent voted, all
   normalized `final_answer`s match (case, whitespace, quotes and trailing punctuation
   are ignored), at least `min(2, #agents)` voted, and (from round 2 on, unless
   `--loose`) everyone set `"agree": true`. Unanimous identical answers in round 1
   stop the chat immediately.
5. **Synthesis:** with `--moderator <agent>`, that agent reads the whole discussion
   plus the vote tally and writes the final answer and its own vote. Otherwise the
   latest vote of each agent is tallied by majority, with ties broken by summed
   confidence and then agent order. If the moderator fails, council falls back to the vote.
6. Agents that error or time out are shown as `(no response this round: …)` and
   do not block the others. The run fails only if every agent fails in a round.

## Architecture

```
cmd/council/          main: wires stdin/stdout into internal/cli
internal/cli/         subcommands (ask, init, doctor/status, login, agents), flag parsing, live printer
internal/config/      YAML config, defaults, preset overrides, `init` template
internal/harness/     Harness interface; CommandHarness (subprocess runner: placeholders,
                      prompt modes, JSON/JSONL extraction, process-group kill on timeout);
                      presets for codex/claude/gemini/antigravity; doctor checks; Func (in-process fake)
internal/chat/        Transcript, prompt construction, Council orchestrator (rounds, concurrency,
                      consensus loop, moderator synthesis, Markdown rendering)
internal/consensus/   vote parsing, answer normalization, agreement detection, majority vote
examples/             fake-agent.sh + fake-council.yaml (demo/testing without real CLIs)
scripts/build.sh      cross-compilation
```

## Development

```sh
make vet test        # go vet ./... && go test ./...
go test -race ./...
make demo            # consensus run against the fake agents
make dist            # CGO_ENABLED=0 builds for darwin/{amd64,arm64}, linux/{amd64,arm64}, windows/amd64
make zip source-zip  # ../council-binaries.zip, ../council-source.zip
```

The tests need no LLM CLI. `internal/chat` drives the full consensus loop with
in-process fake harnesses (`harness.Func`), covering convergence, majority
fallback, moderator synthesis, failures, timeouts and concurrency.
`internal/harness` exercises the subprocess runner with tiny shell-script fakes
that mimic each CLI's output format, and `internal/cli` runs `council ask`
end to end against `examples/fake-agent.sh`.

## Limitations

* The built-in flags follow each CLI's documentation as of October 2026 and were not
  run against the real binaries here. If a CLI changes its flags, override `args`
  in the config.
* Agents are asked not to edit files (Codex also runs with `--sandbox read-only`),
  but Claude/Gemini/Antigravity use their own permission defaults. In headless mode
  those deny tools that need approval. Tighten this with `extra_args`.
* Each turn is a fresh, stateless CLI invocation that carries the whole transcript in the
  prompt, so long discussions cost tokens. Use `max_transcript_chars` or fewer rounds.
* Consensus matching is textual (normalized). Semantically equal answers with
  different wording only match once agents copy each other's wording, which the prompt asks them to do,
  or through a moderator.
* Live streaming is per message (each reply prints as it completes), not token by token.
* `gemini` and `agy` have no non-interactive auth-status command, so `doctor` infers
  their status. Use `doctor --probe` for a definite answer.
* The fake agent is a POSIX shell script; the shell-based tests skip on Windows.
