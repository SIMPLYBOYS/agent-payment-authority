// adapter.js: the Dashboard's pure logic, no DOM (docs/demo-ui.md section 3).
// Everything shown comes from two authenticated reads of the Store:
// GET /api/tasks/{id} and GET /api/tasks/{id}/events. Nothing here invents
// a decision, an expiry, a payment or a delivery.

export const UNKNOWN = "未知／未提供";

// The fixture tasks the demo registry gives each consumer. A choice in the
// page is not an authorization: the server decides by the token and the
// task's stored owner.
export const FIXTURE_TASKS = {
  "consumer-a": ["task-a", "task-a-next"],
  "consumer-b": ["task-b"],
};

// A Dashboard state. gen counts consumer switches and task changes: a reply
// started under an older gen is dropped, however late it arrives.
export function newState() {
  return { gen: 0, consumer: null, taskId: null, snapshot: null, status: "idle", lastGoodServedAt: null, seen: new Set() };
}

// reset forgets everything of the previous viewer or task.
export function reset(state, consumer = null, taskId = null) {
  state.gen += 1;
  state.consumer = consumer;
  state.taskId = taskId;
  state.snapshot = null;
  state.status = consumer && taskId ? "loading" : "idle";
  state.lastGoodServedAt = null;
  state.seen = new Set();
  return state.gen;
}

// consistent: the two reads describe the same committed state.
export function consistent(task, events) {
  return !!task && !!events && task.task_id === events.task_id && task.as_of_sequence === events.as_of_sequence;
}

// dedupe keeps one copy of each event, by event_id, in sequence order.
export function dedupe(events) {
  const byId = new Map();
  for (const e of events || []) {
    if (e && typeof e.event_id === "string" && !byId.has(e.event_id)) byId.set(e.event_id, e);
  }
  return [...byId.values()].sort((a, b) => a.sequence - b.sequence);
}

// commit takes a pair of replies into the state. It returns why it did not
// when it did not: "stale" (an older gen), "wrong_task", "inconsistent"
// (different as_of_sequence: read again), "older" (behind what is shown).
export function commit(state, gen, task, events) {
  if (gen !== state.gen) return "stale";
  if (!task || task.task_id !== state.taskId) return "wrong_task";
  if (!consistent(task, events)) return "inconsistent";
  if (state.snapshot && task.as_of_sequence < state.snapshot.task.as_of_sequence) return "older";
  state.snapshot = { task, events: dedupe(events.events) };
  state.status = "ok";
  state.lastGoodServedAt = task.served_at;
  return "ok";
}

// fail records a failed read without dropping the last good snapshot.
export function fail(state, gen, status) {
  if (gen !== state.gen) return "stale";
  state.status = status; // offline | updating | not_found | unauthenticated
  if (status === "not_found" || status === "unauthenticated") state.snapshot = null;
  return status;
}

const DECISION_PHASE = { Ask: "waiting", Deny: "denied", Allow: "allowed_unpaid" };

