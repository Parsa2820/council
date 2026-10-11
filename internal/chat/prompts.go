package chat

import (
	"fmt"
	"strings"
)

const voteFormat = "```json\n" +
	`{"final_answer": "<your final answer, as concise as possible but complete>", "agree": <true|false>, "agrees_with": ["<agent names whose answer you endorse>"], "confidence": <0.0-1.0>}` +
	"\n```"

func header(self string, agents []string, round, maxRounds int) string {
	var others []string
	for _, a := range agents {
		if a != self {
			others = append(others, a)
		}
	}
	who := "You are working alone."
	if len(others) > 0 {
		who = fmt.Sprintf("The other participants are: %s.", strings.Join(others, ", "))
	}
	final := ""
	if round == maxRounds && round > 1 {
		final = " This is the FINAL round: commit to the answer you believe is best."
	}
	return fmt.Sprintf(`You are %q, one of %d AI agents in a group chat that must reach a consensus answer to the user's request. %s

This is ROUND %d of at most %d.%s
Answer in text only: do not modify files or run commands that change anything.`,
		self, len(agents), who, round, maxRounds, final)
}

// RoundPrompt builds the prompt for agent self in the given round.
func RoundPrompt(question, self string, agents []string, round, maxRounds int, discussion string) string {
	var sb strings.Builder
	sb.WriteString(header(self, agents, round, maxRounds))
	sb.WriteString("\n\n## User request\n\n")
	sb.WriteString(strings.TrimSpace(question))
	sb.WriteString("\n\n")
	if round == 1 || discussion == "" {
		sb.WriteString(`## Instructions

Answer independently; in the next round you will see the other agents' answers and can revise.
Give your answer with brief reasoning.`)
	} else {
		sb.WriteString("## Discussion so far\n\n")
		sb.WriteString(discussion)
		sb.WriteString(`

## Instructions

Critically review the other agents' latest answers: point out mistakes, acknowledge correct points, and revise your own answer if you are persuaded. Aim to converge on the single best answer, but do not agree just to be agreeable.
Set "agree": true only if your final_answer is the same answer the others are now proposing and you endorse it. When you endorse another agent's answer, copy its final_answer wording so the votes can be matched.`)
	}
	sb.WriteString("\n\n## Response format\n\nEnd your message with exactly one fenced JSON vote block:\n\n")
	sb.WriteString(voteFormat)
	sb.WriteString("\n")
	return sb.String()
}

// ModeratorPrompt asks the moderator to synthesize the final answer.
func ModeratorPrompt(question, self string, agents []string, rounds int, converged bool, discussion, tally string) string {
	state := "without reaching full consensus"
	if converged {
		state = "with consensus"
	}
	return fmt.Sprintf(`You are %q, the MODERATOR of a group chat among AI agents (%s). The discussion ended after %d round(s) %s.

## User request

%s

## Discussion

%s

## Latest votes

%s

## Instructions

Synthesize the group's final consensus answer for the user. Prefer the position with the strongest support and the best arguments; resolve remaining disagreements explicitly and briefly note any important caveats or dissent. Answer in text only.

End your message with exactly one fenced JSON vote block:

%s
`, self, strings.Join(agents, ", "), rounds, state, strings.TrimSpace(question), discussion, tally, voteFormat)
}
