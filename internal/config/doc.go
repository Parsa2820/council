// Package config loads council's YAML configuration, which defines the
// default settings for "council ask" and the agents taking part in the group
// chat, including custom (generic) harnesses.
//
// The file is located by [DefaultPath]: $COUNCIL_CONFIG, else
// $XDG_CONFIG_HOME/council/config.yaml, else ~/.config/council/config.yaml
// (config.yml and an extension-less config are also honoured). A missing file
// is not an error; [Load] then returns the built-in [Default], which is the
// same document "council init" writes ([DefaultYAML]).
//
// Unknown keys are rejected. Each agent entry selects a harness kind (codex,
// claude, antigravity/agy, opencode or generic); every other field overrides
// the built-in preset for that kind. [Config.Spec] resolves an agent name to
// a [harness.Spec], and [Config.Harness] builds the runnable harness. Names
// of built-in kinds (codex, claude, antigravity, agy, opencode) resolve to the
// preset even when they are not listed under agents. The removed gemini
// harness (Gemini CLI) is reported with a hint to use antigravity or generic.
package config