// scene maps the committed events to the shop scene. Every purchase event
// is tied to its own purchase_order_id: a closing (APPROVAL_EXPIRED,
// APPROVAL_WITHDRAWN) only closes its own purchase, so an old purchase's
// expiry never moves a newer one. The scene shows one purchase: the one with
// the latest POLICY_DECIDED (by sequence), else the latest proposed one; its
// verdict, reason, closing and deadline all come from that purchase. A Deny
// stays the scene's decision whatever ends the task later; an event without
// a purchase_order_id is never guessed into a purchase.
export function scene(snapshot) {
  if (!snapshot) return { phase: "empty", label: "尚無資料", notes: [], po: null, verdict: "", reason: null };
  const { task, events } = snapshot;
  const purchases = new Map(); // po -> { proposedSeq, decision, decidedSeq, closed }
  const of = (po) => {
    if (!purchases.has(po)) purchases.set(po, { proposedSeq: -1, decision: null, decidedSeq: -1, closed: null });
    return purchases.get(po);
  };
  let started = false;
  let end = null;
  let orphanDecision = false;
  for (const e of events) {
    const po = typeof e.purchase_order_id === "string" && e.purchase_order_id !== "" ? e.purchase_order_id : null;
    switch (e.event_type) {
      case "TASK_STARTED": started = true; break;
      case "PURCHASE_PROPOSED": if (po) of(po).proposedSeq = e.sequence; break;
      case "POLICY_DECIDED":
        if (po) { const p = of(po); p.decision = e.decision || null; p.decidedSeq = e.sequence; } else orphanDecision = true;
        break;
      case "APPROVAL_EXPIRED": if (po) of(po).closed = "expired"; break;
      case "APPROVAL_WITHDRAWN": if (po) of(po).closed = "withdrawn"; break;
      case "TASK_CANCELLED": end = "cancelled"; break;
      case "TASK_TIMED_OUT": end = "timed_out"; break;
    }
  }
  // The shown purchase: latest decision; else latest proposal.
  let shown = null;
  let best = -1;
  for (const [po, p] of purchases) if (p.decision && p.decidedSeq > best) { shown = po; best = p.decidedSeq; }
  if (!shown) for (const [po, p] of purchases) if (p.proposedSeq > best) { shown = po; best = p.proposedSeq; }
  const cur = shown ? purchases.get(shown) : null;
  let phase = started ? "entered" : "empty";
  if (cur && !cur.decision) phase = "quoted";
  if (cur && cur.decision) phase = DECISION_PHASE[cur.decision.policy_verdict] || "unknown";
  if (!cur && orphanDecision) phase = "unknown";
  const notes = [];
  if (phase === "waiting" && cur.closed === "expired") phase = "expired";
  if (phase === "waiting" && cur.closed === "withdrawn") phase = "withdrawn";
  if (phase === "waiting") {
    const p = (task.purchases || []).find((x) => x.purchase_order_id === shown);
    // The server's clock decides, never the browser's: past the deadline
    // with no closing event committed, only say so.
    if (p && p.deadline_at && !p.approval_closed && task.served_at >= p.deadline_at) notes.push("已達期限，等待後端確認");
  }
  if (phase === "allowed_unpaid") notes.push("M1 無付款能力");
  if (end === "cancelled") notes.push(phase === "denied" ? "之後 task 已取消（不改變原 Deny）" : "task 已取消");
  if (end === "timed_out") notes.push(phase === "denied" ? "之後 CLI 逾時（不改變原 Deny）" : "CLI 逾時");
  if (purchases.size > 1) notes.push("此 task 有 " + purchases.size + " 筆請購；場景顯示最近裁決的 " + shown);
  const labels = {
    empty: "尚無事件", entered: "進店", quoted: "取得報價", waiting: "Ask：等待人工核准（M1 無法核准）",
    denied: "Deny：離店", allowed_unpaid: "Allow：但 M1 無付款能力", expired: "Ask 已到期（後端已記錄）：離店",
    withdrawn: "Ask 已撤回（task 取消）：離店", unknown: "未知裁決",
  };
  const d = cur && cur.decision ? cur.decision : null;
  return { phase, label: labels[phase] || UNKNOWN, notes, po: shown, verdict: d ? d.policy_verdict || "" : "", reason: d ? d.reason || null : null };
}

// fresh returns the sequences not seen before and marks them seen: an event
// fetched again is never animated twice. On a first load everything is
// marked seen and nothing is returned (a stored snapshot, not news).
export function fresh(state, firstLoad) {
  const out = [];
  for (const e of state.snapshot ? state.snapshot.events : []) {
    if (!state.seen.has(e.event_id)) {
      state.seen.add(e.event_id);
      if (!firstLoad) out.push(e.sequence);
    }
  }
  return out;
}

