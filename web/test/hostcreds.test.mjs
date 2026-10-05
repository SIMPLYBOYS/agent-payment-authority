// Tests of web/hostcreds.js (docs/demo-operation-design.md section 6.3 and
// the slice 3.1 conditions): the token file checks, and that no late step
// of an older generation (file read, verification, readiness, 401) commits
// tokens or clears newer ones. Run: node --test web/test/hostcreds.test.mjs
import { test } from "node:test";
import assert from "node:assert/strict";
import { parseTokenFile, createHostCreds, MAX_FILE_BYTES, MESSAGES } from "../hostcreds.js";

const tok = (c) => c.repeat(64);
const H = tok("a"), A = tok("b"), B = tok("c"), H2 = tok("d"), A2 = tok("e"), B2 = tok("f");
const OP = { [H]: tok("1"), [H2]: tok("2") }; // each start's operator token (slice 3.2)
const fileDoc = (host, a, b) => ({ note: "demo", tokens: [
  { role: "viewer", consumer_id: "consumer-a", token: a },
  { role: "viewer", consumer_id: "consumer-b", token: b },
  { role: "host", token: host },
  { role: "operator", token: OP[host] },
] });
const asFile = (doc, name = "demo-tokens.json") => {
  const text = typeof doc === "string" ? doc : JSON.stringify(doc);
  let reads = 0;
  return { name, size: text.length, text: async () => { reads++; return text; }, get reads() { return reads; } };
};

// A server that knows one set of tokens; its replies can be held.
function server(valid) {
  const held = [];
  let holdNext = 0;
  const calls = [];
  const runs = [];
  const answer = (token, path, post) => {
    const role = token === valid.host ? "host" : token === valid.operator ? "operator" : null;
    if (path === "/api/host/status") return role ? { status: 200, body: { caller_role: role, http: "ok" } } : { status: valid.all.has(token) ? 403 : 401, body: null };
    if (path === "/api/host/runs" && post) {
      if (role !== "operator") return { status: valid.all.has(token) ? 403 : 401, body: null };
      const known = runs.find((r) => r.request_id === post.body.request_id);
      if (known) return { status: 200, body: known };
      const run = { run_id: "run-" + runs.length, request_id: post.body.request_id, scenario: post.body.scenario, task_id: "show-a-0" + (runs.length + 1), consumer_id: "consumer-a", state: "accepted", live: "running" };
      runs.push(run);
      return { status: 202, body: run };
    }
    if (path === "/api/host/runs") return role ? { status: 200, body: { runs } } : { status: valid.all.has(token) ? 403 : 401, body: null };
    if (path.endsWith("/stop") && post) return role === "operator" ? { status: 202, body: { live: "stopping" } } : { status: valid.all.has(token) ? 403 : 401, body: null };
    if (path === "/api/viewer") {
      const c = valid.viewers.get(token);
      return c ? { status: 200, body: { role: "viewer", consumer_id: c } } : { status: token === valid.host ? 403 : 401, body: null };
    }
    return { status: 404, body: null };
  };
  let failNext = 0; // network errors (a rejected call) for the next n calls
  const call = (token, path, signal, post) => {
    calls.push((post ? "POST " : "") + path);
    if (failNext > 0) {
      failNext--;
      return Promise.reject(new TypeError("network"));
    }
    const r = answer(token, path, post);
    if (holdNext > 0) {
      holdNext--;
      return new Promise((res) => held.push(() => res(r)));
    }
    return Promise.resolve(r);
  };
  return { call, calls, runs, hold: (n = 1) => { holdNext += n; }, fail: (n = 1) => { failNext += n; }, releaseAll: async () => { while (held.length) held.shift()(); await flush(); } };
}
const valid = (host, a, b) => ({ host, operator: OP[host], viewers: new Map([[a, "consumer-a"], [b, "consumer-b"]]), all: new Set([host, OP[host], a, b]) });
const flush = () => new Promise((r) => setTimeout(r, 0));

let ids = 0;
function setup(srv) {
  const views = [];
  const creds = createHostCreds({ call: srv.call, onChange: (v) => views.push(v), newId: () => "req-test-" + String(++ids).padStart(8, "0"), wait: () => Promise.resolve() });
  return { creds, views };
}
const holds = (creds) => ({ mode: creds.view().mode, host: creds.view().hasHost, a: creds.viewerToken("consumer-a"), b: creds.viewerToken("consumer-b") });

test("parseTokenFile accepts the host's token file", () => {
  const p = parseTokenFile(JSON.stringify(fileDoc(H, A, B)));
  assert.equal(p.host, H);
  assert.equal(p.operator, OP[H]);
  assert.deepEqual([...p.viewers], [["consumer-a", A], ["consumer-b", B]]);
});

