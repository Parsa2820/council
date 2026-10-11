// council web UI: plain JavaScript, no build step.
"use strict";

const $ = (id) => document.getElementById(id);
const token = document.querySelector('meta[name="council-token"]').content;
const state = { run: null, colors: {}, transcript: "", round: 0, pending: new Set(), moderator: "majority", agentKey: undefined };

function el(tag, cls, text) {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text !== undefined) e.textContent = text;
  return e;
}

function colorOf(agent) {
  if (!(agent in state.colors)) state.colors[agent] = "c" + (Object.keys(state.colors).length % 8);
  return state.colors[agent];
}

function setStatus(text) { $("status").textContent = text; }

function showError(text) {
  const box = $("error");
  box.hidden = !text;
  box.textContent = text || "";
}

function badge(a) {
  if (a.checking) return ["checking", "checking…"];
  if (!a.installed) return ["absent", "not installed"];
  if (a.auth === "ok") return ["ok", "ready"];
  if (a.auth === "missing") return ["missing", "signed out"];
  return ["unknown", "status unknown"];
}

function setBadge(row, a) {
  let b = row.querySelector(".badge");
  if (!b) { b = el("span"); row.append(b); }
  const [cls, text] = badge(a);
  b.className = "badge " + cls;
  b.textContent = text;
  b.title = a.checking ? "Checking install and sign-in status…" : [a.version, a.detail].filter(Boolean).join(" · ");
  // Say why an agent is not ready, e.g. "Please sign in to view available models".
  let why = row.querySelector(".why");
  const show = !a.checking && cls !== "ok" && a.detail;
  if (show && !why) { why = el("span", "why"); row.append(why); }
  if (why) { why.hidden = !show; why.textContent = show ? a.detail : ""; }
}

// renderAgents builds the agent checkboxes and the moderator list. It only
// runs when the set of agents changes, so status updates never rebuild the
// <select> (which would close it while the user has it open) and never
// touch the user's choices.
function renderAgents(body) {
  const list = $("agent-list");
  const first = state.agentKey === undefined;
  const checked = new Set([...list.querySelectorAll("input:checked")].map((i) => i.value));
  list.replaceChildren();
  for (const a of body.agents) {
    const row = el("label", "agent");
    row.dataset.agent = a.agent;
    const box = el("input");
    box.type = "checkbox";
    box.value = a.agent;
    box.checked = first ? a.default : checked.has(a.agent);
    row.append(box, el("span", "name", a.agent), el("span", "kind", a.kind || ""));
    setBadge(row, { checking: true });
    list.append(row);
  }
  if (!body.agents.length) list.append(el("p", "muted", "No agents configured. Run council init."));

  // Any configured agent can moderate, whether or not it also takes part.
  const mod = $("moderator");
  if (first) state.moderator = body.defaults.moderator;
  mod.replaceChildren(new Option("majority vote", "majority"));
  for (const a of body.agents) mod.append(new Option(a.agent, a.agent));
  mod.value = [...mod.options].some((o) => o.value === state.moderator) ? state.moderator : "majority";
  if (first) {
    $("rounds").value = body.defaults.rounds;
    $("timeout").placeholder = "agent default (" + body.defaults.timeout + ")";
  }
  state.agentKey = body.agents.map((a) => a.agent).join("\n");
}

async function loadAgents(withStatus, refresh) {
  const list = $("agent-list");
  if (withStatus) list.querySelectorAll(".agent").forEach((row) => setBadge(row, { checking: true }));
  try {
    const q = withStatus ? (refresh ? "?refresh=1" : "") : "?status=0";
    const res = await fetch("/api/agents" + q);
    const body = await res.json();
    if (!res.ok) throw new Error(body.error || res.statusText);
    $("config-path").textContent = "config: " + body.config;
    if (body.agents.map((a) => a.agent).join("\n") !== state.agentKey) renderAgents(body);
    if (!withStatus) return;
    for (const a of body.agents) {
      const row = list.querySelector('.agent[data-agent="' + CSS.escape(a.agent) + '"]');
      if (row) setBadge(row, a);
    }
  } catch (err) {
    state.agentKey = undefined;
    list.replaceChildren(el("p", "error", "Could not load agents: " + err.message));
  }
}