// replay is the list of scene phases after each stored event, for a
// read-only historical replay. It writes nothing and changes no time.
export function replay(snapshot) {
  if (!snapshot) return [];
  return snapshot.events.map((_, i) => scene({ task: snapshot.task, events: snapshot.events.slice(0, i + 1) }).phase);
}

const str = (v) => (typeof v === "string" && v !== "" ? v : typeof v === "number" ? String(v) : UNKNOWN);

// amount keeps the integer string as stored; anything else is unknown.
export function amount(v) {
  return typeof v === "string" && /^[0-9]+$/.test(v) ? v : UNKNOWN;
}

// inspector rows: the task's stored identity and its purchases. Balances
// and remaining budget are not in the API, so they say so.
export function inspector(snapshot) {
  if (!snapshot) return [];
  const t = snapshot.task;
  const rows = [
    ["consumer", str(t.consumer_id)], ["agent", str(t.agent_id)], ["wallet", str(t.wallet_id)],
    ["wallet 證據", str(t.wallet_evidence_mode)], ["task", str(t.task_id)], ["payer", str(t.payer)],
    ["task 狀態", str(t.status)], ["快照序號", str(t.as_of_sequence)],
    ["fixture 餘額", UNKNOWN + "（M1 無鏈上餘額）"], ["剩餘預算", UNKNOWN + "（預算不是鏈上餘額）"],
    ["付款", "未付款（payment_capability：" + str(t.payment_capability) + "）"],
  ];
  for (const p of t.purchases || []) {
    const d = p.decision || {};
    rows.push(
      ["— 請購", str(p.purchase_order_id)], ["資源", str(p.http_method) + " " + str(p.resource_url)], ["payTo", str(p.pay_to)],
      ["amount（基本單位）", amount(p.amount)], ["asset", str(p.asset_id)], ["network（chain）", str(p.chain_id)],
      ["intent hash", str(p.intent_hash)], ["裁決", str(d.policy_verdict)], ["reason", str(d.reason)],
      ["approval", str(d.approval_status) + (p.approval_closed ? "，收尾：" + str(p.approval_closed) : "")],
      ["payment_status", str(d.payment_status)],
    );
  }
  return rows;
}

// timeline rows, one per event; json is the API's whitelisted event, not
// CLI output and not a raw log.
export function timeline(snapshot) {
  if (!snapshot) return [];
  return snapshot.events.map((e) => ({
    id: e.event_id,
    seq: e.sequence,
    type: str(e.event_type),
    time: typeof e.observed_at === "number" ? new Date(e.observed_at * 1000).toISOString() : UNKNOWN,
    detail: e.decision ? str(e.decision.policy_verdict) + " / " + str(e.decision.reason) : e.code ? str(e.code) : "",
    source: str(e.source) + " · " + str(e.evidence_mode),
    json: JSON.stringify(e, null, 2),
  }));
}

// ---- The Unity shop (docs/demo-ui.md section 4) ----
// The shop gets this projection and nothing else: no token, no API address,
// no free text beyond fixed codes. It shows; it never decides.
export const BRIDGE_VERSION = 1;

// projection is the message for the committed snapshot, or null.
export function projection(state, animate) {
  if (!state.snapshot) return null;
  const { task, events } = state.snapshot;
  const sc = scene(state.snapshot);
  const last = events.length ? events[events.length - 1].sequence : 0;
  return {
    v: BRIDGE_VERSION, gen: state.gen, clear: false, consumer: task.consumer_id, task_id: task.task_id,
    seq: Math.max(task.as_of_sequence, last), phase: sc.phase, verdict: sc.verdict, reason: sc.reason || "", animate: !!animate,
  };
}

export function clearMessage(gen) {
  return { v: BRIDGE_VERSION, gen, clear: true };
}

// bump starts a new generation for the same viewer and task, keeping the
// snapshot: the read-only historical replay uses it, so late replies and
// animation callbacks of the previous generation are dropped.
export function bump(state) {
  state.gen += 1;
  state.seen = new Set();
  return state.gen;
}
