#!/bin/sh
# fake-agent.sh - a deterministic stand-in for an LLM coding CLI, used to
# demo and test council without codex, claude, agy or opencode installed.
#
# usage: fake-agent.sh NAME INITIAL_ANSWER [follow|stubborn] [DELAY_SECONDS]
#
# Reads the council prompt on stdin and replies with a message ending in a
# JSON vote block:
#   round 1    -> answers INITIAL_ANSWER independently
#   round 2+   -> "follow": adopts the most common answer seen so far and agrees
#                 "stubborn": keeps INITIAL_ANSWER, agrees only if it is the majority
#   moderator  -> summarises and picks the majority answer
set -eu
case "${1:-}" in --status-probe) echo "Logged in (fake)"; exit 0 ;; --version) echo "fake-agent 1.0"; exit 0 ;; esac
name=${1:?name}
initial=${2:?initial answer}
mode=${3:-follow}
delay=${4:-0}

prompt=$(cat)
if [ "$delay" != "0" ]; then sleep "$delay"; fi

vote() { # answer agree confidence
	printf '\n```json\n{"final_answer": "%s", "agree": %s, "confidence": %s}\n```\n' "$1" "$2" "$3"
}

# Most common final_answer among the votes quoted in the prompt (ties: alphabetical).
majority() {
	printf '%s\n' "$prompt" |
		grep -o '"final_answer": "[^"<]*"' |
		sed 's/^"final_answer": "\(.*\)"$/\1/' |
		sort | uniq -c | sort -k1,1nr -k2 | head -n 1 |
		sed 's/^ *[0-9][0-9]* //'
}

case "$prompt" in
*"the MODERATOR"*)
	m=$(majority)
	echo "As moderator ($name) I reviewed every round. The group's strongest, best-supported answer is $m; the remaining objections were addressed in the discussion."
	vote "$m" true 0.95
	exit 0
	;;
*"ROUND 1 of"*)
	echo "[$name] Working independently: my answer is $initial."
	vote "$initial" false 0.6
	exit 0
	;;
esac

m=$(majority)
if [ "$mode" = "stubborn" ]; then
	if [ "$m" = "$initial" ]; then
		echo "[$name] The others now agree with my answer $initial. Agreed."
		vote "$initial" true 0.9
	else
		echo "[$name] I still believe $initial is correct, not $m."
		vote "$initial" false 0.9
	fi
else
	if [ "$m" = "$initial" ]; then
		echo "[$name] Having read the others, I stand by $initial; it matches the majority."
	else
		echo "[$name] I reviewed the other answers; the majority answer $m is right and my $initial was a mistake. Revising."
	fi
	vote "$m" true 0.8
fi