test("parseTokenFile refuses anything else", () => {
  const bad = {
    "not JSON": "{",
    "an array": "[]",
    "an extra top-level key": { ...fileDoc(H, A, B), extra: 1 },
    "a note that is not a string": { ...fileDoc(H, A, B), note: 1 },
    "no tokens": { note: "x" },
    "two operators": { tokens: [...fileDoc(H, A, B).tokens, { role: "operator", token: tok("9") }] },
    "no operator (slice 3.2 needs one)": { tokens: fileDoc(H, A, B).tokens.filter((t) => t.role !== "operator") },
    "an operator with a consumer": { tokens: [...fileDoc(H, A, B).tokens.filter((t) => t.role !== "operator"), { role: "operator", token: OP[H], consumer_id: "consumer-a" }] },
    "an unknown role": { tokens: [{ role: "admin", token: H }, { role: "viewer", consumer_id: "consumer-a", token: A }] },
    "a short token": fileDoc("abc", A, B),
    "an upper-case token": fileDoc(H.toUpperCase(), A, B),
    "a repeated token": fileDoc(H, A, A),
    "a host with a consumer": { tokens: [{ role: "host", token: H, consumer_id: "consumer-a" }, { role: "viewer", consumer_id: "consumer-a", token: A }] },
    "two hosts": { tokens: [...fileDoc(H, A, B).tokens, { role: "host", token: H2 }] },
    "no host": { tokens: fileDoc(H, A, B).tokens.slice(0, 2) },
    "no viewer": { tokens: [{ role: "host", token: H }] },
    "a bad consumer id": { tokens: [{ role: "host", token: H }, { role: "viewer", consumer_id: "a b", token: A }] },
    "a repeated consumer": { tokens: [{ role: "host", token: H }, { role: "viewer", consumer_id: "consumer-a", token: A }, { role: "viewer", consumer_id: "consumer-a", token: B }] },
    "an extra key in an entry": { tokens: [{ role: "host", token: H, x: 1 }, { role: "viewer", consumer_id: "consumer-a", token: A }] },
  };
  for (const [name, doc] of Object.entries(bad)) assert.equal(parseTokenFile(typeof doc === "string" ? doc : JSON.stringify(doc)), null, name);
});

test("load: a valid file is kept only after the server confirms every token", async () => {
  const srv = server(valid(H, A, B));
  const { creds } = setup(srv);
  await creds.load(asFile(fileDoc(H, A, B)));
  assert.deepEqual(holds(creds), { mode: "file", host: true, a: A, b: B });
  assert.deepEqual(srv.calls, ["/api/host/status", "/api/host/status", "/api/viewer", "/api/viewer"]);
  assert.deepEqual(creds.view().consumers, ["consumer-a", "consumer-b"]);
});

test("load: too large is not read; bad, stale or swapped files keep nothing", async () => {
  const srv = server(valid(H, A, B));
  const { creds } = setup(srv);
  const big = asFile(fileDoc(H, A, B));
  Object.defineProperty(big, "size", { value: MAX_FILE_BYTES + 1 });
  await creds.load(big);
  assert.equal(big.reads, 0);
  assert.equal(creds.view().message, MESSAGES.too_large);
  for (const [name, f, msg] of [
    ["bad format", asFile("{not json"), MESSAGES.bad_file],
    ["tokens of a previous start", asFile(fileDoc(H2, A2, B2)), MESSAGES.mismatch],
    ["viewers swapped", asFile({ tokens: [{ role: "viewer", consumer_id: "consumer-a", token: B }, { role: "viewer", consumer_id: "consumer-b", token: A }, { role: "host", token: H }, { role: "operator", token: OP[H] }] }), MESSAGES.mismatch],
    ["host given as a viewer", asFile({ tokens: [{ role: "viewer", consumer_id: "consumer-a", token: H }, { role: "host", token: A }, { role: "operator", token: OP[H] }] }), MESSAGES.mismatch],
    ["host and operator swapped", asFile({ tokens: [{ role: "viewer", consumer_id: "consumer-a", token: A }, { role: "viewer", consumer_id: "consumer-b", token: B }, { role: "host", token: OP[H] }, { role: "operator", token: H }] }), MESSAGES.mismatch],
  ]) {
    await creds.load(f);
    assert.deepEqual(holds(creds), { mode: "none", host: false, a: null, b: null }, name);
    assert.equal(creds.view().message, msg, name);
    for (const t of [H, A, B, H2, A2, B2]) assert.ok(!creds.view().message.includes(t), name);
  }
});

