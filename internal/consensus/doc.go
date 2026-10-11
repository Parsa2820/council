// Package consensus parses the structured votes agents attach to their
// messages and decides whether, and on what, the group agrees.
//
// Every agent message ends with a fenced JSON block such as
//
//	```json
//	{"final_answer": "42", "agree": true, "agrees_with": ["claude"], "confidence": 0.9}
//	```
//
// [ParseVote] extracts the last such block (falling back to the last bare
// JSON object that mentions an answer, and accepting the "answer" and
// "agreement" aliases). [Normalize] canonicalises answers for comparison.
// [Agreement] decides whether one round's ballots are a consensus, and
// [Majority] tallies ballots when they are not, breaking ties by summed
// confidence and then ballot order.
package consensus
