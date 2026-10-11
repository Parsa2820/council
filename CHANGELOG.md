# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `council serve`: a local web UI. Type a prompt, pick agents (with the
  same installed and signed-in status as `council doctor`), set rounds,
  moderator and timeout, and watch the group chat stream in round by round
  with each agent's vote, then a consensus card with the answer, method,
  supporters and dissenters. Runs can be cancelled and their transcript
  downloaded as Markdown. The page follows the system's light or dark theme
  and is embedded in the binary (plain HTML, CSS and JavaScript, standard
  library only). Flags: `--addr` (default `127.0.0.1:8787`), `--open`,
  `--config`, `--max-runs`. See [docs/WEB_UI.md](docs/WEB_UI.md).
- An HTTP API behind the UI: `GET /api/agents`, `POST /api/run` (a
  Server-Sent Events stream of chat events, the result and the transcript)
  and `POST /api/runs/{id}/cancel`.
- `examples/fake-council.yaml` gives the fake agents a status command, so
  `council doctor` and the web UI show them as signed in.

### Security

- The web server listens on loopback by default and warns when bound to
  another address. It rejects non-loopback `Host` headers (DNS rebinding),
  cross-site and cross-origin `POST`s, non-JSON bodies and requests without
  the per-process token, and sends a strict Content-Security-Policy.
  Closing the browser tab cancels the run and kills the agents' processes.

## [0.1.1] - 2026-10-10

### Added

- Unit tests for every package: 319 tests and subtests (up from 55), taking
  statement coverage from 83.7% to 99.7% on Linux and macOS (83.5% on
  Windows, which skips the shell-script tests). They include fake `codex`,
  `claude`, `gemini` and `agy` CLIs that check each preset's argument, stdin
  and output contract, auth status and login commands, malformed output,
  non-zero exits and timeouts that kill the whole process tree.
- `make cover`, `make cover-html` and `make test-race`.
- CI writes per-package and per-function coverage to the job summary on
  Linux, macOS and Windows, and fails below 90% on Linux and macOS.
- The release workflow can be run by hand for an existing tag
  (`gh workflow run release.yml -f tag=vX.Y.Z`), and re-running it replaces
  the release's assets.

### Changed

- `cmd/council`'s `main` is a thin wrapper around a testable `run` function.

### Fixed

- `council ask` no longer exits 0 with an empty answer when no agent produced
  a vote block and the moderator failed; it now reports an error.
- Truncated text (`max_transcript_chars`, error excerpts, `doctor` details)
  no longer cuts a multi-byte UTF-8 character in half.
- Answer normalization strips quotes and trailing punctuation in any order,
  so `"Paris".` now matches `Paris`.
- A JSON harness whose answer field is `null` (for example Gemini CLI's
  `"response": null`) is reported as "no answer" instead of answering the
  literal text `null`.

## [0.1.0] - 2026-10-10

First release.

### Added

- `council ask`: runs several LLM coding CLIs as a group chat and prints their
  consensus answer.
  - Round 1 independent answers, then critique-and-revise rounds over a shared
    transcript; all agents of a round run concurrently.
  - Structured JSON votes (`final_answer`, `agree`, `agrees_with`,
    `confidence`), parsed from the last fenced block, with a bare-JSON
    fallback and `answer`/`agreement` aliases.
  - Consensus detection with answer normalization, a quorum of
    `min(2, #agents)`, and explicit agreement from round 2 on (`--loose` to
    relax).
  - Majority vote over each agent's latest vote, with tie-breaks by summed
    confidence and then agent order.
  - Optional moderator synthesis (`--moderator`) with fallback to the vote
    when the moderator fails.
  - Prompt from an argument, `--file`/`-f`, stdin, or a combination (`-`).
  - `--agents`, `--rounds`, `--timeout`, `--max-context`, `--json`,
    `--transcript` (Markdown), `--verbose`/`-v` live output, `--config`.
- Built-in harnesses: OpenAI Codex CLI (`codex exec`), Claude Code
  (`claude -p`), Gemini CLI (`gemini -p`) and Google Antigravity CLI (`agy`,
  stream-json mode).
- Generic harness for any CLI: argument template with placeholders; prompt as
  an argument, on stdin (with an optional template and `{{prompt_json}}`) or
  in a file; text, JSON or JSON Lines output from stdout or an output file;
  JSON paths for the answer and for errors; per-agent timeout, environment
  and working directory.
- `council init`, `council doctor` (alias `status`, with `--probe` and
  `--json`), `council login <agent> [-- args]`, `council agents`,
  `council version`.
- YAML configuration at `~/.config/council/config.yaml` (or
  `$XDG_CONFIG_HOME`, `$COUNCIL_CONFIG`, `--config`) with strict key checking.
- Timeouts and Ctrl-C kill the harness's whole process group on Unix.
- Deterministic fake agent (`examples/fake-agent.sh`) and demo config.
- Cross-compiled static binaries for macOS (amd64, arm64), Linux (amd64,
  arm64) and Windows (amd64) via `make dist`.
- Documentation: README, `docs/ARCHITECTURE.md`, `docs/HARNESSES.md`,
  `docs/CONFIGURATION.md`, `docs/CONSENSUS.md`, `CONTRIBUTING.md`.
- CI on Linux, macOS and Windows, and a tag-triggered release workflow.

[Unreleased]: https://github.com/Parsa2820/council/compare/v0.1.1...HEAD
[0.1.1]: https://github.com/Parsa2820/council/releases/tag/v0.1.1
[0.1.0]: https://github.com/Parsa2820/council/releases/tag/v0.1.0
