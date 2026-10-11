# Web UI

`council serve` starts a small web UI on your machine. You type a prompt,
pick the agents, and watch the group chat arrive round by round until the
council agrees. It runs the same code as `council ask` (`internal/chat`), so
results, consensus rules and transcripts are identical.

```sh
council serve                                   # http://127.0.0.1:8787/
council serve --open                            # and open it in the default browser
council serve --addr 127.0.0.1:9000             # another port
council serve --config examples/fake-council.yaml --open   # demo with no LLM CLI
```

![council web UI](images/web-ui.png)

In dark mode: [images/web-ui-dark.png](images/web-ui-dark.png).

## Flags

| Flag | Default | Meaning |
|---|---|---|
| `--addr` | `127.0.0.1:8787` | Address to listen on. Use port `0` for a free port. |
| `--open` | off | Open the UI in the default browser (`open` on macOS, `xdg-open` on Linux and BSD, `rundll32 url.dll,FileProtocolHandler` on Windows). |
| `--config` | `~/.config/council/config.yaml` | Config file. It is read again on every request, so edits apply without a restart. |
| `--max-runs` | `2` | Number of runs allowed at the same time; more get `429 Too Many Requests`. |

Ctrl-C stops the server and cancels every run in progress. The agents'
processes are killed, as with `council ask`.

## The page

* **Prompt**: a text area. Ctrl+Enter (Cmd+Enter on macOS) runs it.
* **Agents**: one checkbox per configured agent, ticked for `defaults.agents`.
  Each shows its harness kind and the same install and sign-in status as
  `council doctor` (`checking…`, `ready`, `not installed`, `signed out`,
  `status unknown`); an agent that is not ready also shows why, for example
  agy's "Please sign in to view available models". The checks run in the
  background and the results are cached for 3 minutes, so reloading the page
  is fast; **refresh status** runs them again.
