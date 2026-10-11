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
| `make vet` | `go vet ./...` |
| `make fmt` | `gofmt -w .` |
| `make demo` | Build, then `council ask --config examples/fake-council.yaml --verbose "What is 6 * 7?"` |
| `make dist` | `scripts/build.sh`: static builds for darwin/amd64, darwin/arm64, linux/amd64, linux/arm64 and windows/amd64 in `dist/`, plus `SHA256SUMS` |
| `make zip` | `make dist`, then zip `dist/` into `../council-binaries.zip` |
| `make source-zip` | `git archive` of `HEAD` into `../council-source.zip` |
| `make release` | `vet`, `test`, `zip`, `source-zip` |
| `make clean` | Remove `dist/` and `./council` |

`scripts/build.sh` also takes `TARGETS="linux/amd64 darwin/arm64"` and
`OUT=somedir` environment variables. `dist/`, the `council` binary and the zip
files are ignored by git. Never commit build output.

## Tests

```sh
go test ./...            # everything (about a second)
go test -race ./...      # with the race detector (needs cgo)
go test ./internal/chat -run Moderator -v
```

| Package | What the tests cover | How |
|---|---|---|
| `internal/consensus` | vote parsing (fenced, bare JSON, aliases, last block wins, missing vote, confidence clamping), `StripVote`, normalization, agreement, majority and tie-breaks | pure functions |
| `internal/harness` | JSON paths, argument templating, preset command lines, spec validation, every prompt mode and output format, error paths, non-zero exits, timeouts that kill the process, env, doctor checks, login commands | small generated shell scripts that mimic each CLI's output |
| `internal/config` | the default config, built-in names without definitions, overrides and generic agents, validation errors, `init`, path resolution, empty files | YAML snippets |
| `internal/chat` | convergence, early stop in round 1, prompts that include earlier rounds, majority fallback, explicit agreement, moderator synthesis and fallback, failing agents, abstentions, concurrency, turn timeouts | in-process fakes (`harness.Func`) |
| `internal/cli` | `ask` end to end (consensus, `--json`, stdin, `--moderator`, `--file`, a broken agent, usage errors), `doctor`, `init`, `agents`, `login`, flag parsing, `version` | `examples/fake-agent.sh` through the generic harness |

Tests that need `/bin/sh` call `t.Skip` on Windows, so `go test ./...` passes
there but covers less. CI runs the suite on Linux, macOS and Windows.

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
   * add a `Run` test with a shell-script fake that prints the CLI's real
     output format, including an error case.
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
