# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Unit tests for every package (99.7% statement coverage on Linux and
  macOS), including fake `codex`, `claude`, `gemini` and `agy` CLIs that check
  each preset's argument, stdin and output contract.
- `make cover`, `make cover-html` and `make test-race`; CI reports coverage
  per package in the job summary and fails below 90%.

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

[Unreleased]: https://github.com/Parsa2820/council/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/Parsa2820/council/releases/tag/v0.1.0