* **Rounds**, **Moderator** and **Turn timeout** (`90s`, `5m`; empty uses
  each agent's own timeout). The moderator list has "majority vote" and every
  configured agent, whether or not it is ticked: an agent can moderate a chat
  it does not take part in. The choice is kept when you tick or untick agents
  and when the status refreshes.
* **Loose** checkbox: the same as `council ask --loose`.
* **Run**, **Cancel** and **Download transcript (.md)**. The download is the
  same Markdown as `council ask --transcript`.
* **Chat**: messages grouped by round, one colour per agent, with the turn's
  duration and the parsed vote (answer, agrees or not, confidence). Failed
  turns show their error.
* **Consensus card**: the final answer, how it was reached (unanimous,
  majority or moderator), who agreed and who dissented, and the
  reasoning or moderator details. If the chosen moderator fails (not signed
  in, timed out), the card says so and shows that the majority vote stands.

The page follows the operating system's light or dark setting and works on
narrow screens. It is plain HTML, CSS and JavaScript embedded in the binary
with `go:embed`; there is no build step and nothing is loaded from the
internet.

## Security

The agents are CLIs signed in to your accounts, and they run in the
directory where you started `council serve`. Anyone who can call the API can
use them, so the server is locked to your machine and your browser tab:

* It listens on `127.0.0.1` by default. With any other `--addr` it prints a
  warning: everyone who can reach the port can run your agents. There is no
  login, so only do that behind an authenticating proxy or on a trusted
  network.
* Requests must name a loopback host (`localhost`, `127.0.0.1`, `[::1]`) in
  the `Host` header, which stops DNS rebinding attacks. This check is off
  when you bind to a non-loopback address on purpose.
* `POST` requests must
  * not be cross-site (`Sec-Fetch-Site` is `same-origin` or `none`),
  * carry an `Origin` that matches the `Host`, when present,
  * be `Content-Type: application/json`, which a plain HTML form cannot send,
  * carry the `X-Council-Token` header. The token is random per server
    process and is embedded in the page in a `<meta name="council-token">`
    tag, which another site cannot read.
* Responses have a strict `Content-Security-Policy` (no inline scripts, no
  third-party content), `X-Frame-Options: DENY`, `X-Content-Type-Options:
  nosniff` and `Referrer-Policy: no-referrer`.
* Input is validated: a prompt of at most 200,000 bytes, at most 12 agents
  that must exist in the config, 1 to 10 rounds, a timeout of at most 1 hour.

## HTTP API

The UI uses a small JSON API that scripts can use too. Fetch the token from
the page first:

```sh
TOKEN=$(curl -s http://127.0.0.1:8787/ | sed -n 's/.*council-token" content="\([0-9a-f]*\)".*/\1/p')
```

| Method and path | Purpose |
|---|---|
| `GET /` | The page. |
| `GET /static/{app.js,style.css}` | Its script and stylesheet. |
| `GET /api/agents` | Agents, defaults and limits. `?status=0` skips the install and sign-in checks; `?refresh=1` ignores cached results. |
| `POST /api/run` | Start a run. The response is a stream of Server-Sent Events. |
| `POST /api/runs/{id}/cancel` | Cancel a run. `404` if no such run is active. |

Errors are JSON, `{"error": "…"}`, with status `400` (invalid input), `403`
(failed security check), `404`, `429` (too many runs) or `500` (for example
an invalid config file).

### `GET /api/agents`

```json
{
  "config": "/home/me/.config/council/config.yaml",
  "agents": [
    {"agent": "codex", "kind": "codex", "command": "codex", "installed": true,
     "path": "/usr/local/bin/codex", "version": "codex-cli 0.50.0",
     "auth": "ok", "detail": "Logged in using ChatGPT", "default": true}
  ],
  "defaults": {"agents": ["codex", "claude", "agy", "cursor"], "rounds": 3,
               "moderator": "majority", "timeout": "10m0s"},
  "limits": {"max_prompt_bytes": 200000, "max_agents": 12, "max_rounds": 10,
             "max_timeout_ns": 3600000000000}
}
```

`auth` is `ok`, `missing` or `unknown`. Default agents come first. Status
results are cached per agent (name, kind, command and status command) for 3
minutes; a check that timed out is not cached.

### `POST /api/run`

```sh
curl -N http://127.0.0.1:8787/api/run \
  -H "Content-Type: application/json" -H "X-Council-Token: $TOKEN" \
  -d '{"prompt": "What is 6*7?", "agents": ["codex", "claude"], "rounds": 3,
       "moderator": "majority", "timeout": "5m", "loose": false}'
```

Only `prompt` is required. The others default to the config's `defaults`;
`moderator` is an agent name, or `majority` (also `vote`, `none`) for no
moderator. Unknown fields are rejected.

The response is `text/event-stream`. Each event is one `event:` name and one
JSON `data:` line:

| Event | Data |
|---|---|
| `start` | `{"id", "agents", "rounds", "moderator"}`. `id` is what the cancel endpoint takes. |
| `round_start` | `{"round"}` |
| `message` | `{"round", "agent", "role": "agent"\|"moderator", "text", "vote": {"answer", "agree", "confidence"}, "error", "duration_ms"}`. `text` has the vote block removed; `vote` and `error` are omitted when absent. |
| `consensus` | `{"round", "answer"}` when the agents agree. |
| `no_consensus` | `{"round"}` when the last round ends without agreement. |
| `moderator_start` | `{"round"}` |
| `result` | `{"answer", "details", "method", "converged", "rounds", "supporters", "dissenters", "moderator", "notes"}` |
| `error` | `{"error"}` when the run fails (for example every agent failed). |
| `cancelled` | `{"error"}` when the run was cancelled. |
| `transcript` | `{"markdown"}`: the transcript so far. Sent last, after `result`, `error` or `cancelled`. |

A `: keep-alive` comment is sent every 15 seconds while agents are working.

Closing the connection cancels the run, so a script that wants to stop a run
can just disconnect. The stream must be read with `fetch` or `curl`, not
`EventSource`, because the run is started with a `POST`.

## Limitations

* No login and no TLS: it is meant for one person on one machine.
* Runs live only as long as their connection. Reloading the page cancels the
  run, and there is no history; download the transcript to keep it.
* Messages appear when each turn is complete, not token by token, as with
  `council ask --verbose`.
* Agents run in the server's working directory, like `council ask`.
* The status badges take as long as `council doctor`, a few seconds per agent
  for CLIs with slow status commands.
