# Contributing to council

Thanks for helping! This guide covers the development setup, the tests and
the fake agent, how to add a built-in harness, and the commit and release
conventions.

## Development setup

You need:

* **Go 1.24 or newer** (`go.mod` says `go 1.24`; newer toolchains work).
* **make** and a **POSIX shell** (`/bin/sh`). The end-to-end tests and the demo
  run `examples/fake-agent.sh`.
* `zip` and `sha256sum`, only for `make zip` and `make dist`.

No LLM CLI or API key is needed for development or tests.

```sh
git clone git@github.com:Parsa2820/council.git
cd council
make            # go vet ./... && go test ./... && build ./council
./council version
make demo       # run a consensus round against the fake agents
```

The module path is `github.com/Parsa2820/council`. All packages are under
`internal/`. [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) explains how they
fit together.

### Make targets

| Target | What it does |
|---|---|
| `make` / `make all` | `vet`, `test`, `build` |
| `make build` | Build `./council` for this machine, with the version stamped from `git describe` (override with `VERSION=…`) |
| `make test` | `go test ./...` |
| `make test-race` | `go test -race ./...` (needs cgo) |
| `make cover` | `go test -coverprofile=cover.out ./...`, then `go tool cover -func` (per-function figures and the total); add `COVERFLAGS=-race` for the race detector |
| `make cover-html` | `make cover`, then open the annotated HTML report |
| `make vet` | `go vet ./...` |
| `make fmt` | `gofmt -w .` |
| `make demo` | Build, then `council ask --config examples/fake-council.yaml --verbose "What is 6 * 7?"` |
| `make dist` | `scripts/build.sh`: static builds for darwin/amd64, darwin/arm64, linux/amd64, linux/arm64 and windows/amd64 in `dist/`, plus `SHA256SUMS` |
| `make zip` | `make dist`, then zip `dist/` into `../council-binaries.zip` |
| `make source-zip` | `git archive` of `HEAD` into `../council-source.zip` |
| `make release` | `vet`, `test`, `zip`, `source-zip` |
| `make clean` | Remove `dist/`, `./council` and `cover.out` |

`scripts/build.sh` also takes `TARGETS="linux/amd64 darwin/arm64"` and
`OUT=somedir` environment variables. `dist/`, the `council` binary and the zip
files are ignored by git. Never commit build output.

## Tests

```sh
go test ./...                    # everything (a few seconds)
go test -race ./...              # with the race detector (needs cgo)
go test -count=1 ./...           # bypass the test cache, e.g. to check for flakiness
go test ./internal/chat -run Moderator -v
make cover                       # coverage per package, per function and total
make cover-html                  # the same, annotated line by line in the browser
```

Coverage on Linux and macOS (`make cover`, October 2026):

| Package | Statements covered |
|---|---|
| `cmd/council` | 100.0% |
| `internal/chat` | 100.0% |
| `internal/cli` | 99.7% |
| `internal/config` | 99.3% |
| `internal/consensus` | 100.0% |
| `internal/harness` | 99.6% |
| **total** | **99.7%** |

What is left uncovered is defensive code that cannot fail in practice: the
panic in `config.Default` if the embedded `DefaultYAML` were invalid, the
error from writing the prompt into a temp dir that `Run` has just created,
and the `cfg.Harness` error in `council agents` for a config that already
passed validation. CI writes the same table for each OS to the job summary
and fails on Linux and macOS if the total drops below 90%.

| Package | What the tests cover | How |
|---|---|---|
| `cmd/council` | `run` (exit codes, streams), `isTerminal` (pipe, file, character device), `main` | in-process, with `exit` replaced |
| `internal/consensus` | vote parsing (every fence spelling, bare JSON, aliases, broken blocks, last block wins, missing vote, confidence clamping), `StripVote`, the brace scanner, normalization, agreement, quorum, majority and tie-breaks | table-driven pure functions |
| `internal/harness` | JSON paths, argument templating, `stdin_template`, spec validation and defaults, every prompt mode and output format, `error_path`, null and empty answers, non-zero exits, stderr tails, cancellation, timeouts that kill the whole process tree, env, workdir, doctor checks, the gemini and agy credential probes, login commands | the fake CLIs below, plus small generated shell scripts |
| `internal/config` | durations, path resolution (`$COUNCIL_CONFIG`, `$XDG_CONFIG_HOME`, `config.yml`, `config`), load and parse errors, unknown fields, validation, preset overrides, `~` and `$VAR` expansion, `init` with and without `--force` | YAML snippets |
| `internal/chat` | convergence, early stop in round 1, prompts and the moderator prompt, transcript rendering (`Discussion`, `Markdown`), event order, majority fallback, quorum, explicit agreement, moderator synthesis, fallback and replies without a vote, failing agents, abstentions, concurrency, turn timeouts, cancellation | in-process fakes (`harness.Func`) |
| `internal/cli` | every command and flag of `ask`, `init`, `doctor`/`status` (table, `--json`, `--probe`), `login` (extra args, failures), `agents`, `version`, `help`, exit codes, prompt sources, the live printer and result line | `examples/fake-agent.sh` through the generic harness, and the fake CLIs below |