function resetChat() {
  $("chat").replaceChildren();
  $("final").hidden = true;
  showError("");
  state.transcript = "";
  state.round = 0;
  state.colors = {};
  $("download").disabled = true;
}

function pendingLine() {
  let p = $("chat").querySelector(".pending");
  if (!p) p = el("p", "pending");
  $("chat").append(p); // keep it last
  p.textContent = state.pending.size ? "Waiting for " + [...state.pending].join(", ") + "…" : "";
  p.hidden = state.pending.size === 0;
}

function addMessage(m, agents) {
  const card = el("article", "msg " + colorOf(m.agent));
  if (m.role === "moderator") card.classList.add("moderator");
  if (m.error) card.classList.add("failed");
  const head = el("header");
  head.append(el("span", "who", (m.role === "moderator" ? "moderator · " : "") + m.agent));
  head.append(el("span", "dur", (m.duration_ms / 1000).toFixed(1) + "s"));
  card.append(head);
  card.append(el("div", "text", m.error ? "✗ " + m.error : m.text || "(empty reply)"));
  if (m.vote) {
    const v = el("div", "vote");
    v.append(el("span", "ans", "Answer: " + m.vote.answer));
    v.append(el("span", m.vote.agree ? "yes" : "no", m.vote.agree ? "✓ agrees" : "does not agree yet"));
    v.append(el("span", "conf", "confidence " + Math.round(m.vote.confidence * 100) + "%"));
    card.append(v);
  } else if (!m.error) {
    card.append(el("div", "vote no", "No vote block: counted as an abstention"));
  }
  state.pending.delete(m.agent);
  $("chat").append(card);
  pendingLine();
}

function roundHead(text) {
  const h = el("h3", "round-head", text);
  if (!$("chat").querySelector(".round-head")) h.classList.add("first");
  $("chat").append(h);
}

function showResult(r) {
  $("final").hidden = false;
  $("final").classList.toggle("split", !r.converged);
  $("final").querySelector("h2").textContent = r.converged ? "Consensus" : "No full consensus: decided by " + r.method;
  $("final-answer").textContent = r.answer;
  let meta = "Method: " + r.method + (r.method === "moderator" ? " (" + r.moderator + ")" : "") + " · " + r.rounds + " round(s)";
  if (r.moderator && r.method !== "moderator") meta += " · moderator " + r.moderator + " failed, so the majority vote stands";
  $("final-meta").textContent = meta;
  $("final-for").textContent = r.supporters.join(", ") || "nobody";
  $("final-against").textContent = r.dissenters.join(", ") || "nobody";
  $("final-details").textContent = r.details || "";
  $("final-details-box").hidden = !r.details || r.details.trim() === r.answer.trim();
  const notes = $("final-notes");
  notes.replaceChildren(...(r.notes || []).map((n) => el("li", /^moderator .* failed/.test(n) ? "warn" : "", n)));
  $("final").scrollIntoView({ behavior: "smooth", block: "nearest" });
}

function handle(name, data, agents) {
  switch (name) {
    case "start":
      state.run.id = data.id;
      agents.push(...data.agents);
      data.agents.forEach(colorOf);
      if (data.moderator) colorOf(data.moderator);
      $("cancel").disabled = false;
      setStatus("Running " + data.agents.join(", ") + " for up to " + data.rounds + " round(s), " +
        (data.moderator ? "moderated by " + data.moderator : "decided by majority vote") + "…");
      break;
    case "round_start":
      state.round = data.round;
      roundHead("Round " + data.round);
      agents.forEach((a) => state.pending.add(a));
      pendingLine();
      break;
    case "message":
      addMessage(data, agents);
      break;
    case "consensus":
      setStatus("Consensus in round " + data.round + ".");
      break;
    case "no_consensus":
      setStatus("No consensus after " + data.round + " round(s).");
      break;
    case "moderator_start":
      roundHead("Moderator");
      setStatus("The moderator is writing the final answer…");
      break;
    case "result":
      showResult(data);
      setStatus("Done.");
      break;
    case "error":
      showError(data.error);
      setStatus("Failed.");
      break;
    case "cancelled":
      showError("Cancelled: " + data.error);
      setStatus("Cancelled.");
      break;
    case "transcript":
      state.transcript = data.markdown;
      $("download").disabled = false;
      break;
  }
}