test("A still loading, then 「清除全部憑證」: A's late success restores nothing", async () => {
  const srv = server(valid(H, A, B));
  const { creds, views } = setup(srv);
  srv.hold();
  const a = creds.load(asFile(fileDoc(H, A, B)));
  await flush();
  creds.clearAll("cleared");
  await srv.releaseAll();
  await a;
  assert.deepEqual(holds(creds), { mode: "none", host: false, a: null, b: null });
  assert.equal(views.at(-1).message, "cleared");
  assert.equal(views.filter((v) => v.mode === "file").length, 0);
});

test("A's file read still pending, then cleared: the read commits nothing", async () => {
  const srv = server(valid(H, A, B));
  const { creds } = setup(srv);
  let release;
  const slow = { name: "slow.json", size: 10, text: () => new Promise((r) => { release = () => r(JSON.stringify(fileDoc(H, A, B))); }) };
  const a = creds.load(slow);
  creds.clearAll("cleared");
  release();
  await a;
  await flush();
  assert.deepEqual(holds(creds), { mode: "none", host: false, a: null, b: null });
  assert.deepEqual(srv.calls, []); // nothing was even verified
});

test("A and B interleaved: A's late success does not replace B", async () => {
  const srv = server({ host: H, operator: OP[H], viewers: new Map([[A, "consumer-a"], [B, "consumer-b"], [A2, "consumer-a"], [B2, "consumer-b"]]), all: new Set([H, OP[H], A, B, A2, B2]) });
  const { creds } = setup(srv);
  srv.hold();
  const a = creds.load(asFile(fileDoc(H, A, B), "a.json"));
  await flush();
  await creds.load(asFile(fileDoc(H, A2, B2), "b.json"));
  await srv.releaseAll();
  await a;
  assert.deepEqual(holds(creds), { mode: "file", host: true, a: A2, b: B2 });
  assert.equal(creds.view().fileName, "b.json");
});

test("A and B interleaved: A's late failure does not clear B", async () => {
  const srv = server(valid(H, A, B));
  const { creds } = setup(srv);
  srv.hold();
  const a = creds.load(asFile(fileDoc(H2, A2, B2), "old.json")); // a previous start's tokens: 401
  await flush();
  await creds.load(asFile(fileDoc(H, A, B), "new.json"));
  await srv.releaseAll();
  await a;
  assert.deepEqual(holds(creds), { mode: "file", host: true, a: A, b: B });
  assert.equal(creds.view().message.startsWith("已載入"), true);
});

test("B loaded, then a 401 of A's generation: B's credentials stay", async () => {
  const srv = server(valid(H, A, B));
  const { creds } = setup(srv);
  await creds.load(asFile(fileDoc(H, A, B)));
  const oldGen = creds.gen;
  await creds.load(asFile(fileDoc(H, A, B), "b.json"));
  assert.equal(creds.unauthorized(oldGen), false);
  assert.deepEqual(holds(creds), { mode: "file", host: true, a: A, b: B });
  assert.equal(creds.unauthorized(creds.gen), true); // a 401 of the current generation does clear
  assert.deepEqual(holds(creds), { mode: "none", host: false, a: null, b: null });
  assert.equal(creds.view().message, MESSAGES.stale);
});

test("A's verification finishes after the switch to manual viewer mode: nothing of A is kept", async () => {
  const srv = server(valid(H, A, B));
  const { creds } = setup(srv);
  srv.hold();
  const a = creds.load(asFile(fileDoc(H, A, B)));
  await flush();
  creds.manual();
  await srv.releaseAll();
  await a;
  assert.deepEqual(holds(creds), { mode: "manual", host: false, a: null, b: null });
});

test("a readiness poll of an older generation neither shows nor clears anything", async () => {
  const srv = server(valid(H, A, B));
  const { creds } = setup(srv);
  await creds.load(asFile(fileDoc(H, A, B)));
  srv.hold();
  const poll = creds.readiness();
  await flush();
  await creds.load(asFile(fileDoc(H, A, B), "again.json"));
  await srv.releaseAll();
  assert.equal(await poll, null);
  assert.deepEqual(holds(creds), { mode: "file", host: true, a: A, b: B });
  assert.deepEqual(await creds.readiness(), { status: { caller_role: "host", http: "ok" } });
});

test("a readiness 401 of the current generation clears all (the host restarted)", async () => {
  const live = valid(H, A, B);
  const srv = server(live);
  const { creds } = setup(srv);
  await creds.load(asFile(fileDoc(H, A, B)));
  live.host = H2; live.all = new Set([H2]); // the host restarted: new tokens, the old ones unknown
  assert.equal(await creds.readiness(), null);
  assert.deepEqual(holds(creds), { mode: "none", host: false, a: null, b: null });
  assert.equal(creds.view().message, MESSAGES.stale);
});