Tests that need `/bin/sh` call `t.Skip` on Windows, so `go test ./...` passes
there but covers less. CI runs the suite on Linux, macOS and Windows.

### The fake harness CLIs

`internal/harness/testdata/fakecli/` holds shell-script stand-ins for `codex`,
`claude`, `gemini` and `agy`. Tests copy them into a temp dir at the front of
`PATH`, so the built-in presets run unchanged. Each fake rejects arguments the
real CLI would not accept in our invocation, reads the prompt the way the real
one does, and prints the real output format:

| Fake | Contract it checks | Output |
|---|---|---|
| `codex` | `exec … --output-last-message FILE … -`, prompt on stdin | final message in `FILE`, progress noise on stdout |
| `claude` | `-p --output-format json --no-session-persistence`, prompt on stdin | `{"type":"result","is_error":…,"result":…}` |
| `gemini` | `--output-format json … -p TEXT`, prompt on stdin | `{"response":…,"stats":…}` or `{"error":{…}}` |
| `agy` | `--input-format stream-json --output-format stream-json`, one `{"event":"user",…}` line on stdin | NDJSON events ending in `{"event":"result","result":{…}}` |

All four answer `--version`; `codex login [status]` and `claude auth
login|status` behave like the real commands. Environment variables select the
behaviour: `FAKE_MODE` (`ok`, or a failure such as `fail`, `malformed`,
`hang`; see each script), `FAKE_AUTH=ok|missing`, `FAKE_LOGIN_EXIT`, and
`FAKE_ARGS`/`FAKE_STDIN` (files that record the argv and stdin they received).
When a real CLI changes its flags or output, update its fake together with the
preset.

Guidelines:

* Tests must not need network access, an LLM CLI or credentials.
* Prefer `harness.Func` fakes for orchestration logic and small shell scripts
  for subprocess behaviour.
* Keep tests fast. Use timeouts in milliseconds, not seconds.
* Bug fixes come with a test that fails without the fix.

### The fake agent

`examples/fake-agent.sh` is a deterministic stand-in for an LLM CLI. It reads
the council prompt on stdin and replies with a message ending in a vote block:

```
fake-agent.sh NAME INITIAL_ANSWER [follow|stubborn] [DELAY_SECONDS]
```

| Situation | Behaviour |
|---|---|
| Round 1 (prompt contains `ROUND 1 of`) | Answers `INITIAL_ANSWER`, `agree: false`, confidence 0.6. |
| Later round, `follow` (default) | Adopts the most common `final_answer` quoted in the prompt (ties: alphabetical) with `agree: true`, confidence 0.8. |
| Later round, `stubborn` | Keeps `INITIAL_ANSWER`; `agree: true` only if it is the majority, confidence 0.9. |
| Moderator prompt (contains `the MODERATOR`) | Picks the majority answer with `agree: true`, confidence 0.95. |
| `--version` / `--status-probe` as first argument | Prints a version or `Logged in (fake)` and exits 0 (for `doctor` tests). |

`DELAY_SECONDS` makes it sleep before answering, to exercise concurrency and
timeouts. Wire it up with the generic harness, as `examples/fake-council.yaml`
does:

```yaml
agents:
  alpha:
    harness: generic
    command: ./examples/fake-agent.sh
    args: ["alpha", "42", "follow"]
    prompt_mode: stdin
```

Try new scenarios by hand:

```sh
council ask --config examples/fake-council.yaml --rounds 1 --verbose "q"        # majority
council ask --config examples/fake-council.yaml --moderator judge "q"           # moderator
council ask --config examples/fake-council.yaml --json "q" | jq .method
```

## Adding a built-in harness

