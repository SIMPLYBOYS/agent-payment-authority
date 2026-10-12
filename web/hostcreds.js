// hostcreds.js: the credentials of the host's local page /host
// (docs/demo-operation-design.md section 6.3), without any DOM, so the
// asynchronous cases can be tested in Node (web/test/hostcreds.test.mjs).
//
// It holds the host token and the viewer tokens of one loaded token file,
// in memory only. Every load, clear, manual-viewer switch and pagehide
// starts a new generation; an asynchronous step (reading the file, each
// verification request, a readiness poll) commits nothing unless its
// generation is still the current one. A 401 is applied only when it
// belongs to the current generation. Nothing here writes storage, a URL,
// a cookie or the console, and no message carries file content or a token.

export const MAX_FILE_BYTES = 16 * 1024;
const TOKEN = /^[0-9a-f]{64}$/;
const CONSUMER = /^[A-Za-z0-9._-]{1,64}$/;

// Fixed texts for every failure; never the file's content.
export const MESSAGES = {
  too_large: "token 檔過大（上限 16 KiB），未讀取",
  bad_file: "token 檔格式不符，未採用",
  mismatch: "token 檔與目前服務不符（服務可能已重啟），請重新載入",
  unreachable: "無法連線到 demo host，token 檔未採用",
  stale: "服務已重啟或 token 失效，請重新載入 token 檔",
  pair_format: "配對碼格式不符（12 個英數字，可含 - 或空格），沒有送出",
  pair_failed: "配對碼不正確；連續錯 5 次後本次配對失效",
  pair_closed: "配對碼已使用、逾時或失效：請改用「進階」的 token 檔（啟動時印出的場次目錄 tok/tokens.json），或重新啟動展示",
  pair_off: "這個服務沒有啟用配對：請改用「進階」的 token 檔",
};

const CODE = /^[A-Z2-7]{12}$/;

// parseTokenFile checks the text of var/demo-tokens.json and returns
// { host, operator, viewers: Map(consumer_id -> token) }, or null when
// anything is off: exactly one host, exactly one operator (slice 3.2), at
// least one viewer, no other role.
export function parseTokenFile(text) {
  let doc;
  try { doc = JSON.parse(text); } catch { return null; }
  if (!doc || typeof doc !== "object" || Array.isArray(doc)) return null;
  if (Object.keys(doc).some((k) => k !== "note" && k !== "tokens")) return null;
  if (doc.note !== undefined && typeof doc.note !== "string") return null;
  if (!Array.isArray(doc.tokens)) return null;
  let host = null;
  let operator = null;
  const viewers = new Map();
  const seen = new Set();
  for (const t of doc.tokens) {
    if (!t || typeof t !== "object" || Array.isArray(t)) return null;
    if (Object.keys(t).some((k) => k !== "role" && k !== "token" && k !== "consumer_id")) return null;
    if (typeof t.token !== "string" || !TOKEN.test(t.token) || seen.has(t.token)) return null;
    seen.add(t.token);
    if (t.role === "host" || t.role === "operator") {
      if ((t.role === "host" ? host : operator) !== null || "consumer_id" in t) return null;
      if (t.role === "host") host = t.token; else operator = t.token;
    } else if (t.role === "viewer") {
      if (typeof t.consumer_id !== "string" || !CONSUMER.test(t.consumer_id) || viewers.has(t.consumer_id)) return null;
      viewers.set(t.consumer_id, t.token);
    } else {
      return null;
    }
  }
  if (host === null || operator === null || viewers.size === 0) return null;
  return { host, operator, viewers };
}

