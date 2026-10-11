// Package web implements `council serve`: a small local web UI for running
// the council.
//
// The server embeds a single page (static/index.html, app.js, style.css) and
// exposes a JSON API:
//
//	GET  /api/agents             configured agents with install/sign-in status
//	POST /api/run                start a run; the response is a Server-Sent
//	                             Events stream of chat events and the result
//	POST /api/runs/{id}/cancel   cancel a running run
//
// Runs use internal/chat directly, through its OnEvent hook, so the web UI
// and `council ask` share all discussion and consensus logic. A run is tied
// to its HTTP request: closing the stream cancels it and kills the agents'
// processes.
//
// Because the agents are local CLIs that hold the user's credentials, every
// request must carry a loopback Host header (unless the server was bound to
// another address on purpose), and state-changing requests must be
// same-origin JSON requests that carry the per-process token embedded in the
// page.
package web