Most CLIs do not need code: a [generic harness](docs/HARNESSES.md#adding-a-custom-harness)
in the config is enough. Add a built-in preset when a CLI is popular enough
that every user would otherwise copy the same config.

1. **Work out the headless invocation.** From the CLI's documentation, find:
   the non-interactive mode, how to pass the prompt (prefer stdin or a file
   over an argument: transcripts get long), a machine-readable output format
   and the JSON path of the final answer and of errors, the model flag, the
   login command, and a status command if there is one. Write down which
   documentation version you used.
2. **Add the kind** in `internal/harness/presets.go`:
   * a `Kind…` constant;
   * any aliases in `kindAliases`;
   * the kind in `BuiltinKinds()`;
   * a `case` in `Preset` returning the `Spec`: `Command`, `Args` with
     `{{model_args}}` and `{{extra_args}}`, `ModelFlag`, `PromptMode`,
     `StdinTemplate` if needed, `Output`, `OutputSource`, `JSONPath`,
     `ErrorPath`, `Timeout` (10 minutes like the others), `LoginArgs`,
     `LoginNote` (one or two sentences shown by `council login`), and either
     `StatusArgs` or a `statusFunc`;
   * extend the documentation comment above `Preset` with the flags and why.
3. **Status without a status command:** write a `func(context.Context)
   (AuthState, string)` like `geminiAuth`. Only check for the *presence* of
   documented credential sources (environment variables, a credentials file
   path). Never read or print credential contents. Return `AuthUnknown` when
   you cannot tell; `doctor --probe` covers that case.
4. **Tests** in `internal/harness/harness_test.go`:
   * add the expected command line to `TestPresetsBuildDocumentedFlags`;
   * add a fake CLI to `internal/harness/testdata/fakecli/` that checks the
     argument and stdin contract and prints the real output format, and add
     the kind to the tables in `presets_test.go` (`TestPresetRunContracts`,
     `TestPresetRunFailures`, `TestPresetCheck`, `TestPresetLoginCommands`)
     and to `fakePresetCLIs` in `internal/cli/commands_test.go`.
5. **Config:** add a commented entry to `DefaultYAML` in
   `internal/config/default.go`. Add it to `defaults.agents` only if most
   users will have it. Update `TestDefaultConfig` if needed.
6. **Docs:** a section in [docs/HARNESSES.md](docs/HARNESSES.md), rows in the
   README tables (built-in harnesses, signing in), the `Spec` field reference
   if you added a field, and a `CHANGELOG.md` entry.
7. Check by hand: `council agents`, `council doctor --probe <kind>`, and a
   real `council ask --agents <kind>,claude -v "What is 2+2?"` if you have the
   CLI.

If the CLI needs behaviour that `Spec` cannot express (a new prompt mode or
output format), add it to `Spec` and `CommandHarness` generally, with tests,
so generic harnesses can use it too. Do not special-case one CLI in `Run`.

## Code style

* `gofmt` (run `make fmt`) and `go vet` must be clean.
* Every package has a `doc.go` with its package comment. Exported identifiers
  have doc comments that start with their name.
* Errors are lower-case, without trailing punctuation, and wrapped with
  `%w` when the caller may inspect them. Harness errors start with the agent
  name.
* Keep the dependency list short: currently only `gopkg.in/yaml.v3`.
* User-facing output: results on stdout, everything else (status, notes,
  progress, errors) on stderr.
* council must never store, read or print credentials.

## Commit style

* One logical change per commit. Formatting, refactors and behaviour
  changes go in separate commits.
* Subject line: imperative mood, capitalized, no trailing period, about 72
  characters at most, e.g. `Add Ollama preset` or `Fix JSONL parsing of
  trailing log lines`.
* Optional body after a blank line, wrapped at about 72 characters,
  explaining **why** and anything surprising, not repeating the diff.
* `go build ./... && go vet ./... && go test ./...` passes at every commit.
* Update docs and `CHANGELOG.md` (under `Unreleased`) in the same commit as
  the behaviour change.

## Releasing

1. Move the `Unreleased` entries in `CHANGELOG.md` under a new version
   heading with the date, and commit.
2. Tag: `git tag -a v0.2.0 -m "council v0.2.0"`.
3. `make release` builds `dist/` and `../council-binaries.zip` (the version
   comes from the tag) and `../council-source.zip`.
4. Pushing the tag (`git push origin v0.2.0`) runs
   `.github/workflows/release.yml`, which builds the same binaries and
   attaches them, with `SHA256SUMS`, to a GitHub release.