test("pagehide (clearAll): a verification in flight commits nothing afterwards", async () => {
  const srv = server(valid(H, A, B));
  const { creds } = setup(srv);
  srv.hold();
  const a = creds.load(asFile(fileDoc(H, A, B)));
  await flush();
  creds.clearAll("left");
  await srv.releaseAll();
  await a;
  assert.deepEqual(holds(creds), { mode: "none", host: false, a: null, b: null });
  assert.equal(await creds.readiness(), null);
});

// ---- slice 3.2: control requests keep the generation rule (section 13.8 R4) ----

test("startRun: a reply of the current generation counts; a resend reuses the request ID", async () => {
  const srv = server(valid(H, A, B));
  const { creds } = setup(srv);
  await creds.load(asFile(fileDoc(H, A, B)));
  srv.fail(1); // the first attempt is lost on the network
  const r = await creds.startRun("ask");
  assert.equal(r.gen, creds.gen);
  assert.equal(r.status, 202);
  assert.equal(srv.runs.length, 1, "one run, although the request was sent twice");
  assert.equal(srv.runs[0].request_id, r.requestId);
  assert.deepEqual(srv.calls.filter((c) => c.startsWith("POST")), ["POST /api/host/runs", "POST /api/host/runs"]);
});

test("startRun sent, then 清除全部, then the reply: dropped, nothing resent", async () => {
  const srv = server(valid(H, A, B));
  const { creds } = setup(srv);
  await creds.load(asFile(fileDoc(H, A, B)));
  srv.hold();
  const p = creds.startRun("ask");
  await flush();
  creds.clearAll("cleared");
  await srv.releaseAll();
  assert.equal(await p, null, "the reply of the old generation is dropped");
  assert.deepEqual(holds(creds), { mode: "none", host: false, a: null, b: null });
  assert.equal(srv.runs.length, 1, "the server's accepted run stands: clearing does not withdraw it");
  assert.equal(srv.calls.filter((c) => c.startsWith("POST")).length, 1, "and it is not resent as a new run");
});

test("startRun sent, then file B loaded, then the old reply: B unchanged", async () => {
  const srv = server(valid(H, A, B));
  const { creds } = setup(srv);
  await creds.load(asFile(fileDoc(H, A, B), "a.json"));
  srv.hold();
  const p = creds.startRun("ask");
  await flush();
  await creds.load(asFile(fileDoc(H, A, B), "b.json"));
  const before = creds.view();
  await srv.releaseAll();
  assert.equal(await p, null);
  assert.deepEqual(creds.view(), before);
});

test("a control request's 401 of an older generation does not clear newer credentials", async () => {
  const live = valid(H, A, B);
  const srv = server(live);
  const { creds } = setup(srv);
  await creds.load(asFile(fileDoc(H, A, B)));
  live.operator = tok("7"); live.all = new Set([H, tok("7"), A, B]); // the old operator token is now unknown: 401
  srv.hold();
  const p = creds.startRun("ask");
  await flush();
  live.operator = OP[H]; live.all = new Set([H, OP[H], A, B]);
  await creds.load(asFile(fileDoc(H, A, B), "again.json"));
  await srv.releaseAll();
  assert.equal(await p, null);
  assert.equal(creds.view().mode, "file", "the stale 401 cleared nothing");
});

test("a generation change during resends stops them and makes no new request ID", async () => {
  const srv = server(valid(H, A, B));
  let waited;
  const views = [];
  const made = [];
  const creds = createHostCreds({ call: srv.call, onChange: (v) => views.push(v), newId: () => { const id = "req-gen-" + String(made.length).padStart(8, "0"); made.push(id); return id; },
    wait: () => new Promise((r) => { waited = r; }) });
  await creds.load(asFile(fileDoc(H, A, B)));
  srv.fail(1);
  const p = creds.startRun("ask");
  await flush();
  creds.clearAll("cleared"); // during the pause before the resend
  waited();
  assert.equal(await p, null);
  assert.equal(made.length, 1);
  assert.equal(srv.calls.filter((c) => c.startsWith("POST")).length, 1, "no resend after the generation changed");
});

test("stopRun and runs follow the same generation rule", async () => {
  const srv = server(valid(H, A, B));
  const { creds } = setup(srv);
  await creds.load(asFile(fileDoc(H, A, B)));
  const started = await creds.startRun("ask");
  const list = await creds.runs();
  assert.equal(list.runs.length, 1);
  srv.hold();
  const p = creds.stopRun(started.body.run_id);
  await flush();
  creds.clearAll("cleared");
  await srv.releaseAll();
  assert.equal(await p, null);
  assert.equal(await creds.runs(), null, "no credentials, no request");
});
