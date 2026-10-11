# Configuration

council is configured with one YAML file. It is optional: without it, the
built-in defaults are used, which are the same as the file `council init`
writes.

- [Location](#location)
- [Structure](#structure)
- [`defaults`](#defaults)
- [`agents`](#agents)
- [How agent entries are resolved](#how-agent-entries-are-resolved)
- [Command-line flags versus config](#command-line-flags-versus-config)
- [Complete annotated example](#complete-annotated-example)
- [The default file](#the-default-file)
- [Validation](#validation)
- [Recipes](#recipes)

## Location

The first match wins:

1. `--config PATH` on any command (`ask`, `init`, `doctor`, `login`, `agents`).
2. `$COUNCIL_CONFIG`.
3. `$XDG_CONFIG_HOME/council/` if `XDG_CONFIG_HOME` is set, otherwise
   `~/.config/council/`. In that directory council uses the first existing
   file of `config.yaml`, `config.yml` and `config`, and falls back to
   `config.yaml` when none exists.

The `~/.config` location is used on every OS, so on Windows it is
`%USERPROFILE%\.config\council\config.yaml`. `council doctor` and
`council agents` print the path they used.

A missing file is not an error. council then behaves as if the
[default file](#the-default-file) were there. `council init` writes that file
so you can edit it. It refuses to overwrite an existing file without `--force`,
and creates the directory if needed.

## Structure

```yaml
defaults:        # settings for `council ask`
  …
agents:          # named participants (and possible moderators)
  <name>:
    harness: …
    …
```

Both sections are optional. Unknown keys anywhere are an error (for example
`field moderatr not found in type config.Defaults`), so typos do not go
unnoticed.

## `defaults`

| Key | Type | Default | Meaning |
|---|---|---|---|
| `agents` | list of names | `[codex, claude, agy]` | Agents used by `council ask` when `--agents` is not given. Order matters: it is the order of messages in the transcript and the last tie-breaker of the majority vote. Names are checked when `ask` runs, not when the file loads. |
| `rounds` | integer ≥ 1 | `3` | Maximum number of rounds. Round 1 is the independent answer; the chat stops early on consensus. `0` or a missing value means 3. |
| `moderator` | `majority` or an agent name | `majority` | Who decides when there is no consensus, and who writes the final synthesis. With an agent name, that agent always runs after the discussion. See [CONSENSUS.md](CONSENSUS.md#moderator). (`vote` and `none` are accepted by `--moderator`, not here.) |
| `timeout` | duration | `10m` | Per-turn limit for every agent without its own `timeout`. Go duration syntax: `90s`, `5m`, `1h30m`. `0` means the default (10m); turns cannot be unlimited. |
| `require_explicit_agreement` | bool | `true` | From round 2 on, consensus also needs every voter to set `"agree": true`, not only to give the same answer. `--loose` turns it off for one run. |
| `max_transcript_chars` | integer ≥ 0 | `0` | Truncate each earlier message to this many bytes when it is shown to the agents (with `…[truncated]` appended). `0` = no limit. Applies to the agents' and moderator's prompts, not to `--transcript` or `--json`. |

## `agents`

Each key under `agents` is an agent name. Names appear in the chat, in
`--agents`, in `--moderator`, and in `council login <name>`. Every field is
optional except where noted.

| Key | Type | Applies to | Meaning |
|---|---|---|---|
| `harness` | string | all | `codex`, `claude`, `antigravity`, `opencode` or `generic`, case-insensitive. Aliases: `agy` → antigravity, `claude-code` → claude, `open-code` → opencode, `openai` → codex, `custom` → generic. If omitted: the built-in whose name equals the agent name, otherwise `generic`. `gemini` and `gemini-cli` (Gemini CLI, removed in v0.2.1) are an error that suggests `antigravity` or `generic`. |
| `command` | string | all; **required for generic** | Program to run: a name looked up on `PATH`, or a path. A leading `~` and `$VARS` / `${VARS}` are expanded. A relative path (one containing a slash, like `./bin/x`) is resolved against `workdir` if set, otherwise against the directory you run council from. Built-in default: `codex`, `claude`, `agy`, `opencode`. |
| `args` | list of strings | all | Argument template; replaces the preset's entirely. See [placeholders](HARNESSES.md#placeholders). `$VARS` are **not** expanded here. |
| `extra_args` | list of strings | all | Inserted where the template has `{{extra_args}}` (all presets do), or appended after the template. |
| `model` | string | all | Model name. Becomes `model_flag model` through `{{model_args}}`, and replaces `{{model}}`. |
| `model_flag` | string | all | Flag used by `{{model_args}}`. Presets: `--model`. |
| `prompt_mode` | `arg` \| `stdin` \| `file` | all | How the prompt is passed. Presets use `stdin`; generic defaults to `arg`. |
| `stdin_template` | string | `stdin` mode | Written to stdin instead of the raw prompt. `{{prompt}}` = raw prompt; `{{prompt_json}}` = prompt as a JSON string literal. |
| `output` | `text` \| `json` \| `jsonl` | all | How the answer is parsed. Default `text`. |
| `output_source` | `stdout` \| `file` | all | Where the answer is read from. `file` reads `{{output_file}}` after the run (the template must contain it). Default `stdout`. |
| `json_path` | string | `json`, `jsonl` | Dot path to the answer (`result`, `choices.0.message.content`, `items.-1.text`). Empty = whole document. |
| `error_path` | string | `json`, `jsonl` | Dot path; a truthy value there fails the turn. |
| `timeout` | duration | all | Per-turn limit for this agent; overrides `defaults.timeout`. |
| `env` | map of string → string | all | Extra environment variables, added to council's own environment. Values may reference `$VARS`, expanded when the CLI starts. |
| `workdir` | string | all | Working directory for the CLI (`~` expanded, `$VARS` not). Default: the directory you run council from. Also available as `{{workdir}}`. |
| `login_args` | list of strings | all | `council login <name>` runs `command login_args… [args after --]`. Unset = no login command; `[]` = run `command` with no arguments. |
| `status_args` | list of strings | all | `council doctor` and the web UI run `command status_args…`; exit 0 = signed in. Unset = the preset's check (`codex login status`, `claude auth status`, `agy models`, `opencode auth list`), or `unknown` for generic agents. Setting it replaces the preset's check, including how its output is read. |

Durations accept Go syntax (`300ms`, `45s`, `10m`, `1h`). An invalid one is
reported with its line number.

## How agent entries are resolved

When council needs agent `NAME` (from `--agents`, `defaults.agents`,
`--moderator`, `defaults.moderator`, `doctor` or `login`):

1. If `NAME` is not under `agents:` but is a built-in kind (`codex`, `claude`,
   `antigravity`, `agy`, `opencode`, or another alias), it is treated as
   `NAME: {harness: NAME}`. Otherwise it is an error:
   `unknown agent "NAME" (define it under \`agents:\` in <path>)`.
2. The kind is `harness` resolved through the aliases; if empty, the built-in
   named `NAME`, else `generic`.
3. The starting point is the built-in preset for that kind (see
   [HARNESSES.md](HARNESSES.md#built-in-harnesses)), or an empty spec for
   `generic`.
4. Every field you set replaces the preset's value:
   * strings (`command`, `model`, `model_flag`, `prompt_mode`,
     `stdin_template`, `output`, `output_source`, `json_path`, `error_path`,
     `workdir`) override only when non-empty, so you cannot clear a preset
     string with `""`;
   * lists (`args`, `extra_args`, `login_args`, `status_args`) override
     whenever they are present, even as `[]`;
   * `env` is used when it has at least one entry;
   * `timeout` is the agent's value, else `defaults.timeout`.
5. The result is validated (see [Validation](#validation)).

`council agents` shows the resolved command line of every agent defined in the
file. Agents that are only implied by name (step 1) are not listed there, but
`council doctor` checks them if they appear in `defaults.agents`.

## Command-line flags versus config

| `council ask` flag | Overrides |
|---|---|
| `--agents` | `defaults.agents` |
| `--rounds N` (N > 0) | `defaults.rounds` |
| `--moderator` | `defaults.moderator` |
| `--loose` | sets `require_explicit_agreement` to false |
| `--max-context N` (N ≥ 0) | `defaults.max_transcript_chars` |
| `--timeout D` | adds a second per-turn limit on top of each agent's `timeout`; the shorter one wins |

## Complete annotated example

```yaml
# ~/.config/council/config.yaml

defaults:
  # Who takes part when --agents is not given, in this order.
  agents: [codex, claude, agy]
  # Up to 3 rounds: independent answers, then up to two rounds of critique.
  rounds: 3
  # Plain majority vote when there is no consensus. Put an agent name here
  # (e.g. claude) to get a written synthesis every time.
  moderator: majority
  # Each turn of each agent may take up to 10 minutes.
  timeout: 10m
  # From round 2 on, everyone must say "agree": true for consensus.
  require_explicit_agreement: true
  # Show earlier messages in full to the agents.
  max_transcript_chars: 0

agents:
  # ---- built-in harnesses ------------------------------------------------
  codex:
    harness: codex
    model: gpt-5-codex                 # -> codex exec … --model gpt-5-codex … -

  claude:
    harness: claude
    model: sonnet                      # -> claude -p … --model sonnet
    extra_args: ["--permission-mode", "plan"]

  agy:
    harness: antigravity               # alias: agy
    timeout: 15m                       # this one is slower

  opencode:
    harness: opencode
    model: anthropic/claude-sonnet-4-5 # -> opencode run … --model anthropic/claude-sonnet-4-5
    env:
      ANTHROPIC_API_KEY: $MY_ANTHROPIC_KEY   # expanded when the CLI starts

  # Second Claude with a bigger model, e.g. as moderator:
  #   council ask --moderator claude-opus "…"
  claude-opus:
    harness: claude
    model: opus

  # A Codex that may read a specific project:
  codex-repo:
    harness: codex
    workdir: ~/src/myproject

  # ---- generic harnesses -------------------------------------------------
  # Local model via Ollama, prompt on stdin, plain text out.
  llama:
    harness: generic
    command: ollama
    args: ["run", "llama3.1"]
    prompt_mode: stdin
    output: text
    timeout: 5m

  # HTTP API through curl: JSON request built from the prompt, JSON answer.
  llama-http:
    harness: generic
    command: curl
    args: ["-sS", "http://localhost:11434/api/generate", "-d", "@-"]
    prompt_mode: stdin
    stdin_template: '{"model": "llama3.1", "prompt": {{prompt_json}}, "stream": false}'
    output: json
    json_path: response
    error_path: error

  # A tool that reads a prompt file and prints JSON, with its own login.
  my-json-tool:
    harness: generic
    command: ~/bin/my-llm
    args: ["--input", "{{prompt_file}}", "--format", "json", "{{model_args}}", "{{extra_args}}"]
    model: big-model
    model_flag: --model                # {{model_args}} -> --model big-model
    extra_args: ["--temperature", "0"]
    prompt_mode: file
    output: json
    json_path: choices.0.message.content
    error_path: error
    login_args: ["login"]              # council login my-json-tool
    status_args: ["whoami"]            # council doctor: exit 0 = signed in
```

## The default file

`council init` writes this file, and it is what council uses when no file
exists:

```yaml
# council configuration
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

  # (commented generic harness examples follow in the real file)
```

## Validation

The file is validated when it is loaded, before any agent runs. Every agent
under `agents:` and `defaults.moderator` must resolve. Errors are prefixed with
the file path:

| Error | Fix |
|---|---|
| `field X not found in type config.Agent` (or `config.Defaults`, `config.Config`) | Typo or unsupported key. |
| `line N: invalid duration "X"` | Use Go duration syntax such as `90s` or `10m`. |
| `defaults.rounds must be >= 1` | Use a positive number. |
| `defaults.moderator: unknown agent "X" …` | Use `majority` or a defined or built-in agent name. |
| `agent "X": unknown harness "Y" (want one of antigravity, claude, codex, opencode or generic)` | Fix `harness:`. |
| `agent "X": the gemini harness was removed in council v0.2.1 because Gemini CLI is deprecated; use ...` | Switch the agent to `harness: antigravity` (Google's Antigravity CLI) or `harness: generic`, and update `defaults.agents` if it lists it. |
| `agent "X": generic harness needs \`command\`` | Add `command:`. |
| `agent "X": unknown prompt_mode "Y" (want arg, stdin or file)` | Fix `prompt_mode:`. |
| `agent "X": unknown output "Y" (want text, json or jsonl)` | Fix `output:`. |
| `agent "X": unknown output_source "Y" (want stdout or file)` | Fix `output_source:`. |
| `agent "X": output_source=file requires {{output_file}} in args` | Put `{{output_file}}` in `args`. |

Names in `defaults.agents`, `--agents` and `--moderator` are checked when
`council ask` runs; an unknown one is a usage error (exit 2).

## Recipes

**Use a different set of agents for one question**

```sh
council ask --agents claude,claude-opus,opencode "…"
```

**Always get a written synthesis**

```yaml
defaults:
  moderator: claude-opus
```

**Cheaper, faster runs**

```yaml
defaults:
  rounds: 2
  max_transcript_chars: 4000
  timeout: 3m
```

**Keep separate configs per project**

```sh
export COUNCIL_CONFIG="$PWD/.council.yaml"
council init        # writes .council.yaml here
```

**Point at a CLI that is not on PATH**

```yaml
agents:
  claude:
    harness: claude
    command: ~/.local/share/claude/bin/claude
```
