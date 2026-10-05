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
};

// parseTokenFile checks the text of var/demo-tokens.json and returns
// { host, viewers: Map(consumer_id -> token) }, or null when anything is off.
// Only host and viewer roles are accepted (operator comes with slice 3.2).
export function parseTokenFile(text) {
  let doc;
  try { doc = JSON.parse(text); } catch { return null; }
  if (!doc || typeof doc !== "object" || Array.isArray(doc)) return null;
  if (Object.keys(doc).some((k) => k !== "note" && k !== "tokens")) return null;
  if (doc.note !== undefined && typeof doc.note !== "string") return null;
  if (!Array.isArray(doc.tokens)) return null;
  let host = null;
  const viewers = new Map();
  const seen = new Set();
  for (const t of doc.tokens) {
    if (!t || typeof t !== "object" || Array.isArray(t)) return null;
    if (Object.keys(t).some((k) => k !== "role" && k !== "token" && k !== "consumer_id")) return null;
    if (typeof t.token !== "string" || !TOKEN.test(t.token) || seen.has(t.token)) return null;
    seen.add(t.token);
    if (t.role === "host") {
      if (host !== null || "consumer_id" in t) return null;
      host = t.token;
    } else if (t.role === "viewer") {
      if (typeof t.consumer_id !== "string" || !CONSUMER.test(t.consumer_id) || viewers.has(t.consumer_id)) return null;
      viewers.set(t.consumer_id, t.token);
    } else {
      return null;
    }
  }
  if (host === null || viewers.size === 0) return null;
  return { host, viewers };
}

// createHostCreds: call(token, path, signal) -> Promise<{ status, body }>
// does one GET; onChange(view) is told after every change.
export function createHostCreds({ call, onChange }) {
  let gen = 0;
  let abort = new AbortController();
  let held = null; // { host, viewers } of the current generation, once verified
  let mode = "none"; // none | loading | file | manual
  let fileName = "";
  let message = "";

  const view = () => ({ gen, mode, fileName, message, consumers: held ? [...held.viewers.keys()] : [], hasHost: !!held });
  const changed = () => onChange(view());

  // next ends every step of the previous generation and drops all tokens.
  function next(newMode, msg) {
    gen++;
    abort.abort();
    abort = new AbortController();
    held = null;
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
    try {
      const st = await call(parsed.host, "/api/host/status", signal);
      if (g !== gen) return;
      if (st.status !== 200) return fail("mismatch");
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
    mode = "file";
    fileName = typeof file.name === "string" ? file.name.slice(0, 80) : "";
    message = "已載入 token 檔：host 與 " + parsed.viewers.size + " 位 viewer";
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

  return {
    load, clearAll, manual, unauthorized, readiness,
    viewerToken: (consumer) => (held ? held.viewers.get(consumer) || null : null),
    get gen() { return gen; },
    view,
  };
}
