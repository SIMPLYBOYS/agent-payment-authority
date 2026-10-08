// adapter.js: the Dashboard's pure logic, no DOM (docs/demo-ui.md section 3).
// Everything shown comes from two authenticated reads of the Store:
// GET /api/tasks/{id} and GET /api/tasks/{id}/events. Nothing here invents
// a decision, an expiry, a payment or a delivery.

export const UNKNOWN = "未知／未提供";

// Task status codes as the task list shows them (GET /api/tasks, section 14
// of docs/demo-operation-design.md); an unknown code is shown as it is.
export const STATUS_LABELS = {
  created: "已建立", running: "執行中", awaiting_approval: "等待核准", denied: "Deny", cancelled: "已取消", failed: "CLI 逾時",
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
// a purchase_order_id is never guessed into a purchase. cause is the
// sequence of the event that set the phase (D2, 16.4), 0 when none; end is
// the first TASK_CANCELLED or TASK_TIMED_OUT ("" before one).
export function scene(snapshot) {
  if (!snapshot) return { phase: "empty", label: "尚無資料", notes: [], po: null, verdict: "", reason: null, cause: 0, end: "" };
  const { task, events } = snapshot;
  const purchases = new Map(); // po -> { proposedSeq, decision, decidedSeq, closed, closedSeq }
  const of = (po) => {
    if (!purchases.has(po)) purchases.set(po, { proposedSeq: -1, decision: null, decidedSeq: -1, closed: null, closedSeq: -1 });
    return purchases.get(po);
  };
  let started = false;
  let startedSeq = 0;
  let end = null;
  let firstEnd = "";
  let orphanDecision = false;
  for (const e of events) {
    const po = typeof e.purchase_order_id === "string" && e.purchase_order_id !== "" ? e.purchase_order_id : null;
    switch (e.event_type) {
      case "TASK_STARTED": if (!started) startedSeq = e.sequence; started = true; break;
      case "PURCHASE_PROPOSED": if (po) of(po).proposedSeq = e.sequence; break;
      case "POLICY_DECIDED":
        if (po) { const p = of(po); p.decision = e.decision || null; p.decidedSeq = e.sequence; } else orphanDecision = true;
        break;
      case "APPROVAL_EXPIRED": if (po) Object.assign(of(po), { closed: "expired", closedSeq: e.sequence }); break;
      case "APPROVAL_WITHDRAWN": if (po) Object.assign(of(po), { closed: "withdrawn", closedSeq: e.sequence }); break;
      case "TASK_CANCELLED": end = "cancelled"; firstEnd = firstEnd || end; break;
      case "TASK_TIMED_OUT": end = "timed_out"; firstEnd = firstEnd || end; break;
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
  const cause = { entered: startedSeq, quoted: cur && cur.proposedSeq, waiting: cur && cur.decidedSeq, denied: cur && cur.decidedSeq,
    allowed_unpaid: cur && cur.decidedSeq, expired: cur && cur.closedSeq, withdrawn: cur && cur.closedSeq }[phase];
  return { phase, label: labels[phase] || UNKNOWN, notes, po: shown, verdict: d ? d.policy_verdict || "" : "", reason: d ? d.reason || null : null,
    cause: Number.isInteger(cause) && cause > 0 ? cause : 0, end: firstEnd };
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

// projection is the message for the committed snapshot (or a replay step
// shaped as one, stepView), or null. cause_seq and task_end are D2's two
// optional fields (16.4): cause_seq is sent only within 0 < cause_seq <= seq,
// else 0 (unknown).
export function projection(state, animate) {
  if (!state.snapshot) return null;
  const { task, events } = state.snapshot;
  const sc = scene(state.snapshot);
  const last = events.length ? events[events.length - 1].sequence : 0;
  const seq = Math.max(task.as_of_sequence, last);
  return {
    v: BRIDGE_VERSION, gen: state.gen, clear: false, consumer: task.consumer_id, task_id: task.task_id,
    seq, phase: sc.phase, verdict: sc.verdict, reason: sc.reason || "", animate: !!animate,
    cause_seq: sc.cause > 0 && sc.cause <= seq ? sc.cause : 0, task_end: sc.end,
  };
}

// sameShown: whether Unity's report names exactly the projection last sent:
// the same generation, task, sequence and phase. Anything else (another
// task or phase, an older or a later sequence) confirms nothing (review of
// d7070af; the same rule as the evidence panel's sync, review of 7ffc45d).
export const sameShown = (sent, rep) => !!sent && !!rep && rep.gen === sent.gen && rep.task === sent.task && rep.seq === sent.seq && rep.phase === sent.phase;

// ---- D2: the shop's transition table (docs/demo-presentation-design.md 16.4) ----
// The same table as Unity's SceneRules (Projection.cs); both are tested with
// testdata/demo/scene-transitions.json. The page uses it only to explain the
// move in words; Unity decides its own moves from the message.
const COUNTER = new Set(["waiting", "denied", "allowed_unpaid"]);
const labelKind = (phase, end) => (phase === "waiting" && end === "timed_out" ? "waiting/timed_out" : phase);
const fixedCode = (r) => (typeof r === "string" && /^[A-Z0-9_]{1,40}$/.test(r) ? r : "");

// shopLabel is the customer's ASCII label in Unity: #n is the cause, never
// the snapshot sequence; an unknown cause (0) shows no number.
export function shopLabel(phase, cause, end, reason) {
  const n = cause > 0 ? " (#" + cause + ")" : "";
  switch (phase) {
    case "empty": return "";
    case "entered": return "entered" + n;
    case "quoted": return "request proposed" + n;
    case "waiting": return end === "timed_out" ? "CLI timeout - ask still pending" : "ASK" + n + " - waiting for a human";
    case "denied": {
      const why = [cause > 0 ? "#" + cause : "", fixedCode(reason)].filter(Boolean).join(", ");
      return "DENY" + (why ? " (" + why + ")" : "") + " - leaving";
    }
    case "allowed_unpaid": return "ALLOW - no payment capability";
    case "expired": return "ask expired" + n + " - leaving";
    case "withdrawn": return "ask withdrawn" + n + " - leaving";
    default: return "unknown state" + n;
  }
}

const BASIS = { entered: "進店事件", quoted: "請購", waiting: "裁決", denied: "裁決", allowed_unpaid: "裁決", expired: "到期", withdrawn: "撤回" };

// transition compares a projection m with the target t the shop was last
// given (null: nothing yet), first match wins: 1 another phase; 2 the same
// counter phase with both causes valid and different (a new purchase);
// 3 only the label kind changed; 4 the same place. browse: the move plays
// the "[anim] browsing" segment. text says it in words for the page.
export function transition(t, m) {
  t = t || { phase: "empty", cause: 0, end: "" };
  const phase = m.phase;
  const cause = Number.isInteger(m.cause_seq) && m.cause_seq > 0 && m.cause_seq <= m.seq ? m.cause_seq : 0;
  const end = m.task_end === "cancelled" || m.task_end === "timed_out" ? m.task_end : "";
  const newPurchase = t.cause > 0 && cause > 0 && t.cause !== cause;
  const rule = t.phase !== phase ? 1 : COUNTER.has(phase) && newPurchase ? 2 : labelKind(t.phase, t.end) !== labelKind(phase, end) ? 3 : 4;
  const browse = rule === 2 || (rule === 1 && (phase === "quoted" || (COUNTER.has(phase) && t.phase !== "quoted" && (!COUNTER.has(t.phase) || newPurchase))));
  const target = { phase, cause, end, seq: m.seq, label: shopLabel(phase, cause, end, m.reason) };
  let text;
  if (rule === 3) text = "不移動，只換標籤" + (labelKind(phase, end) === "waiting/timed_out" ? "：CLI 逾時，同一筆 Ask 仍未決" : "");
  else if (rule === 4) text = "不移動、不重播：場景的請購未變（快照 #" + m.seq + "）";
  else {
    const anim = [...(browse ? ["選商品"] : []), ...(COUNTER.has(phase) ? ["到櫃檯"] : [])];
    const parts = [...(anim.length ? ["展示動畫：" + anim.join(" → ") + "（沒有獨立事件）"] : []),
      ...(phase === "entered" ? ["進店"] : []), ...(["denied", "expired", "withdrawn"].includes(phase) ? ["離店"] : [])];
    const basis = BASIS[phase] ? BASIS[phase] + (cause ? "已在 #" + cause + " 記錄" : "已記錄（序號未知）") : "未知狀態";
    text = (rule === 2 ? "新的一筆請購：" : "") + (parts.length ? parts.join(" → ") + "；" : "") + basis;
  }
  return { rule, browse, target, text };
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

// ---- D1: the purchase card, the replay step and the evidence panel ----
// (docs/demo-presentation-design.md sections 4.4, 4.5, 6.2, 7). Every
// value comes from the snapshot, the replayed events or the shop's catalog,
// and says where it came from; nothing is inferred from time or animation.

// project derives a task's status from its events, by the Store's own rule
// (internal/store project): the first ending event decides, a Deny keeps
// its reason, an open Ask means awaiting_approval.
export function project(events) {
  const open = new Set();
  for (const e of events) {
    const po = e.purchase_order_id || "";
    switch (e.event_type) {
      case "POLICY_DECIDED": {
        const v = e.decision && e.decision.policy_verdict;
        if (v === "Deny") return { status: "denied", reason: (e.decision && e.decision.reason) || "" };
        if (v === "Ask") open.add(po);
        break;
      }
      case "APPROVAL_EXPIRED": case "APPROVAL_WITHDRAWN": open.delete(po); break;
      case "TASK_CANCELLED": return { status: "cancelled", reason: "" };
      case "TASK_TIMED_OUT": return { status: "failed", reason: "" };
    }
  }
  return { status: open.size ? "awaiting_approval" : "running", reason: "" };
}

// The purchase fields fixed when it was proposed (the intent is pinned to
// its first PURCHASE_PROPOSED): safe to show at any replay step after it.
const FIXED = ["purchase_order_id", "request_id", "http_method", "resource_url", "chain_id", "asset_contract", "asset_id", "pay_to", "amount", "intent_hash", "payment_requirement_hash"];

// stepView is the replay step n of a stored snapshot, shaped as a snapshot:
// only events with sequence <= n; a purchase only once proposed, with its
// fixed fields; its verdict from the replayed POLICY_DECIDED event and its
// closing from the replayed closing event, never from the final record's
// decision or approval_closed; a deadline only once its Ask is decided. The
// task status is derived from those events (derived: true). There is no
// served_at, so nothing compares a past deadline with the present.
export function stepView(snapshot, n) {
  if (!snapshot) return null;
  const { task, events } = snapshot;
  const prefix = events.filter((e) => e.sequence <= n);
  const stored = new Map((task.purchases || []).map((p) => [p.purchase_order_id, p]));
  const shown = new Map();
  for (const e of prefix) {
    const po = typeof e.purchase_order_id === "string" ? e.purchase_order_id : "";
    if (e.event_type === "PURCHASE_PROPOSED" && stored.has(po) && !shown.has(po)) {
      const p = {};
      for (const k of FIXED) if (stored.get(po)[k] !== undefined) p[k] = stored.get(po)[k];
      shown.set(po, p);
      continue;
    }
    const p = shown.get(po);
    if (!p) continue;
    if (e.event_type === "POLICY_DECIDED" && e.decision) {
      p.decision = e.decision;
      p.decided_at = e.observed_at;
      if (e.decision.policy_verdict === "Ask") p.deadline_at = stored.get(po).deadline_at;
    }
    if (e.event_type === "APPROVAL_EXPIRED") { p.approval_closed = "expired"; p.closed_at = e.observed_at; }
    if (e.event_type === "APPROVAL_WITHDRAWN") { p.approval_closed = "withdrawn"; p.closed_at = e.observed_at; }
  }
  const pr = project(prefix);
  return {
    task: {
      task_id: task.task_id, consumer_id: task.consumer_id, agent_id: task.agent_id, wallet_id: task.wallet_id, payer: task.payer,
      wallet_evidence_mode: task.wallet_evidence_mode, payment_capability: task.payment_capability,
      status: pr.status, deny_reason: pr.reason, as_of_sequence: n, purchases: [...shown.values()], derived: true,
    },
    events: prefix,
  };
}

// Plain words for the fixed reason codes; an unknown code is shown as it is.
export const REASONS = {
  HUMAN_APPROVAL_REQUIRED: "需要人工核准（M1 無核准能力）", INSUFFICIENT_BUDGET: "預算不足", APPROVAL_TIMEOUT: "核准期限已到",
  HOST_CANCELLED: "主持人取消", PAYMENT_CAPABILITY_UNAVAILABLE: "M1 無付款能力", APPROVER_UNAVAILABLE: "沒有核准人",
  CLI_TIMEOUT: "CLI 逾時",
};
const reasonText = (r) => (typeof r === "string" && r !== "" ? r + (REASONS[r] ? "（" + REASONS[r] + "）" : "") : UNKNOWN);
const hhmmss = (s) => (typeof s === "number" ? new Date(s * 1000).toISOString().slice(11, 19) + " UTC" : UNKNOWN);
const CUSTOMER = { "consumer-a": "A", "consumer-b": "B" };

// catalogFor finds the shop catalog's entry for a purchase's resource.
function catalogFor(catalog, p) {
  const list = catalog && Array.isArray(catalog.products) ? catalog.products : [];
  return list.find((x) => x && p && x.resource_url === p.resource_url && x.price) || null;
}

function purchaseStatus(p, live, servedAt) {
  if (!p) return "此 task 尚無請購";
  const d = p.decision;
  if (!d) return "已提出請購，尚無裁決";
  if (d.policy_verdict === "Deny") return "Deny：" + reasonText(d.reason);
  if (d.policy_verdict === "Allow") return "Allow：政策允許，M1 未付款（PAYMENT_CAPABILITY_UNAVAILABLE）";
  if (d.policy_verdict !== "Ask") return "未知裁決：" + str(d.policy_verdict);
  if (p.approval_closed === "expired") return "Ask 已到期（後端已記錄）";
  if (p.approval_closed === "withdrawn") return "Ask 已撤回（task 取消）";
  const reached = live && typeof servedAt === "number" && typeof p.deadline_at === "number" && servedAt >= p.deadline_at;
  return "Ask：等待人工核准（M1 無核准能力）" + (reached ? "；已達期限，等待後端確認" : "");
}

// card is the purchase card's model for a snapshot (live) or a stepView
// (replay). chosen is a purchase picked by hand (live only; null follows
// the scene). ctx: { mode: "live"|"replay", n, m, catalog, catalogAt,
// history }: history is the host's history mode (a recorded real run,
// docs/cli-poc.md 54), null otherwise.
export function card(view, chosen, ctx) {
  if (!view) return null;
  const live = ctx.mode !== "replay";
  const hist = ctx.history || null;
  const t = view.task;
  const sc = scene(view);
  const purchases = t.purchases || [];
  const ids = purchases.map((p) => p.purchase_order_id);
  const po = live && chosen && ids.includes(chosen) ? chosen : sc.po;
  const p = purchases.find((x) => x.purchase_order_id === po) || null;
  const evs = view.events;
  const at = (type) => evs.find((e) => e.event_type === type && (e.purchase_order_id || "") === (po || "-"));
  const ended = evs.find((e) => e.event_type === "TASK_CANCELLED" || e.event_type === "TASK_TIMED_OUT");
  const later = live ? "尚未發生" : "尚未發生（回放位置 #" + ctx.n + "）";
  const step = (label, ev, note) => ({ label, state: ev ? "done" : "future", note: ev ? "#" + ev.sequence + (note ? " " + note : "") : later });
  const closing = p && (at("APPROVAL_EXPIRED") || at("APPROVAL_WITHDRAWN"));
  const waiting = p && p.decision && p.decision.policy_verdict === "Ask" && !p.approval_closed;
  const steps = [
    step("進店", evs.find((e) => e.event_type === "TASK_STARTED"), "TASK_STARTED"),
    { label: "選商品", state: "none", note: hist ? "無 Store 事件；工具呼叫見查證面板（真實執行 trace）"
      : ctx.live ? "無 Store 事件；工具呼叫在 CLI 結束後見查證面板（真實 Agent）" : "無獨立事件（fake 腳本）" },
    { label: "報價", state: "none", note: (hist ? "無 Store 事件；工具呼叫見查證面板（真實執行 trace）；"
      : ctx.live ? "無 Store 事件；工具呼叫在 CLI 結束後見查證面板（真實 Agent）；" : "無獨立事件；") + "402 以 hash 綁定在請購" },
    step("請購", p && at("PURCHASE_PROPOSED"), "PURCHASE_PROPOSED"),
    step("裁決", p && at("POLICY_DECIDED"), p && p.decision ? str(p.decision.policy_verdict) : ""),
    closing || ended ? step("收尾", closing || ended, (closing || ended).event_type)
      : waiting ? { label: "收尾", state: "wait", note: hist ? "執行結束時仍待核准（歷史快照，不是即時狀態；期限已過）" : "等待中（M1 無核准能力）" }
      : p && p.decision && p.decision.policy_verdict === "Deny" ? { label: "收尾", state: "none", note: "Deny 即結束；沒有收尾事件" }
      : p && p.decision && p.decision.policy_verdict === "Allow" ? { label: "收尾", state: "none", note: "M1 無付款；沒有後續事件" }
      : step("收尾", null),
  ];
  const cat = catalogFor(ctx.catalog, p);
  const rows = [];
  if (p) {
    rows.push(["402 綁定金額", amount(p.amount) + "（基本單位，" + str(p.asset_id) + "）", "Store"]);
    rows.push(["目錄標示", cat ? str(cat.price.asset_display) + "；" + str(cat.price.fiat_amount) + " " + str(cat.price.fiat_currency) + "（FX " + str(cat.price.fx_rate) + "，" + str(cat.price.fx_source) + "）" : "目錄未取得",
      "商店 HTTP /catalog（fixture，不綁定此請購" + (ctx.catalogAt ? "；瀏覽器讀取 " + ctx.catalogAt : "") + "）"]);
    if (p.decision) {
      rows.push(["裁決", str(p.decision.policy_verdict), live ? "Store" : "Store 事件（回放）"]);
      rows.push(["原因", reasonText(p.decision.reason), live ? "Store" : "Store 事件（回放）"]);
    }
    if (typeof p.deadline_at === "number") rows.push(["期限", hhmmss(p.deadline_at), hist ? "Store 時間；當時的期限，早已過，不是倒數" : "Store 時間；裁決時決定"]);
    rows.push(["付款", "未付款（payment_status：" + str(p.decision ? p.decision.payment_status : "unavailable") + "）", "Store"]);
  }
  return {
    mode: live ? "live" : "replay", n: ctx.n, m: ctx.m,
    title: "客人 " + (CUSTOMER[t.consumer_id] || str(t.consumer_id)) + " · " + str(t.consumer_id) + " · " + str(t.task_id),
    po: po || null, scenePo: sc.po || null, differs: !!(po && sc.po && po !== sc.po), options: live ? ids : [],
    steps, status: hist && p && p.decision && p.decision.policy_verdict === "Ask" && !p.approval_closed
      ? "Ask：執行結束時待人工核准（歷史快照；不是仍有效的核准）" : purchaseStatus(p, live, t.served_at),
    history: hist ? hist.label : null,
    live: ctx.live ? ctx.live.label : null,
    task: "task 狀態：" + (STATUS_LABELS[t.status] || str(t.status)) + (t.deny_reason ? "（" + t.deny_reason + "）" : "") + (t.derived ? "（依已回放事件推導，網頁）" : "（Store）")
      + (ended ? "；" + (ended.event_type === "TASK_CANCELLED" ? "task 已取消" : "CLI 逾時") : ""),
    rows,
  };
}

// liveTools is the evidence panel's tool-call section in live mode
// (docs/cli-poc.md 55), from GET /api/tasks/{id}/trace: the buyer host
// writes the trace when the CLI ends, so while it runs the calls are not
// known here, and that is what is said.
const TRACE_MISSING = {
  no_trace: "沒有這個 task 的 trace（CLI 未在本 host 執行過此 task，或執行中斷）",
  unreadable: "trace 無法讀取或尚未完成：不顯示任何對應",
  inconsistent: "trace 與 Store 的裁決不一致：拒絕對應（兩者都未修改）",
  ambiguous: "這個 task 有多份 trace：無法確定，不顯示對應",
};
const USAGE = [["input_tokens", "input"], ["output_tokens", "output"], ["cache_creation_input_tokens", "cache_creation"], ["cache_read_input_tokens", "cache_read"]];
function liveTools(reply, taskId, toolRows) {
  const title = "工具呼叫（真實 Agent trace）";
  const note = "工具呼叫來自 CLI 結束後才寫出的白名單 trace，不是逐工具即時串流；請購與事件的對應由 Host 推導；不含模型文字。";
  if (!reply || reply.task_id !== taskId) return { title, note, rows: [["工具呼叫", "讀取中（未確認）", "—"]] };
  if (reply.state === "running") return { title, note, rows: [["工具呼叫", "CLI 執行中：trace 在 CLI 結束後才寫出，現在無法逐工具顯示（不代表工具尚未呼叫）", "Host"]] };
  if (reply.state !== "available" || !reply.trace) return { title, note, rows: [["工具呼叫", TRACE_MISSING[reply.reason] || "無法取得（未確認）", "Host"]] };
  const tr = reply.trace;
  const u = tr.usage || null;
  return { title, note, rows: [
    ["Gate", "gate_mode " + str(tr.gate_mode) + " · init gate " + str(tr.gate) + " · 結果 " + str(tr.outcome), "trace"],
    ...toolRows(tr),
    ["用量（CLI 自報）", u ? USAGE.filter(([k]) => typeof u[k] === "number").map(([k, n]) => n + " " + u[k]).join(" · ") + "（不是計費證明）" : "unknown（CLI 未回報）", "trace"],
  ] };
}

const short = (h) => (typeof h === "string" && h.length > 22 ? h.slice(0, 22) + "…" : str(h));

// evidence is the evidence panel for one purchase of the full stored
// snapshot (never a replay step). In a replay it says so and marks the
// events the replay has not reached. ctx adds sent (the projection the page
// sent to Unity, with the purchase the page derived for it), report (what
// Unity last reported: it carries no purchase_order_id) and motion (the
// page's account of the shop's last move, D2: { rule, text }).
export function evidence(snapshot, po, ctx) {
  if (!snapshot) return null;
  const replay = ctx.mode === "replay";
  const hist = ctx.history || null;
  const t = snapshot.task;
  const p = (t.purchases || []).find((x) => x.purchase_order_id === po) || null;
  const evs = snapshot.events;
  const mine = evs.filter((e) => !e.purchase_order_id || e.purchase_order_id === po);
  const proposed = evs.find((e) => e.event_type === "PURCHASE_PROPOSED" && e.purchase_order_id === po);
  const cat = catalogFor(ctx.catalog, p);
  const d = (p && p.decision) || {};
  const sections = [
    { title: "報價", note: "402 的報價以 payment_requirement_hash 綁定在請購上；目錄價格只是說明，不與此請購綁定。", rows: p ? [
      ["資源", str(p.http_method) + " " + str(p.resource_url), "Store"], ["amount（基本單位）", amount(p.amount), "Store"],
      ["asset", str(p.asset_id), "Store"], ["network（chain）", str(p.chain_id), "Store"], ["payTo", str(p.pay_to), "Store"],
      ["payment_requirement_hash", str(p.payment_requirement_hash), "Store"],
      ["目錄價格", cat ? str(cat.price.asset_display) + "；" + str(cat.price.fiat_amount) + " " + str(cat.price.fiat_currency) : "目錄未取得", "商店 HTTP /catalog" + (ctx.catalogAt ? "（瀏覽器讀取 " + ctx.catalogAt + "）" : "")],
      ["FX", cat ? str(cat.price.fx_rate) + "（" + str(cat.price.fx_source) + "）" : "目錄未取得", "商店 HTTP /catalog"],
      ["完整 402 內容", "未提供（API 未開放；D4 暫緩）", "—"],
    ] : [] },
    { title: "請購", note: "intent_hash 為 fixture domain（agent-payment-authority/m1-fixture/v2/verifier-not-deployed），不是真實付款綁定。", rows: p ? [
      ["purchase_order_id", str(p.purchase_order_id), "Store"], ["request_id", str(p.request_id), "Store"],
      ["intent_hash", str(p.intent_hash), "Store"], ["payment_requirement_hash", str(p.payment_requirement_hash), "Store"],
      ["PURCHASE_PROPOSED", proposed ? "#" + proposed.sequence + " · " + hhmmss(proposed.observed_at) : UNKNOWN, "Store 事件"],
    ] : [] },
    { title: "裁決", note: "固定原因代碼；M1 沒有核准、付款或交付。", rows: p ? [
      ["verdict", str(d.policy_verdict), "Store"], ["reason", reasonText(d.reason), "Store"], ["approval_status", str(d.approval_status), "Store"],
      ["payment_status", str(d.payment_status), "Store"], ["decided_at", hhmmss(p.decided_at), "Store 時間"],
      ["deadline_at", typeof p.deadline_at === "number" ? hhmmss(p.deadline_at) : "—（非 Ask）", "Store 時間"],
      ["收尾", p.approval_closed ? str(p.approval_closed) + " · " + hhmmss(p.closed_at) : hist ? "執行結束時尚未收尾（歷史快照）" : "尚未收尾", "Store"],
    ] : [] },
    { title: "Store 事件", note: "hash chain 只表示這份事件序列前後相連，不是 ZKP，也不是 B1-07 獨立驗證。", rows: mine.map((e) => [
      "#" + e.sequence + " " + str(e.event_type),
      hhmmss(e.observed_at) + " · " + str(e.source) + "/" + str(e.evidence_mode) + " · prev " + short(e.prev_event_hash) + (replay && e.sequence > ctx.n ? " · 回放尚未到此" : ""),
      "Store",
    ]) },
  ];
  // The tool calls of a run trace, each joined (by the Host, after a
  // verdict check) to its purchase and Store events.
  const toolRows = (tr) => tr.tool_calls.map((c) => [
    "#" + c.n + " " + str(c.tool),
    (c.product_id ? str(c.product_id) + " · " : "") + "outcome " + str(c.outcome)
      + (c.purchase_order_id ? " → " + str(c.purchase_order_id) + "（Store 事件 " + (c.event_sequences || []).map((q) => "#" + q).join("、") + "）" : "")
      + (replay && (c.event_sequences || []).some((q) => q > ctx.n) ? " · 回放尚未到此" : ""),
    c.purchase_order_id ? "trace；請購與事件對應為 Host 推導" : "trace",
  ]);
  const trace = hist && hist.trace && hist.trace.task_id === t.task_id ? hist.trace : null;
  if (hist) {
    sections.push({ title: "工具呼叫（真實執行 trace）",
      note: "來自該次真實執行的白名單 trace（gate_mode " + str(trace ? trace.gate_mode : "—") + "，init gate " + str(trace ? trace.gate : "—")
        + "）；工具呼叫本身不在 Store，對應的 Store 事件由 Host 依請購 ID 規則找出。不含模型文字。",
      rows: trace ? toolRows(trace) : [["工具呼叫", hist.trace ? "這個 task 沒有 trace（trace 屬於另一個 task）"
        : "沒有可顯示的 trace（host 未指定 --history-trace，或 trace 的 task 不在你的查看範圍）", "—"]] });
  } else if (ctx.live) {
    sections.push(liveTools(ctx.trace, t.task_id, toolRows));
  }
  // In step only when Unity's report names the projection sent: the same
  // generation, task, sequence and phase. Anything else is "not yet in
  // step", never a completed sync (review of 7ffc45d, R1).
  const sent = ctx.sent, rep = ctx.report;
  const diffs = [];
  if (sent && rep) {
    if (rep.gen !== sent.gen) diffs.push("世代不同（Unity gen " + rep.gen + "）");
    if (rep.task !== sent.task) diffs.push("task 不同（Unity " + str(rep.task) + "）");
    if (rep.seq < sent.seq) diffs.push("序號較舊（Unity #" + rep.seq + "，送出 #" + sent.seq + "）");
    else if (rep.seq !== sent.seq) diffs.push("序號不同（Unity #" + rep.seq + "）");
    if (rep.phase !== sent.phase) diffs.push("phase 不同（Unity " + str(rep.phase) + "）");
  }
  const same = !!(sent && rep) && diffs.length === 0;
  const sync = !sent ? "尚未送出投影" : !rep ? "Unity 尚未回報：尚未一致" : same ? "Unity 已呈現 " + str(rep.task) + " #" + rep.seq + "（" + str(rep.phase) + "）"
    : "Unity 尚未一致：" + diffs.join("、") + "（不是同步完成）";
  sections.push({ title: "場景對應", note: "Unity 的回報沒有 purchase_order_id；請購關聯由網頁依送出的投影推導，不是 Unity 獨立確認。", rows: [
    ["送出投影", sent ? (replay ? "回放 · " : "") + "gen " + sent.gen + " · " + str(sent.task) + " #" + sent.seq + " " + str(sent.phase) : "尚未送出", "網頁"],
    ["對應請購", sent && sent.po ? sent.po + "（網頁推導）" : "—", "網頁推導"],
    ["這次過場", ctx.motion ? ctx.motion.text + "（規則 " + ctx.motion.rule + "）" : "—", "網頁依轉換表推導"],
    ["Unity 回報", rep ? "gen " + rep.gen + " · " + str(rep.task) + " #" + rep.seq + " " + str(rep.phase) : "尚未回報", "瀏覽器觀測"],
    ["同步", sync, "網頁比較"],
    ...(sent && sent.po && po && po !== sent.po ? [["選取", "你正在查看 " + po + "；商店場景呈現的是 " + sent.po + "（選取不同，不是同步失敗）", "網頁"]] : []),
  ] });
  return {
    banner: (hist ? hist.label + " · " : ctx.live ? ctx.live.label + " · " : "") + (replay ? "完整紀錄，非回放當步（回放位置 #" + ctx.n + "／共 " + ctx.m + "）"
      : hist ? "歷史紀錄：執行結束時的快照 #" + t.as_of_sequence + "（非即時）" : "即時：最新一致快照 #" + t.as_of_sequence),
    po: po || null, sections,
  };
}