// createHostCreds: call(token, path, signal, post) -> Promise<{ status, body }>
// does one GET (or a JSON POST of post.body); onChange(view) is told after
// every change. newId makes a request ID; wait(ms) pauses between resends.
export function createHostCreds({ call, onChange, newId = () => crypto.randomUUID(), wait = (ms) => new Promise((r) => setTimeout(r, ms)) }) {
  let gen = 0;
  let abort = new AbortController();
  let held = null; // { host, viewers } of the current generation, once verified
  // P1-a: the fake-only simulated approver (docs/commercial-transaction-flow.md 9):
  // { consumer, token } of this generation, in memory only; one consumer at
  // a time (the host replaces the previous one too). Not a person's login.
  let sim = null;
  let mode = "none"; // none | loading | pairing | file | paired | manual
  let fileName = "";
  let message = "";

  const view = () => ({ gen, mode, fileName, message, consumers: held ? [...held.viewers.keys()] : [], hasHost: !!held, simConsumer: sim ? sim.consumer : null });
  const changed = () => onChange(view());

  // next ends every step of the previous generation and drops all tokens.
  function next(newMode, msg) {
    gen++;
    abort.abort();
    abort = new AbortController();
    held = null;
    sim = null;
    mode = newMode;
    fileName = "";
    message = msg || "";
    return gen;
  }

  // clearAll: the 「清除全部憑證」 button, pagehide, and a current 401.
  function clearAll(msg) {
    next("none", msg);
    changed();
  }

  // manual: the page is switching to one typed viewer token; all loaded
  // tokens go first.
  function manual() {
    next("manual", "手動 viewer 模式：已清除載入的 token 檔");
    changed();
  }

  // load reads and checks file (a File or anything with size, name, text()),
  // then confirms every token with this run of the host before keeping any.
  async function load(file) {
    const g = next("loading", "讀取 token 檔…");
    const signal = abort.signal;
    changed();
    const fail = (code) => { if (g === gen) { next("none", MESSAGES[code]); changed(); } };
    if (!file || typeof file.size !== "number" || typeof file.text !== "function") return fail("bad_file");
    if (file.size > MAX_FILE_BYTES) return fail("too_large"); // not read at all
    let text;
    try { text = await file.text(); } catch { return fail("bad_file"); }
    if (g !== gen) return;
    const parsed = parseTokenFile(text);
    text = null;
    if (!parsed) return fail("bad_file");
    await adopt(g, signal, parsed, fail, "file", typeof file.name === "string" ? file.name.slice(0, 80) : "",
      "已載入 token 檔：host、operator 與 " + parsed.viewers.size + " 位 viewer");
  }

  // pair trades the one-time code the host showed on its terminal for this
  // run's tokens (POST /api/host/pair, no token sent), then confirms and
  // holds them exactly as a loaded file. The code is checked here first
  // (nothing is sent for a malformed one) and is never kept or shown.
  async function pair(typed) {
    const g = next("pairing", "配對中…");
    const signal = abort.signal;
    changed();
    const fail = (code) => { if (g === gen) { next("none", MESSAGES[code]); changed(); } };
    const code = typeof typed === "string" ? typed.replace(/[\s-]/g, "").toUpperCase() : "";
    if (!CODE.test(code)) return fail("pair_format");
    let r;
    try { r = await call(null, "/api/host/pair", signal, { body: { code } }); } catch { return fail("unreachable"); }
    if (g !== gen) return; // cleared, left or replaced meanwhile: a late answer signs nothing in
    if (r.status === 410) return fail("pair_closed");
    if (r.status === 404 || r.status === 405) return fail("pair_off");
    if (r.status !== 200) return fail("pair_failed");
    const parsed = parseTokenFile(JSON.stringify(r.body));
    if (!parsed) return fail("bad_file");
    await adopt(g, signal, parsed, fail, "paired", "", "已配對：本次服務的主持人憑證只在這個頁面的記憶體中（離開或清除即失效）");
  }

  // adopt confirms every token with this run of the host before keeping
  // any: the host and operator tokens must each be what the server says
  // they are (caller_role), not just accepted somewhere.
  async function adopt(g, signal, parsed, fail, newMode, name, msg) {
    try {
      for (const [token, role] of [[parsed.host, "host"], [parsed.operator, "operator"]]) {
        const st = await call(token, "/api/host/status", signal);
        if (g !== gen) return;
        if (st.status !== 200 || !st.body || st.body.caller_role !== role) return fail("mismatch");
      }
      for (const [consumer, token] of parsed.viewers) {
        const v = await call(token, "/api/viewer", signal);
        if (g !== gen) return;
        if (v.status !== 200 || !v.body || v.body.role !== "viewer" || v.body.consumer_id !== consumer) return fail("mismatch");
      }
    } catch {
      return fail("unreachable");
    }
    if (g !== gen) return;
    held = parsed;
    mode = newMode;
    fileName = name;
    message = msg;
    changed();
  }

  // unauthorized(g): a 401 seen by a step of generation g; applied only if
  // g is still current, so an old request cannot clear newer credentials.
  function unauthorized(g) {
    if (g !== gen) return false;
    clearAll(MESSAGES.stale);
    return true;
  }

  // readiness reads GET /api/host/status with the host token; null when
  // there is no host token, the answer is stale, or it was a 401 (applied).
  async function readiness() {
    if (!held) return null;
    const g = gen;
    let r;
    try { r = await call(held.host, "/api/host/status", abort.signal); } catch { return g === gen ? { error: "unreachable" } : null; }
    if (g !== gen) return null;
    if (r.status === 401) { unauthorized(g); return null; }
    if (r.status !== 200) return { error: "status_" + r.status };
    return { status: r.body };
  }

  // operatorCall sends one control request of generation g; null when the
  // generation changed (the reply is dropped) or on a 401 (applied).
  async function operatorCall(g, path, post, timeoutMs) {
    if (!held || g !== gen) return null;
    const signal = AbortSignal.any([abort.signal, AbortSignal.timeout(timeoutMs)]);
    const r = await call(held.operator, path, signal, post);
    if (g !== gen) return null;
    if (r.status === 401) { unauthorized(g); return null; }
    return r;
  }

  // startRun is one start operation (section 13.8 R4): one request ID, made
  // here and kept in memory; a network error or timeout resends the same ID
  // within the same generation only. A reply of an older generation is
  // dropped: nothing switches, signs in or opens. Clearing the credentials
  // does not withdraw a run the server accepted, and never resends it.
  async function startRun(scenario, delegationId) {
    if (!held) return null;
    const g = gen;
    const requestId = newId();
    const body = delegationId ? { request_id: requestId, scenario, delegation_id: delegationId } : { request_id: requestId, scenario };
    for (let attempt = 0; attempt < 3; attempt++) {
      if (g !== gen) return null;
      let r;
      try {
        r = await operatorCall(g, "/api/host/runs", { body }, 8000);
      } catch {
        if (g !== gen) return null;
        await wait(500);
        continue;
      }
      if (!r) return null;
      return { gen: g, requestId, status: r.status, body: r.body };
    }
    return g === gen ? { gen: g, requestId, error: "unreachable" } : null;
  }

  // stopRun stops a running run's process; the reply counts only in its
  // own generation. No resend: a lost reply shows in the runs list.
  async function stopRun(runId) {
    if (!held) return null;
    const g = gen;
    try {
      const r = await operatorCall(g, "/api/host/runs/" + encodeURIComponent(runId) + "/stop", { body: {} }, 8000);
      return r && { gen: g, status: r.status, body: r.body };
    } catch {
      return g === gen ? { gen: g, error: "unreachable" } : null;
    }
  }

  // cancelTask asks the server to cancel a stored task (slice 3.4). No
  // resend: a lost reply is "not confirmed", and a second click is a new
  // request the server answers once (already: true). Same generation rule.
  async function cancelTask(taskId) {
    if (!held) return null;
    const g = gen;
    try {
      const r = await operatorCall(g, "/api/host/tasks/" + encodeURIComponent(taskId) + "/cancel", { body: {} }, 8000);
      return r && { gen: g, status: r.status, body: r.body };
    } catch {
      return g === gen ? { gen: g, error: "unreachable" } : null;
    }
  }

  // hostRead is a GET with the read-only host token (the task list and a
  // task: what the host may read anyway); same generation and 401 rules.
  async function hostRead(path) {
    if (!held) return null;
    const g = gen;
    let r;
    try { r = await call(held.host, path, abort.signal); } catch { return g === gen ? { gen: g, error: "unreachable" } : null; }
    if (g !== gen) return null;
    if (r.status === 401) { unauthorized(g); return null; }
    return { gen: g, status: r.status, body: r.body };
  }

  // runs reads the latest runs (operator token), same generation rule.
  async function runs() {
    if (!held) return null;
    const g = gen;
    try {
      const r = await operatorCall(g, "/api/host/runs", undefined, 8000);
      return r && r.status === 200 ? { gen: g, runs: r.body.runs } : r && { gen: g, error: "status_" + r.status };
    } catch {
      return g === gen ? { gen: g, error: "unreachable" } : null;
    }
  }

  // simulate asks the host (operator, fake only) to act as consumer; the
  // capability it returns replaces any previous one. A reply of an older
  // generation is dropped.
  async function simulate(consumer) {
    if (!held) return null;
    const g = gen;
    try {
      const r = await operatorCall(g, "/api/host/simulate", { body: { consumer_id: consumer } }, 8000);
      if (!r) return null;
      if (r.status === 200 && r.body && r.body.consumer_id === consumer && TOKEN.test(r.body.token || "")) {
        sim = { consumer, token: r.body.token };
        changed();
      }
      return { gen: g, status: r.status, body: r.status === 200 ? { consumer_id: consumer } : r.body };
    } catch {
      return g === gen ? { gen: g, error: "unreachable" } : null;
    }
  }

  // simCall is one request with the simulated approver's capability; a 401
  // (expired, or replaced by the host) drops it. Same generation rule.
  async function simCall(path, post, timeoutMs = 8000) {
    if (!sim) return null;
    const g = gen, mine = sim;
    const signal = AbortSignal.any([abort.signal, AbortSignal.timeout(timeoutMs)]);
    const r = await call(mine.token, path, signal, post);
    if (g !== gen || sim !== mine) return null;
    if (r.status === 401) { sim = null; changed(); return null; }
    return { gen: g, consumer: mine.consumer, status: r.status, body: r.body };
  }

  async function approvals() {
    try { return await simCall("/api/sim/approvals"); } catch { return sim ? { gen, error: "unreachable" } : null; }
  }

  // confirmDelegation and resolve are one operation each: one request ID,
  // resent unchanged on a network error within the generation; the server
  // answers a resend with the same result.
  async function once(path, body) {
    if (!sim) return null;
    const g = gen;
    const request = { ...body, request_id: newId() };
    for (let attempt = 0; attempt < 3; attempt++) {
      if (g !== gen) return null;
      try {
        const r = await simCall(path, { body: request });
        return r && { ...r, requestId: request.request_id };
      } catch {
        if (g !== gen) return null;
        await wait(500);
      }
    }
    return g === gen ? { gen: g, error: "unreachable" } : null;
  }
  const confirmDelegation = (scenario) => once("/api/sim/delegations", { scenario });
  const resolve = (sessionId, intentHash, decision) => once("/api/sim/approvals/" + encodeURIComponent(sessionId), { intent_hash: intentHash, decision });

  return {
    load, pair, clearAll, manual, unauthorized, readiness, startRun, stopRun, cancelTask, hostRead, runs,
    simulate, approvals, confirmDelegation, resolve,
    viewerToken: (consumer) => (held ? held.viewers.get(consumer) || null : null),
    get gen() { return gen; },
    view,
  };
}
