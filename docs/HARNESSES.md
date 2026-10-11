# Harnesses

A **harness** is a command-line program that council can send a prompt to and
get an answer back from. Each participant in the chat (an **agent**) is a named
harness configuration. You can have several agents on the same harness, for
example two Claude Code agents with different models.

council ships six built-in adapters (codex, claude, agy, opencode, cursor and
pi) and a `generic` one that can drive almost any CLI. All of them run on the same code (`harness.CommandHarness`): a
built-in adapter is just a preset of the fields you could also write yourself
in the [config](CONFIGURATION.md).

- [How a turn runs](#how-a-turn-runs)
- [Built-in harnesses](#built-in-harnesses)
  - [codex: OpenAI Codex CLI](#codex-openai-codex-cli)
  - [claude: Claude Code](#claude-claude-code)
  - [agy: Google Antigravity CLI](#agy-google-antigravity-cli)
  - [opencode: OpenCode](#opencode-opencode)
  - [cursor: Cursor CLI](#cursor-cursor-cli)
  - [pi: Pi coding agent](#pi-pi-coding-agent)
  - [Gemini CLI (removed)](#gemini-cli-removed)
- [Overriding a built-in harness](#overriding-a-built-in-harness)
- [Adding a custom harness](#adding-a-custom-harness)
  - [Prompt as an argument](#prompt-as-an-argument)
  - [Prompt on stdin](#prompt-on-stdin)
  - [Prompt in a file](#prompt-in-a-file)
  - [JSON output and JSON paths](#json-output-and-json-paths)
  - [JSON Lines output](#json-lines-output)
  - [Answer written to a file](#answer-written-to-a-file)
  - [Login and status commands](#login-and-status-commands)
  - [Checklist](#checklist)
- [Reference: placeholders, prompt modes and output parsing](#reference)

## How a turn runs

For every turn, `CommandHarness.Run`:

1. Creates a private temp directory `council-<agent>-XXXX`.
2. With `prompt_mode: file`, writes the prompt to `<tmp>/prompt.md` (mode 0600).
3. Expands the argument template (see [placeholders](#placeholders)).
   `<tmp>/output.txt` is the `{{output_file}}`.
4. Starts `command` with those arguments, in `workdir` (or council's current
   directory), with council's environment plus the agent's `env`.
5. With `prompt_mode: stdin`, writes the prompt (or `stdin_template` with the
   prompt filled in) to the process's stdin and closes it.
6. Waits for the process, killing it (and on Unix its whole process group) if
   the agent's `timeout`, `--timeout`, or Ctrl-C fires first.
7. Reads stdout, or the output file with `output_source: file`, and extracts
   the answer according to `output`, `json_path` and `error_path`.
8. Deletes the temp directory.

The process gets no terminal: stdin is either the prompt or empty, and stdout
and stderr are captured. Interactive prompts inside the CLI therefore cannot be
answered. Use each CLI's headless or print mode.

## Built-in harnesses

Use these names directly in `--agents` or `defaults.agents`, even without
defining them under `agents:`. Each preset uses a 10 minute timeout, which the
config's `defaults.timeout` replaces (and that is 10 minutes too unless you
change it).

`council agents` prints the exact resolved command line for every agent in your
config.

### codex: OpenAI Codex CLI

| | |
|---|---|
| Kind / aliases | `codex` / `openai` |
| Command | `codex exec --skip-git-repo-check --ephemeral --sandbox read-only --color never --output-last-message <output_file> [--model M] [extra_args…] -` |
| Prompt | stdin (the trailing `-` tells `codex exec` to read the prompt from stdin) |
| Answer | the file written by `--output-last-message`, as text (`output_source: file`) |
| Login | `council login codex` → `codex login` (ChatGPT sign-in). `-- --device-auth` for a device code on a headless machine; `-- --with-api-key` to read an API key from stdin. |
| Status | `codex login status`; exit 0 = signed in |

Why these flags: `exec` is Codex's non-interactive mode;
`--skip-git-repo-check` lets it run outside a git repository; `--ephemeral`
does not persist the session; `--sandbox read-only` stops the agent from
editing files; `--color never` keeps escape codes out of the output; and
`--output-last-message` writes only the final message, so progress output on
stdout does not matter. If Codex exits successfully without writing that file,
the turn fails with `expected output file was not written`.

### claude: Claude Code

| | |
|---|---|
| Kind / aliases | `claude` / `claude-code` |
| Command | `claude -p --output-format json --no-session-persistence [--model M] [extra_args…]` |
| Prompt | stdin |
| Answer | JSON document on stdout; answer = `result`; failure if `is_error` is true (the `result` text is then used as the error message) |
| Login | `council login claude` → `claude auth login`. `-- --console` for Anthropic Console (API) billing, `-- --sso` for SSO. |
| Status | `claude auth status`; exit 0 = signed in |

`-p` is print (headless) mode, `--output-format json` prints one object such as
`{"type":"result","is_error":false,"result":"…"}`, and
`--no-session-persistence` keeps these runs out of `claude --resume`.
Claude Code keeps its own permission settings. In print mode, tools that need
approval are denied. To be stricter, add for example
`extra_args: ["--permission-mode", "plan"]`.

### agy: Google Antigravity CLI

| | |
|---|---|
| Kind / aliases | `antigravity` / `agy` |
| Command | `agy --input-format stream-json --output-format stream-json [--model M] [extra_args…]` |
| Prompt | stdin, wrapped as one JSON event line: `{"event":"user","message":{"content":<prompt as a JSON string>}}` |
| Answer | JSON Lines on stdout; the last line that has `result.response` gives the answer; a truthy `result.error` fails the turn |
| Login | `council login agy` runs `agy` interactively: sign in with Google (on SSH it prints a URL to open), then type `/quit`. The session is stored in the OS keyring (Keychain, Secret Service over D-Bus, or Windows Credential Manager). For CI, export `GEMINI_API_KEY` and set `"modelProvider": "gemini"` in `~/.gemini/antigravity-cli/settings.json`. |
| Status | `agy models`. It costs no tokens and needs a session: signed in, it lists the models and exits 0 (`ok`, "signed in (N models available)"); signed out, it exits 1 with `Error: Please sign in to view available models…` (`missing`, with that message). In API-key mode it fails the same way when `GEMINI_API_KEY` is not set. |

The stream-json session mode reads the prompt from stdin, so it avoids argument
length limits. Each turn ends with an event such as
`{"event":"result","result":{"status":"…","response":"…"}}`.
To use the simpler print mode instead, see the
[override example](#overriding-a-built-in-harness).

How the status check was chosen (agy 1.3.3, October 2026): `agy --help` lists
no `auth`, `login`, `logout`, `status` or `whoami` subcommand, and the
[install and auth docs](https://antigravity.google/docs/cli/install/) say the
token lives in the OS keyring, with no file that shows a sign-in (the
`~/.gemini/antigravity-cli/` directory only holds settings, logs, caches and
conversations, signed in or not). `agy models` is the cheapest command that
needs the session: signed out it fails at once with the message above, and its
log says `You are not logged into Antigravity`. Two consequences: when the
keyring is locked (for example over SSH on macOS, or with no D-Bus session on
Linux), agy cannot read the token, so `doctor` most likely reports `missing`
even though you did sign in (unlock the keyring as the
[troubleshooting page](https://antigravity.google/docs/cli/troubleshooting/)
describes); and in
API-key mode agy only checks that `GEMINI_API_KEY` is set, not that it is
valid. `council doctor --probe agy` sends a real prompt when you need
certainty.

### opencode: OpenCode

| | |
|---|---|
| Kind / aliases | `opencode` / `open-code` |
| Command | `opencode run --format json --agent plan [--model provider/model] [extra_args…]` |
| Prompt | stdin (`opencode run` with no message arguments reads the message from stdin) |
| Answer | JSON Lines events on stdout; the last `{"type":"text","part":{"text":…}}` event is the answer; `{"type":"error","error":{"data":{"message":…}}}` fails the turn |
| Login | `council login opencode` → `opencode auth login` (pick a provider and paste a key or sign in). `-- --provider anthropic` skips the provider list. Keys are stored in `~/.local/share/opencode/auth.json`. |
| Status | `opencode auth list`, which lists stored credentials and provider environment variables (such as `ANTHROPIC_API_KEY`) and always exits 0. `doctor` reports `ok` with the counts. With none it still reports `ok`, because OpenCode's free OpenCode Zen models work without an account, and says so in the detail. |

`run` is OpenCode's non-interactive mode. `--format json` prints raw JSON
events (`step_start`, `text`, `tool_use`, `step_finish`, `error`) instead of
formatted text. When the agent uses tools, a turn has several steps; the text
of the last one is the final answer. `--agent plan` selects the built-in
read-only Plan agent, so OpenCode does not edit files or run shell commands
(drop it with an `args` override if you want the Build agent). `--model`
takes `provider/model`, as listed by `opencode models`, for example
`anthropic/claude-sonnet-4-5`; without it OpenCode uses its configured
default model. `extra_args: ["--variant", "high"]` sets the reasoning effort
on providers that support it.

Checked against opencode-ai 1.19.0 from npm and the
[CLI docs](https://opencode.ai/docs/cli/).

### cursor: Cursor CLI

| | |
|---|---|
| Kind / aliases | `cursor` / `cursor-agent`, `cursor-cli` |
| Command | `cursor-agent -p --output-format json --mode ask --trust [--model M] [extra_args…]` |
| Prompt | stdin (with no prompt argument and piped stdin, print mode reads all of stdin as the prompt) |
| Answer | one JSON object, `{"type":"result","subtype":"success","is_error":false,"result":…}`; `result` is the answer and a true `is_error` fails the turn. On failure the CLI exits non-zero with a message on stderr and no JSON. |
| Login | `council login cursor` → `cursor-agent login` (opens the browser; `NO_OPEN_BROWSER=1 council login cursor` prints the URL instead). For CI, set `CURSOR_API_KEY` (in your shell or the agent's `env:`). |
| Status | `cursor-agent models`. Signed out it exits 1 with "Authentication required. Run 'agent login', …"; with a bad key, "The provided API key is invalid." `doctor` shows `missing` with that message, `ok` when the command succeeds, and `unknown` for other failures (such as no network). |

The installer (`curl https://cursor.com/install -fsS | bash`) puts the same
binary on `PATH` as `cursor-agent` and `agent`; council uses `cursor-agent`
because `agent` is too generic a name. `-p`/`--print` is the non-interactive
mode. `--mode ask` is Cursor's read-only Q&A mode (`plan` is the other
read-only mode, for proposing plans); council never passes `--force`, without
which print mode only proposes changes anyway. In print mode an untrusted
workspace is refused ("Workspace Trust Required") unless `--trust` is given,
so the preset passes it; Cursor remembers that decision for the directory you
run council in. `--model` takes a name from `cursor-agent models`, such as
`gpt-5` or `sonnet-4`.

Why `models` and not `status`: `cursor-agent status` (alias `whoami`, with
`--format json`) exits 0 even when signed out (`{"status":"unauthenticated",
"isAuthenticated":false,…}`), and it only looks at the stored login, so it
says "Not logged in" even when `CURSOR_API_KEY` is set. `cursor-agent models`
needs a working session or key, costs no tokens, and fails clearly. The
status check runs with the agent's `env:`, so a key set there counts.

Checked against cursor-agent 2026.10.01-e373342 (installed with the official
script) and the [CLI docs](https://cursor.com/docs/cli/reference/parameters),
signed out and with an invalid key. The JSON shape of a successful run comes
from the [output format docs](https://cursor.com/docs/cli/reference/output-format).

### pi: Pi coding agent

| | |
|---|---|
| Kind / aliases | `pi` / `pi-coding-agent` |
| Command | `pi -p --no-session --tools read,grep,find,ls --no-mcp [--model M] [extra_args…]` |
| Prompt | stdin (piped stdin becomes the prompt; with a message argument it is prepended) |
| Answer | plain text: print mode writes only the final assistant text to stdout. A failed response exits non-zero with the error on stderr, for example "No API key found for the selected model." |
| Login | `council login pi` starts `pi` interactively: type `/login`, choose a provider (a subscription sign-in or an API key; saved in `~/.pi/agent/auth.json`), then `/quit`. Provider environment variables (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `GEMINI_API_KEY`, …; see `pi --help`) work without logging in. |
| Status | `pi --list-models`, which exits 0 either way: it prints a table of the models you can use, or "No models available. Use /login …". `doctor` shows `ok` with the number of models and their providers, or `missing`. |

This is Mario Zechner's pi coding agent, npm
[`@earendil-works/pi-coding-agent`](https://www.npmjs.com/package/@earendil-works/pi-coding-agent)
(formerly `@mariozechner/pi-coding-agent`, from the `badlogic/pi-mono`
repository, now `earendil-works/pi`); it needs Node.js 22.19 or later:

```sh
npm install -g --ignore-scripts @earendil-works/pi-coding-agent
```

`-p` runs once and exits. `--tools read,grep,find,ls` is the read-only tool
set from pi's own documentation (no `bash`, `edit` or `write`), and `--no-mcp`
also removes MCP tools, which `--tools` would otherwise keep. `--no-session`
keeps council's turns out of pi's session list. `--model` takes a pattern or
`provider/id`, optionally with a thinking level (`sonnet:high`);
`extra_args: ["--thinking", "high"]` works too. council uses print mode
rather than `--mode json` because JSON mode does not exit non-zero when the
model call fails.

`pi --list-models` only shows that a credential is present, not that it
works; `council doctor --probe pi` sends a real prompt. `pi auth check
--provider P` exits 0, 1 or 2 for ready, not ready or invalid, so if you use
one provider you can set `status_args: ["auth", "check", "--provider",
"anthropic"]` for a stricter check.

Checked against `@earendil-works/pi-coding-agent` 1.1.0 from npm (with
Node.js 22) and its bundled docs: `--help`, signed out, and a full run against
a local OpenAI-compatible test server configured in `~/.pi/agent/models.json`.

### Gemini CLI (removed)

The `gemini` harness (Gemini CLI) was removed in council v0.2.1 because Gemini
CLI is deprecated. A config that still says `harness: gemini` (or
`gemini-cli`) fails to load with a message saying so. Use Google's
Antigravity CLI instead (`harness: antigravity`, the `agy` agent), or drive
any CLI you like with `harness: generic`.

## Overriding a built-in harness

Any field you set on an agent with a built-in `harness:` replaces that field of
the preset; everything else is kept. Typical overrides:

```yaml
agents:
  codex:
    harness: codex
    command: /opt/homebrew/bin/codex     # a specific binary
    model: gpt-5-codex                   # -> --model gpt-5-codex
    timeout: 20m

  claude:
    harness: claude
    model: sonnet
    extra_args: ["--permission-mode", "plan"]
    workdir: ~/src/myproject             # let it read this project

  claude-opus:                           # a second Claude at the same table
    harness: claude
    model: opus

  agy:
    harness: antigravity
    env:
      GEMINI_API_KEY: $COUNCIL_GEMINI_KEY   # with "modelProvider": "gemini" in agy's settings.json

  opencode:
    harness: opencode
    model: anthropic/claude-sonnet-4-5   # -> --model anthropic/claude-sonnet-4-5

  cursor:
    harness: cursor
    model: gpt-5                         # -> --model gpt-5
    env:
      CURSOR_API_KEY: $MY_CURSOR_KEY     # instead of `council login cursor`

  pi:
    harness: pi
    model: anthropic/claude-sonnet-4-5:high

  # Antigravity in plain print mode instead of the stream-json session.
  agy-print:
    harness: antigravity
    args: ["-p", "{{prompt}}", "--output-format", "json", "{{model_args}}", "{{extra_args}}"]
    prompt_mode: arg
    output: json
    json_path: response
    error_path: error
```

Notes:

* `args` replaces the whole template. Keep `{{model_args}}` and
  `{{extra_args}}` in it if you still want `model` and `extra_args` to work.
* Empty strings do not override. For example `json_path: ""` keeps the preset's
  path. In the `agy-print` example the preset's `stdin_template` is still set,
  but it is ignored because the prompt mode is no longer `stdin`.
* `env` adds to council's own environment; it does not replace it.

## Adding a custom harness

Set `harness: generic` (or leave `harness` out for a name that is not a
built-in) and give at least a `command`. Then pick how the prompt goes in
(`prompt_mode`) and how the answer comes out (`output`, `output_source`,
`json_path`).

The command examples below are illustrations. Check your tool's own
documentation for its real flags.

### Prompt as an argument

`prompt_mode: arg` is the default for generic harnesses. The prompt replaces
`{{prompt}}`, or is appended as the last argument if no argument contains
`{{prompt}}`.

```yaml
agents:
  # ollama run llama3.1 "<prompt>"   (prompt appended as the last argument)
  llama:
    harness: generic
    command: ollama
    args: ["run", "llama3.1"]
    prompt_mode: arg
    output: text
    timeout: 5m

  # Explicit placeholder, here in the middle of the command line:
  my-cli:
    harness: generic
    command: my-cli
    args: ["ask", "--question", "{{prompt}}", "--plain"]
    prompt_mode: arg

  # The placeholder can be part of a larger argument:
  my-other-cli:
    harness: generic
    command: my-other-cli
    args: ["--prompt={{prompt}}"]
```

Use `arg` only for short discussions. From round 2 on the prompt contains the
whole transcript, and operating systems limit argument size: about 128 KiB per
argument on Linux, and about 32 K characters for the whole command line on
Windows. Prefer `stdin` or `file`.

### Prompt on stdin

```yaml
agents:
  # echo "<prompt>" | ollama run llama3.1
  llama:
    harness: generic
    command: ollama
    args: ["run", "llama3.1"]
    prompt_mode: stdin
    output: text
```

`stdin_template` wraps the prompt before it is written to stdin. `{{prompt}}`
is the raw prompt; `{{prompt_json}}` is the prompt as a quoted, escaped JSON
string, which is what you need to build a JSON request body:

```yaml
agents:
  # POST the prompt to a local Ollama server.
  llama-http:
    harness: generic
    command: curl
    args: ["-sS", "http://localhost:11434/api/generate", "-H", "Content-Type: application/json", "-d", "@-"]
    prompt_mode: stdin
    stdin_template: '{"model": "llama3.1", "prompt": {{prompt_json}}, "stream": false}'
    output: json
    json_path: response
    error_path: error
    timeout: 5m
```

### Prompt in a file

With `prompt_mode: file` the prompt is written to a temp file (`prompt.md`,
readable only by you). Its path replaces `{{prompt_file}}`, or is appended as
the last argument if no argument contains `{{prompt_file}}`.

```yaml
agents:
  # my-llm --input /tmp/council-file-agent-123/prompt.md --format json
  file-agent:
    harness: generic
    command: my-llm
    args: ["--input", "{{prompt_file}}", "--format", "json"]
    prompt_mode: file
    output: json
    json_path: choices.0.message.content

  # A tool that only takes the file as its last argument:
  summarize:
    harness: generic
    command: ~/bin/summarize        # ~ and $VARS are expanded in command
    prompt_mode: file               # -> ~/bin/summarize /tmp/…/prompt.md
```

`{{prompt_file}}` is also substituted inside larger arguments, for example
`"--input=@{{prompt_file}}"`.

### JSON output and JSON paths

With `output: json`, all of stdout must be a single JSON document (whitespace
around it is fine). Then:

1. If `error_path` is set and the value there is truthy, the turn fails with
   `harness reported error: <value>`. Truthy means `true`, a non-empty string,
   a non-zero number, or a non-empty object or array. If the value is just
   `true` (like Claude's `is_error`), the text at `json_path` is used as the
   error message.
2. The value at `json_path` is the answer. Strings are used as they are; other
   values (numbers, objects) are JSON-encoded. An empty answer, or a path that
   does not exist, fails the turn.

A JSON path is a dot-separated list of object keys and array indexes:

| Document | `json_path` | Result |
|---|---|---|
| `{"result": "hi"}` | `result` | `hi` |
| `{"result": {"response": "hi"}}` | `result.response` | `hi` |
| `{"choices": [{"message": {"content": "hi"}}]}` | `choices.0.message.content` | `hi` |
| `{"items": [{"t": "a"}, {"t": "b"}]}` | `items.-1.t` | `b` (negative indexes count from the end) |
| `{"answer": 42}` | `answer` | `42` |
| `"hi"` | *(empty)* | `hi` (empty path = whole document) |

A leading dot (`.result`) is allowed. Keys that contain a dot cannot be
addressed.

Example: an OpenAI-compatible HTTP API through `curl`. Environment variables
are not expanded in `args`, so a small `sh -c` wrapper lets the shell insert
the API key:

```yaml
agents:
  gpt-api:
    harness: generic
    command: sh
    args:
      - -c
      - 'curl -sS https://api.openai.com/v1/chat/completions -H "Authorization: Bearer $OPENAI_API_KEY" -H "Content-Type: application/json" -d @-'
    prompt_mode: stdin
    stdin_template: '{"model": "gpt-4o-mini", "messages": [{"role": "user", "content": {{prompt_json}}}]}'
    output: json
    json_path: choices.0.message.content
    error_path: error
    env:
      OPENAI_API_KEY: $MY_OPENAI_KEY   # optional: map from another variable
```

### JSON Lines output

`output: jsonl` is for CLIs that stream newline-delimited JSON events. council
reads stdout **from the last line backwards**:

* lines that are empty, do not start with `{`, or are not valid JSON are
  skipped (so log lines mixed into stdout are harmless);
* on each JSON line, a truthy `error_path` fails the turn immediately;
* the first line (from the end) where `json_path` exists gives the answer.

```yaml
agents:
  # Prints e.g.
  #   {"type":"progress","text":"thinking…"}
  #   {"type":"final","data":{"text":"The answer is 42."}}
  streamer:
    harness: generic
    command: my-agent
    args: ["run", "--events", "--json"]
    prompt_mode: stdin
    output: jsonl
    json_path: data.text
    error_path: data.error
```

The built-in `agy` adapter is a complete example: it uses `stdin_template` to
send one JSON event and `output: jsonl` with `json_path: result.response`.
The `opencode` adapter reads `part.text` from the last `text` event.

### Answer written to a file

Some CLIs print progress on stdout and write the final answer to a file. Put
`{{output_file}}` in `args` and set `output_source: file`. council then parses
that file (as `text`, `json` or `jsonl`) instead of stdout. The built-in `codex`
adapter works this way.

```yaml
agents:
  writer:
    harness: generic
    command: my-agent
    args: ["--quiet", "--out", "{{output_file}}"]
    prompt_mode: stdin
    output_source: file
    output: text
```

If the process succeeds but the file was not written, the turn fails.

### Login and status commands

`login_args` and `status_args` let `council login` and `council doctor` work
with your harness. Both are appended to `command`:

```yaml
agents:
  my-api:
    harness: generic
    command: my-llm
    prompt_mode: stdin
    login_args: ["auth", "login"]   # council login my-api [-- extra] -> my-llm auth login [extra]
    status_args: ["auth", "status"] # council doctor: exit 0 = ok, otherwise missing
```

* Without `login_args`, `council login <agent>` fails with a hint.
  `login_args: []` (an empty list) runs the bare command, which is useful for
  CLIs that log in from their interactive UI.
* Without `status_args`, `doctor` reports auth `unknown`. `doctor --probe`
  then decides.
* `doctor` always runs `<command> --version` and shows the first line, if the
  command succeeds.
* The status command runs with the agent's `env:` and `workdir`, like a turn,
  so an API key set under `env:` counts.

### Checklist

1. Run the CLI by hand in its headless mode and note how it takes the prompt
   and where the answer appears.
2. Add the agent to your config, then run `council agents` to check the
   resolved command line.
3. `council doctor --probe <agent>` sends a one-line prompt end to end.
4. `council ask --agents <agent>,claude --verbose "What is 2+2?"` shows whether
   its replies include a parsable vote block. Weak models sometimes forget it,
   which counts as an abstention.

To turn a harness into a built-in preset in Go, see
[CONTRIBUTING.md](../CONTRIBUTING.md#adding-a-built-in-harness).

## Reference

### Placeholders

| Placeholder | Kind | Replaced with |
|---|---|---|
| `{{prompt}}` | substring | The full prompt. In `arg` mode, appended as the last argument if absent. |
| `{{prompt_file}}` | substring | Path of the temp prompt file (only written in `file` mode). In `file` mode, appended as the last argument if absent. |
| `{{output_file}}` | substring | Path of the temp output file. Required in `args` when `output_source: file`. |
| `{{model}}` | substring | `model` (empty if unset). |
| `{{workdir}}` | substring | `workdir` after `~` expansion (empty if unset). |
| `{{model_args}}` | whole argument | `model_flag` and `model` as two arguments if both are set, otherwise nothing. |
| `{{extra_args}}` | whole argument | The `extra_args` list. If no argument is `{{extra_args}}`, the list is appended after the template instead. |

Whole-argument placeholders must be the entire argument (surrounding spaces
are ignored). Order of the final command line: the expanded template, then
`extra_args` if there was no `{{extra_args}}`, then the prompt or prompt file
if it is appended.

In `stdin_template`: `{{prompt}}` (raw) and `{{prompt_json}}` (JSON string
literal, quotes included).

### Prompt modes

| `prompt_mode` | Delivery | Stdin |
|---|---|---|
| `arg` (default for generic) | `{{prompt}}` or last argument | empty |
| `stdin` | written to stdin, through `stdin_template` if set | the prompt |
| `file` | `{{prompt_file}}` or last argument | empty |

### Output parsing

| `output` | Parsing | Failure when |
|---|---|---|
| `text` (default) | Trimmed stdout or output file. | It is empty. |
| `json` | One JSON document; `error_path` checked, then `json_path`. | Invalid JSON, truthy `error_path`, missing path, or empty answer. |
| `jsonl` | Lines from the end; non-JSON lines skipped; first line with `json_path` wins. | Truthy `error_path` on a scanned line, or no line with `json_path`. |

A non-zero exit status always fails the turn, even if an answer could be
parsed. The error message includes the last 800 characters of stderr (or of
stdout if stderr is empty).
