// Package chat runs the group chat: a shared [Transcript], round-based
// prompting of every agent concurrently, consensus detection and the final
// synthesis step.
//
// [New] builds a [Council] from a list of [harness.Harness] values and
// [Options]; [Council.Run] drives the discussion:
//
//  1. Round 1: every agent answers the request independently.
//  2. Round N > 1: every agent sees the full discussion so far, critiques
//     the others and revises. The last round is announced as final.
//  3. After each round the votes are checked with [consensus.Agreement];
//     the loop stops early on consensus.
//  4. Without consensus the latest vote of every agent is tallied with
//     [consensus.Majority].
//  5. If a moderator is configured it always runs last, even after
//     consensus: it reads the whole discussion and the vote tally and
//     writes the final answer. If it fails, the vote result stands.
//
// All agents of a round run in parallel, one goroutine each. A failed or
// timed-out agent is recorded in the transcript and does not block the
// others; the run fails only if every agent fails in the same round.
// [Options.OnEvent] receives serialized progress events for live output.
package chat
