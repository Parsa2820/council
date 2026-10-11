// Package harness adapts LLM coding CLIs ("harnesses") such as Codex CLI,
// Claude Code, Antigravity CLI and OpenCode to a single interface: send a
// prompt non-interactively, get the agent's final text back.
//
// Every adapter, built-in or user-defined, is a [CommandHarness] configured
// by a [Spec]. Built-in harnesses are just Spec presets (see [Preset]) that
// encode each CLI's documented headless flags. A Spec describes:
//
//   - the command and its argument template, with placeholders such as
//     {{prompt}}, {{prompt_file}}, {{output_file}}, {{model_args}} and
//     {{extra_args}} (see [CommandHarness.BuildArgs]);
//   - how the prompt is delivered: as an argument, on stdin (optionally
//     wrapped by a template such as a JSON event line), or in a temp file;
//   - how the answer is read: text, a single JSON document, or a JSONL
//     stream, from stdout or from an output file, with a dot path
//     ([Lookup]) selecting the answer and another flagging errors;
//   - the per-run timeout, extra environment, working directory, and the
//     login and status commands used by "council login" and "council doctor".
//
// Each [CommandHarness.Run] call is a fresh, stateless subprocess in its own
// temporary directory. On Unix the child runs in its own process group so a
// timeout kills the whole process tree.
//
// [Func] adapts a plain Go function to [Harness]; it is used for in-process
// fakes in tests and for embedding the chat engine as a library.
package harness