async function run(ev) {
  ev.preventDefault();
  if (state.run) return;
  const agents = [...document.querySelectorAll("#agent-list input:checked")].map((i) => i.value);
  const body = {
    prompt: $("prompt").value,
    agents,
    rounds: Number($("rounds").value) || 0,
    moderator: $("moderator").value,
    timeout: $("timeout").value.trim(),
    loose: $("loose").checked,
  };
  if (!body.prompt.trim()) { showError("Write a prompt first."); return; }
  if (!agents.length) { showError("Pick at least one agent."); return; }
  resetChat();
  const ctrl = new AbortController();
  state.run = { ctrl, id: null };
  $("run").disabled = true;
  $("cancel").disabled = false;
  setStatus("Starting…");
  const seen = [];
  try {
    const res = await fetch("/api/run", {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-Council-Token": token },
      body: JSON.stringify(body),
      signal: ctrl.signal,
    });
    if (!res.ok) {
      const err = await res.json().catch(() => ({ error: res.statusText }));
      throw new Error(err.error || res.statusText);
    }
    const reader = res.body.pipeThrough(new TextDecoderStream()).getReader();
    let buf = "";
    for (;;) {
      const { value, done } = await reader.read();
      if (done) break;
      buf += value;
      let i;
      while ((i = buf.indexOf("\n\n")) >= 0) {
        const chunk = buf.slice(0, i);
        buf = buf.slice(i + 2);
        let name = "message", data = "";
        for (const line of chunk.split("\n")) {
          if (line.startsWith("event: ")) name = line.slice(7);
          else if (line.startsWith("data: ")) data += line.slice(6);
        }
        if (data) handle(name, JSON.parse(data), seen);
      }
    }
  } catch (err) {
    if (err.name === "AbortError") { showError("Cancelled."); setStatus("Cancelled."); }
    else { showError(err.message); setStatus("Failed."); }
  } finally {
    state.run = null;
    state.pending.clear();
    pendingLine();
    $("run").disabled = false;
    $("cancel").disabled = true;
  }
}

async function cancel() {
  const r = state.run;
  if (!r) return;
  $("cancel").disabled = true;
  setStatus("Cancelling…");
  if (r.id) {
    try {
      await fetch("/api/runs/" + encodeURIComponent(r.id) + "/cancel", {
        method: "POST",
        headers: { "Content-Type": "application/json", "X-Council-Token": token },
        body: "{}",
      });
      return; // the stream ends with a "cancelled" event and the transcript
    } catch (_) { /* fall through */ }
  }
  r.ctrl.abort();
}

function download() {
  if (!state.transcript) return;
  const url = URL.createObjectURL(new Blob([state.transcript], { type: "text/markdown" }));
  const a = el("a");
  a.href = url;
  a.download = "council-transcript-" + new Date().toISOString().replace(/[:.]/g, "-").slice(0, 19) + ".md";
  document.body.append(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

$("run-form").addEventListener("submit", run);
$("cancel").addEventListener("click", cancel);
$("download").addEventListener("click", download);
$("refresh").addEventListener("click", () => loadAgents(true, true));
$("moderator").addEventListener("change", (e) => { state.moderator = e.target.value; });
$("prompt").addEventListener("keydown", (e) => {
  if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) $("run-form").requestSubmit();
});
loadAgents(false).then(() => loadAgents(true));
