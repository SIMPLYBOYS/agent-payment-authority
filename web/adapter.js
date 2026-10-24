// adapter.js: the Dashboard's pure logic, no DOM (docs/demo-ui.md section 3).
// Everything shown comes from two authenticated reads of the Store:
// GET /api/tasks/{id} and GET /api/tasks/{id}/events. Nothing here invents
// a decision, an expiry, a payment or a delivery.

export const UNKNOWN = "未知／未提供";

// PAYING: the payment capabilities under which the host pays an Allow and a
// person's approval (P2), by name: a local anvil (P2-a) or the Base Sepolia
// testnet (docs/commercial-p2b.md B3). Anything else pays nothing.
export const PAYING = { local_anvil: "本機 anvil", base_sepolia: "Base Sepolia testnet" };
// EXPLORER (E1-a): a public chain's transaction page; a local anvil has none.
export const EXPLORER = { base_sepolia: "https://sepolia.basescan.org/tx/" };
const txRef = (cap, hash) => str(hash) + (Object.hasOwn(EXPLORER, cap) ? " · " + EXPLORER[cap] + hash : cap === "local_anvil" ? "（本機鏈，沒有 explorer）" : "");
const RUN = { agent: { live: "真實 Agent（live CLI）", fake: "fake Agent" }, chain: { base_sepolia: "Base Sepolia", local_anvil: "本機 anvil", none: "不付款" },
  authenticator: { device: "實體裝置（主持人宣告）", virtual: "virtual authenticator", none: "沒有 Signer" } };
// serviceRows (gap review C2): the readiness rows of the Signer, its voice
// service and the budget relayer, from the host's status (services).
const SERVICE = {
  signer: { ready: ["ok", "就緒（核准與付款簽章）"], "approvals-only": ["ok", "就緒（只核准，不簽付款）"], unavailable: ["error", "無法連線：核准會失敗。下一步：看啟動 demo 的終端機；通常要 Ctrl-C 後重新啟動"],
    timeout: ["error", "3 秒內沒有回應：核准可能失敗，請檢查 Signer"] },
  voice: { ready: ["ok", "就緒（模型已核對）"], off: ["off", "未設定（核准不含語音）"], unavailable: ["error", "無法使用：核准會記為語音降級（展示時要照實說）。下一步：Ctrl-C 後重新啟動"],
    unknown: ["unconfirmed", "未確認（Signer 未回報；Signer 無法連線或是舊版）"] },
  relayer: { ready: ["ok", "就緒"], in_process: ["ok", "Host 程序內（本機 anvil）"], unavailable: ["error", "無法連線：付款會失敗。下一步：看啟動 demo 的終端機；通常要 Ctrl-C 後重新啟動"],
    timeout: ["error", "3 秒內沒有回應：付款可能失敗，請檢查 apa-relayer"] },
};
export function serviceRows(s) {
  if (!s) return [];
  const row = (k, name) => [name, ...(SERVICE[k][s[k]] || ["unconfirmed", "未確認（" + str(s[k]) + "）"])];
  return [row("signer", "Signer"), row("voice", "語音服務"), ...(s.relayer ? [row("relayer", "relayer")] : []),
    ...TODAY.filter(([k]) => s[k]).map(([k, name, need]) => todayRow(name, s[k], need))];
}
// C4 (10/24: a show ran into the relayer's cap unseen): today's transactions
// against each daily cap, a UTC day. One show of two customers needs the
// relayer 4 (two registrations, two reservations) and the shop 2.
const TODAY = [["relayer_today", "relayer 今天", 4], ["shop_today", "商店今天", 2]];
function todayRow(name, { sent, cap }, need) {
  const left = Math.max(cap - sent, 0);
  const head = "已送 " + sent + "／" + cap + " 筆（UTC 日，台北 08:00 歸零）";
  return left === 0 ? [name, "error", head + "：已達上限，今天不能再付款。下一步：台北 08:00 後再開場，或改用本機場"]
    : left < need ? [name, "unconfirmed", head + "：剩 " + left + " 筆，不夠一場兩位客人（要 " + need + " 筆）。下一步："
      + (left >= need / 2 ? "只跑一位客人，或台北 08:00 後再開場" : "不夠一位客人（review：登記後就沒有預留的額度）；台北 08:00 後再開場，或改用本機場")]
    : [name, "ok", head + "：剩 " + left + " 筆"];
}
// approverText: who approves, by the authenticator the host names (never a virtual one as a device).
export const approverText = (auth) => (auth === "virtual" ? "本人以 virtual authenticator（測試用，不是實體裝置）" : "本人以已登記的裝置");
export const paysOn = (cap) => typeof cap === "string" && Object.hasOwn(PAYING, cap);

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
// The closing events of an Ask and what each records; the last three are
// apa.pay.v1's (P1-a), never in an M1 task.
const CLOSED = { APPROVAL_EXPIRED: "expired", APPROVAL_WITHDRAWN: "withdrawn", APPROVED: "approved", APPROVAL_DENIED: "denied", APPROVAL_SUPERSEDED: "superseded" };
const CLOSED_PHASE = { expired: "expired", withdrawn: "withdrawn", approved: "approved", denied: "approval_denied", superseded: "superseded" };
export const V1 = "apa.pay.v1";
export const V2 = "apa.pay.v2"; // C2: apa.pay.v1 plus the BBS+ credential step
export const isPay = (v) => v === V1 || v === V2;
const pay = (task) => !!task && isPay(task.contract_version); // the pay family: P1-a's words for both

// UNITY_PHASE: the Unity build knows the M1 phases only and is not rebuilt
// for P1-a, so the simulated approval's outcomes go to it as the M1 phase
// whose scene is true of them: approved stays at the counter with "ALLOW -
// no payment capability" (no payment, no pickup); a denial leaves with its
// reason code; a superseded Ask leaves as withdrawn. The page's own labels
// say what really happened.
export const UNITY_PHASE = { approved: "allowed_unpaid", approval_denied: "denied", superseded: "withdrawn" };

// PAY_PHASE (P2, docs/commercial-p2-host.md 5): a purchase's payment
// events as phases, the page's and Unity's (its build knows them).
const PAY_PHASE = { PAYMENT_REQUESTED: "paying", BUDGET_RESERVED: "paying", PAYMENT_SIGNED: "paying", PAYMENT_SUBMITTED: "confirming",
  PAYMENT_SETTLED: "paid", RESOURCE_DELIVERED: "paid", RECEIPT_ISSUED: "delivered", PAYMENT_NOT_SETTLED: "not_paid", PAYMENT_NEEDS_REVIEW: "payment_review" };

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
      case "APPROVAL_EXPIRED": case "APPROVAL_WITHDRAWN": case "APPROVED": case "APPROVAL_DENIED": case "APPROVAL_SUPERSEDED":
        if (po) Object.assign(of(po), { closed: CLOSED[e.event_type], closedSeq: e.sequence, method: e.authorization_method || "",
          closedReason: (e.decision && e.decision.reason) || "" });
        break;
      case "PAYMENT_REQUESTED": case "BUDGET_RESERVED": case "PAYMENT_SIGNED": case "PAYMENT_SUBMITTED": case "PAYMENT_SETTLED":
      case "RESOURCE_DELIVERED": case "RECEIPT_ISSUED": case "PAYMENT_NOT_SETTLED": case "PAYMENT_NEEDS_REVIEW":
        if (po) {
          const p = of(po);
          // Review after settlement is a failed delivery: paid, not delivered.
          p.pay = e.event_type === "PAYMENT_NEEDS_REVIEW" && p.pay === "paid" ? "delivery_failed" : PAY_PHASE[e.event_type];
          p.paySeq = e.sequence;
        }
        break;
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
  if (phase === "waiting" && cur.closed) phase = CLOSED_PHASE[cur.closed] || phase;
  // P2 (PAYING): the host pays an Allow and a person's approval.
  const pays = !!task && paysOn(task.payment_capability);
  if (pays && phase === "allowed_unpaid") phase = "allowed";
  const paidApproval = pays && phase === "approved" && cur.method === "webauthn";
  if (cur && cur.pay) phase = cur.pay;
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
    // apa.pay.v1 (P1-a): the simulated approval's outcomes.
    approved: "已核准（模擬）：未啟用付款，不會取貨", approval_denied: "核准者拒絕：離店", superseded: "報價已改變：需要重新請購",
    // P2: the payment.
    allowed: "Allow：在委託範圍內，尚未付款",
    paying: "付款中：預算預留與簽名", confirming: "已送出，等待鏈上確認（不會自動再付）", paid: "已付款，準備交付", delivered: "已付款、已交付",
    delivery_failed: "已付款，交付失敗：待人工處理", not_paid: "未付款（沒有花錢）：離店", payment_review: "待人工處理：預算已預留但未付款",
  };
  if (pay(task)) labels.waiting = simulated() ? "等你核准（模擬核准；不會付款）" : "等你核准（本人以已登記的裝置；" + (pays ? "核准後由 Host 付款）" : "不會付款）");
  if (pay(task) && (end || denied([...purchases.values()].map((x) => x.decision)))) labels.waiting = ENDED_ASK;
  if (cur && CREDENTIAL_CLOSED[cur.closedReason]) labels.withdrawn = CREDENTIAL_CLOSED[cur.closedReason] + "：離店"; // C2: not the task's cancellation
  if (cur && cur.method === "webauthn") labels.approved = pays ? "本人核准已驗證：尚未付款" : "本人核准已驗證：未啟用付款，不會取貨"; // the record's own method (review R4)
  const d = cur && cur.decision ? cur.decision : null;
  const causeOf = { entered: startedSeq, quoted: cur && cur.proposedSeq, waiting: cur && cur.decidedSeq, denied: cur && cur.decidedSeq,
    allowed_unpaid: cur && cur.decidedSeq, allowed: cur && cur.decidedSeq, expired: cur && cur.closedSeq, withdrawn: cur && cur.closedSeq,
    approved: cur && cur.closedSeq, approval_denied: cur && cur.closedSeq, superseded: cur && cur.closedSeq };
  const cause = cur && cur.pay ? cur.paySeq : causeOf[phase];
  return { phase, label: labels[phase] || UNKNOWN, notes, po: shown, verdict: d ? d.policy_verdict || "" : "", reason: d ? d.reason || null : null,
    cause: Number.isInteger(cause) && cause > 0 ? cause : 0, end: firstEnd, unity: cur && cur.pay ? phase : paidApproval ? "approved" : UNITY_PHASE[phase] || phase };
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
    ["付款", t.payment_capability === "local_anvil" ? "本機 anvil 付款（P2-a：測試金鑰、測試 USDC；payment_capability：local_anvil）"
      : t.payment_capability === "base_sepolia" ? "Base Sepolia testnet 付款（測試 USDC；payment_capability：base_sepolia）"
      : "未付款（payment_capability：" + str(t.payment_capability) + "）"],
    ...(t.contract_version ? [["契約", str(t.contract_version) + (t.delegation_id ? " · 委託 " + str(t.delegation_id) : "")]] : []),
  ];
  // E1-a: what this task's events come from (its TASK_STARTED), and the budget contract tx#1 calls.
  const rc = ((snapshot.events || []).find((e) => e.event_type === "TASK_STARTED") || {}).run_conditions;
  if (rc) rows.push(["執行條件", [RUN.agent[rc.agent], RUN.chain[rc.chain], RUN.authenticator[rc.authenticator]].map(str).join(" · ") + "（記在 TASK_STARTED；每個事件依此標 live 或 fixture）"]);
  if (t.budget_contract) rows.push(["預算合約（Z2，主機設定）", str(t.budget_contract)]);
  for (const p of t.purchases || []) {
    const d = p.decision || {};
    rows.push(
      ["— 請購", str(p.purchase_order_id)], ["資源", str(p.http_method) + " " + str(p.resource_url)], ["payTo", str(p.pay_to)],
      ["amount（基本單位）", amount(p.amount)], ["asset", str(p.asset_id)], ["network（chain）", str(p.chain_id)],
      ["intent hash", str(p.intent_hash)], ["裁決", str(d.policy_verdict)], ["reason", str(d.reason)],
      ["approval", str(d.approval_status) + (p.approval_closed ? "，收尾：" + str(p.approval_closed) : "")],
      ["payment_status", str(d.payment_status)],
    );
    const pv = p.payment;
    if (pv && pv.reserve_tx) rows.push(["tx#1 預留（Z2）", txRef(t.payment_capability, pv.reserve_tx) + (typeof pv.reserve_block === "number" ? " · 區塊 " + pv.reserve_block : "")],
      ["ZK proof", "在 tx#1 的 calldata 裡，由預算合約呼叫 verifier 驗證；本頁不自行宣稱已驗證（要宣稱已驗證，須看獨立 checker 的結果）"]);
    if (pv && pv.settle_tx) rows.push(["tx#2 結算", txRef(t.payment_capability, pv.settle_tx) + (typeof pv.settle_block === "number" ? " · 區塊 " + pv.settle_block : "")]);
    if (pv && pv.receipt_id) rows.push(["收據", str(pv.receipt_id) + " · 內容 sha256 " + str(pv.content_sha256)]);
  }
  return rows;
}

// payDetail is a payment event's record in a Timeline row (E1-a).
const payDetail = (r) => [r.method && "方法 " + str(r.method), r.amount && "金額 " + amount(r.amount), r.tx_hash && "tx " + str(r.tx_hash),
  typeof r.block === "number" && "區塊 " + r.block, r.receipt_id && "收據 " + str(r.receipt_id)].filter(Boolean).join(" · ");

// timeline rows, one per event; json is the API's whitelisted event, not
// CLI output and not a raw log.
export function timeline(snapshot) {
  if (!snapshot) return [];
  return snapshot.events.map((e) => ({
    id: e.event_id,
    seq: e.sequence,
    type: str(e.event_type),
    time: typeof e.observed_at === "number" ? new Date(e.observed_at * 1000).toISOString() : UNKNOWN,
    detail: e.decision ? str(e.decision.policy_verdict) + " / " + str(e.decision.reason) + (e.decision.approval_status && e.decision.approval_status !== "pending" && e.decision.approval_status !== "not_requested" ? " / " + e.decision.approval_status : "")
      : e.grant_id ? "grant " + short(e.grant_id) + "（" + str(e.authorization_method) + "）" : e.approval_session_id ? "session " + short(e.approval_session_id)
      : e.delegation_id ? "delegation " + str(e.delegation_id)
      : e.payment ? payDetail(e.payment) : e.code ? str(e.code) : "",
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
    seq, phase: sc.unity, verdict: sc.verdict, reason: sc.phase === "approval_denied" ? "APPROVER_DENIED" : sc.reason || "", animate: !!animate,
    cause_seq: sc.cause > 0 && sc.cause <= seq ? sc.cause : 0, task_end: sc.end,
    payments: paysOn(task.payment_capability) ? task.payment_capability : "", // P2: the banner
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
// The decisions at the counter; a payment's own phases stand there too but
// are not decisions (each step has a new cause, never a new purchase).
const COUNTER = new Set(["waiting", "denied", "allowed_unpaid", "allowed", "approved"]);
const labelKind = (phase, end) => (phase === "waiting" && end === "timed_out" ? "waiting/timed_out" : phase);
const fixedCode = (r) => (typeof r === "string" && /^[A-Z0-9_]{1,40}$/.test(r) ? r : "");

// shopLabel is the customer's label in Unity, in plain words (gap review
// B3, GD3): no code, no event number (the cause still decides the rule).
// Unity's SceneRules.Label says the same; both are tested with the shared vectors.
export function shopLabel(phase, cause, end, reason) {
  switch (phase) {
    case "empty": return "";
    case "entered": return "進店";
    case "quoted": return "選好商品，送出請購";
    case "waiting": return end === "timed_out" ? "Agent 已逾時，請購仍在等核准" : "超過自動範圍，等核准";
    case "denied": {
      const why = PLAIN[fixedCode(reason)] || "";
      return why === "核准者拒絕" ? "核准者拒絕，離店" : (why ? why + "，" : "") + "已拒絕，離店";
    }
    case "allowed_unpaid": return "已放行，這筆不會付款"; // also a simulated approval's (UNITY_PHASE)
    // Until a payment event: true whether or not a payment ever starts.
    case "allowed": return "在範圍內放行，尚未付款";
    case "approved": return "已核准，尚未付款";
    case "paying": return "付款中：預留預算";
    case "confirming": return "付款已送出，等鏈上確認";
    case "paid": return "已付款，在交付區等交付";
    case "delivered": return "已交付，帶著收據離店";
    case "delivery_failed": return "已付款，交付失敗（待人工處理）";
    case "not_paid": return "沒有付款（沒有花錢），離店";
    case "payment_review": return "付款沒有照預期完成，待主持人確認"; // true of every review code (the label has none)
    case "expired": return "核准期限已過，離店";
    case "withdrawn": return "已取消，離店";
    default: return "狀態未確認";
  }
}

const BASIS = { entered: "進店事件", quoted: "請購", waiting: "裁決", denied: "裁決", allowed_unpaid: "裁決", expired: "到期", withdrawn: "撤回",
  allowed: "裁決", approved: "本人核准", paying: "付款", confirming: "付款送出", paid: "鏈上確認", delivered: "交付與收據", delivery_failed: "交付失敗",
  not_paid: "未付款", payment_review: "付款待人工處理" };

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
      ...(phase === "entered" ? ["進店"] : []), ...(["denied", "expired", "withdrawn", "delivered", "not_paid"].includes(phase) ? ["離店"] : [])];
    const basis = BASIS[phase] ? BASIS[phase] + (cause ? "已在 #" + cause + " 記錄" : "已記錄（序號未知）") : "未知狀態";
    text = (rule === 2 ? "新的一筆請購：" : "") + (parts.length ? parts.join(" → ") + "；" : "") + basis;
  }
  return { rule, browse, target, text };
}

export function clearMessage(gen) {
  return { v: BRIDGE_VERSION, gen, clear: true };
}

// ---- Version 2: the scene with a slot per customer (docs/multi-agent-demo-design.md 5) ----
// The host page shows every consumer's latest run at once. scene is the
// page's scene generation (a credentials change, a replay); slot is one
// consumer's slot generation (its scene task changed). A message for one
// customer never moves, clears or completes another.
export const BRIDGE_V2 = 2;

// projection2: the version 2 message of a slot's committed snapshot.
// UX3: a version 2 message names the scene's room (layout) when the page
// has one; every message of a scene carries the same one (Unity refuses a
// mismatch), and none is the build's default room.
const room = (layout) => (layout ? { layout } : {});

export function projection2(slotState, scene, slot, animate, layout) {
  const m = projection(slotState, animate);
  if (!m) return null;
  const { gen, ...rest } = m;
  return { ...rest, v: BRIDGE_V2, scene, slot, clear: false, ...room(layout) };
}

export const sceneClear2 = (scene, layout) => ({ v: BRIDGE_V2, scene, clear: true, ...room(layout) });
export const slotClear2 = (scene, consumer, slot, layout) => ({ v: BRIDGE_V2, scene, consumer, slot, clear: true, ...room(layout) });

// VENUES: the UX3 scenes (docs/commercial-scenes.md 3) as the page names
// them. cafe and mart are demo scenes: fixture products, no POS, inventory
// or logistics. Delivery waits for payment (UX2-D4); nothing pays here.
export const VENUES = {
  data: { name: "資料研究室", demo: false, goods: "數位資料", delivery: "下載終端（付款後才交付）" },
  cafe: { name: "街角咖啡", demo: true, goods: "餐點・示意菜單", delivery: "出餐口叫號（付款後才交付）" },
  mart: { name: "巷口便利商店", demo: true, goods: "商品・示意商品", delivery: "取貨櫃（付款後才交付）" },
};
export const venue = (key) => VENUES[key] || { name: "情境 " + String(key || "未知"), demo: true, goods: "商品", delivery: "付款後才交付" };
// venues: the scenes a catalog offers, the main one first, then as listed above, then by name.
export function venues(scenarios) {
  const order = Object.keys(VENUES);
  const rank = (k) => (order.includes(k) ? order.indexOf(k) : order.length);
  return [...new Set((scenarios || []).map((x) => x.scene || "data"))].sort((a, b) => rank(a) - rank(b) || a.localeCompare(b));
}

// parseReport2: Unity's version 2 rendered report, or null when malformed:
// gen is the scene; detail is consumer|slot|task|phase|verdict|reason.
export function parseReport2(gen, seq, detail) {
  const parts = String(detail).split("|");
  if (parts.length !== 6 || !Number.isInteger(gen) || !Number.isInteger(seq) || !/^(0|[1-9][0-9]{0,8})$/.test(parts[1])) return null;
  const [consumer, slot, task, phase, verdict, reason] = parts;
  if (!consumer || !task || !phase) return null;
  return { scene: gen, seq, consumer, slot: Number(slot), task, phase, verdict, reason };
}

// sameShown2: whether a version 2 report names exactly the projection last
// sent for that customer: scene, consumer, slot, task, seq and phase, all
// six. Anything else (a missing field, another customer, an older slot or
// scene, an older or a never sent later sequence) confirms nothing.
export const sameShown2 = (sent, rep) => !!sent && !!rep && Number.isInteger(rep.scene) && rep.scene === sent.scene && rep.consumer === sent.consumer
  && rep.slot === sent.slot && rep.task === sent.task_id && rep.seq === sent.seq && rep.phase === sent.phase;

// parseBridge: Unity's bridge report: the versions and customers the build
// can show; null when malformed. A build without it shows one customer.
export function parseBridge(detail) {
  const m = /^versions=([0-9,]+);customers=([a-z0-9,-]*)(;select=1)?(;layouts=([a-z0-9,-]*))?$/.exec(String(detail));
  if (!m) return null;
  // select: the build takes the page's Select and reports clicks (UX1,
  // docs/commercial-ux1.md); layouts: the rooms it has (UX3). An older
  // build says nothing of either.
  return { versions: new Set(m[1].split(",").map(Number)), customers: m[2] ? m[2].split(",") : [], select: !!m[3], layouts: m[5] ? m[5].split(",") : [] };
}

// sceneTasks: each consumer's scene task, the task of its newest run (the
// runs list is newest first). An older task stays out of the scene. With a
// venue (UX3), only that scene's runs count; a run that names no scene (its
// catalog entry changed since) is in none.
export function sceneTasks(runs, venue) {
  const out = new Map();
  for (const r of runs || []) {
    if (venue && (!r || r.scene !== venue)) continue;
    if (r && r.consumer_id && r.task_id && !out.has(r.consumer_id)) out.set(r.consumer_id, { task: r.task_id, run: r.run_id, live: r.live || "" });
  }
  return out;
}

// notShown: the consumers whose runs the scene cannot show (not in the
// build's customers), and whether the host's cap is above what it shows.
export function notShown(consumers, customers, maxRuns) {
  const can = new Set(customers);
  return { consumers: consumers.filter((c) => !can.has(c)), overCap: Number.isInteger(maxRuns) && maxRuns > can.size };
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
      case "APPROVAL_EXPIRED": case "APPROVAL_WITHDRAWN": case "APPROVED": case "APPROVAL_DENIED": case "APPROVAL_SUPERSEDED": open.delete(po); break;
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
    if (e.event_type === "CREDENTIAL_VERIFIED" && e.credential) p.credential = e.credential;
    if (CLOSED[e.event_type]) {
      p.approval_closed = CLOSED[e.event_type]; p.closed_at = e.observed_at;
      if (e.decision && e.decision.reason) p.closed_reason = e.decision.reason;
    }
  }
  const pr = project(prefix);
  return {
    task: {
      task_id: task.task_id, consumer_id: task.consumer_id, agent_id: task.agent_id, wallet_id: task.wallet_id, payer: task.payer,
      wallet_evidence_mode: task.wallet_evidence_mode, payment_capability: task.payment_capability, contract_version: task.contract_version,
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
// apa.pay.v1 words (P1-a): the same codes where both have them, without M1's "cannot".
export const REASONS_V1 = {
  ...REASONS, HUMAN_APPROVAL_REQUIRED: "超過委託的 allow_max，需要你核准", WITHIN_ALLOW_MAX: "在委託範圍內；未啟用付款",
  SIMULATED_APPROVAL: "模擬核准（主持人扮演，不是本人驗證）", APPROVER_DENIED: "核准者拒絕", QUOTE_CHANGED: "報價已改變，需重新請購",
  WEBAUTHN_APPROVAL: "本人核准（Signer 驗證已登記的裝置）",
  // apa.pay.v2 (C2): the credential's reasons.
  VOICE_CHALLENGE_FAILED: "語音挑戰未通過，這筆已拒絕", // V1
  CREDENTIAL_INVALID: "憑證驗證未通過", CREDENTIAL_REVOKED: "憑證已撤銷", CREDENTIAL_STATUS_UNAVAILABLE: "憑證狀態無法確認",
  AUTHORIZATION_EXPIRED: "憑證已過期", CONFIG_MISSING: "缺少設定（沒有憑證）",
};

// V1: the voice outcome codes, in words. A failure denies the purchase; an
// unavailable voice only degrades the approval (and says so).
export const VOICE = {
  VOICE_REPLAY: "錄音是重播（這段錄音已用過）",
  VOICE_PHRASE_MISMATCH: "唸的內容和這筆不符（金額、商家或代碼）",
  VOICE_SPEAKER_MISMATCH: "聲音不是已登記的核准者",
  VOICE_AUDIO_INVALID: "錄音無法辨識",
  VOICE_DEVICE_UNAVAILABLE: "沒有麥克風，或沒有允許使用麥克風",
  VOICE_SERVICE_UNAVAILABLE: "語音服務無法使用",
  VOICE_TIMEOUT: "語音服務逾時",
};
export const VOICE_FAILURES = ["VOICE_REPLAY", "VOICE_PHRASE_MISMATCH", "VOICE_SPEAKER_MISMATCH", "VOICE_AUDIO_INVALID"];
export const voiceText = (c) => VOICE[c] || c || "原因未回報";

// voiceEventText: a voice outcome event, in words (V1).
export function voiceEventText(e) {
  if (e.event_type === "VOICE_CHALLENGE_VERIFIED") return "通過（金額、商家、代碼與聲紋）";
  if (e.event_type === "VOICE_CHALLENGE_FAILED") return "未通過：" + voiceText(e.code) + "；這筆已拒絕，不發授權";
  return "降級：" + voiceText(e.code) + "（仍需摘要確認與裝置驗證）";
}

// C2: an Ask withdrawn for a credential reason (design 7.1) is worded by its
// reason, never as the task's cancellation (HOST_CANCELLED).
export const CREDENTIAL_CLOSED = {
  CREDENTIAL_REVOKED: "憑證已撤銷，這筆已關閉", CREDENTIAL_STATUS_UNAVAILABLE: "憑證狀態無法確認，這筆已關閉", AUTHORIZATION_EXPIRED: "憑證已過期，這筆已關閉",
};
const withdrawnText = (p) => CREDENTIAL_CLOSED[p && p.closed_reason] || "已撤回（task 取消）";

// credentialText words a purchase's verification record (apa.pay.v2):
// its result and fixed detail code; the scheme is the old BBS+, not W3C
// bbs-2023, with synthetic test keys (C1-D1).
const CREDENTIAL_RESULT = { valid: "通過", absent: "沒有憑證（未登記持有人）", invalid: "未通過", status_unavailable: "狀態清單無法確認", revoked: "已撤銷", expired: "已過期" };
export function credentialText(c) {
  if (!c || typeof c !== "object") return UNKNOWN;
  return (CREDENTIAL_RESULT[c.result] || str(c.result)) + (c.detail ? "（" + str(c.detail) + "）" : "")
    + (c.result === "valid" ? "：BBS+（舊版，非 W3C bbs-2023）・測試金鑰" : "");
}

// The host's way of approving (A2): simulated in P1-a, webauthn with the
// person's registered device; it words what is still pending or decided.
let approvalMode = "simulated";
export function setApprovalMode(m) { approvalMode = m === "webauthn" ? "webauthn" : "simulated"; }
export const simulated = () => approvalMode !== "webauthn";
// A decided record is worded by its own method, never the host's mode
// (review R4): the Signer's decisions are resolved_by "webauthn:…".
export const byDevice = (p) => !!(p && p.approval && typeof p.approval.resolved_by === "string" && p.approval.resolved_by.startsWith("webauthn:"));
const grantText = (g) => (g.authorization_method === "webauthn" ? "本人核准 grant " : "模擬 grant ");
// reasonText: a paid Allow is not "payment not enabled" (gap review B4).
const reasonText = (r, task) => {
  if (typeof r !== "string" || r === "") return UNKNOWN;
  const w = (pay(task) ? REASONS_V1 : REASONS)[r];
  return r + (w ? "（" + (paysOn(task && task.payment_capability) ? w.replace("；未啟用付款", "；由 Host 付款（測試 USDC）") : w) + "）" : "");
};
const hhmmss = (s) => (typeof s === "number" ? new Date(s * 1000).toISOString().slice(11, 19) + " UTC" : UNKNOWN);
const CUSTOMER = { "consumer-a": "A", "consumer-b": "B" };

// catalogFor finds the shop catalog's entry for a purchase's resource.
function catalogFor(catalog, p) {
  const list = catalog && Array.isArray(catalog.products) ? catalog.products : [];
  return list.find((x) => x && p && x.resource_url === p.resource_url && x.price) || null;
}

// P2 (docs/commercial-p2-host.md 5): a purchase's payment as the page
// says it, from the Store's payment view (status, state, code).
const NOT_PAID = { RESERVE_FAILED: "預算預留失敗，沒有花錢", NOT_PAID_EXPIRED: "已過付款期限，沒有花錢", TASK_ENDED: "task 已結束，沒有花錢" };
const REVIEW = { RESERVED_NOT_PAID: "預算已預留，但付款沒有送出", NOT_SETTLED_EXPIRED: "已送出，過期未結算", VERIFICATION_FAILED: "商店回覆驗證未通過、沒有送出（商店的說法）",
  RECONCILE_NEEDS_REVIEW: "鏈上對帳需人工判斷" };
// paymentNext (C3, docs/host-manual.md 6.4): what the host does next about a
// payment that did not go through, by its code; "" when nothing is to be done.
export const AGAIN = "要再買這一筆，等這位客人的場次結束（或按 Agent 卡片上的「停止 Agent」）後再開始一次";
const NEXT = {
  not_paid: { RESERVE_FAILED: "看啟動 demo 的終端機上 relayer 拒絕的原因，與就緒表的「relayer 今天」；今天的額度用完就等台北 08:00 後再開場，今天不要再試",
    NOT_PAID_EXPIRED: AGAIN, TASK_ENDED: AGAIN },
  needs_review: { RESERVED_NOT_PAID: "錢沒有動；記下時間找開發者，不要重試這一筆（常見原因：核准人 24 小時內的核准額度已用完）",
    VERIFICATION_FAILED: "看終端機上這一筆的 shop 原因與就緒表的「商店今天」，記下來找開發者；不要重試這一筆",
    NOT_SETTLED_EXPIRED: "錢沒有動（授權已過期、鏈上確認沒有被用掉）；不用處理", RECONCILE_NEEDS_REVIEW: "停下這一場，不要重試，找開發者" },
};
export function paymentNext(pv) {
  if (!pv) return "";
  if (pv.status === "paid" && pv.code === "DELIVERY_FAILED") return "下一步：錢已付；記下時間找開發者處理交付，不要重新購買。";
  const t = NEXT[pv.status] && (NEXT[pv.status][pv.code] || (pv.status === "needs_review" ? "記下代碼 " + str(pv.code) + " 與時間，找開發者" : ""));
  return t ? "下一步：" + t + "。" : "";
}
export function paymentText(pv) {
  const block = typeof pv.settle_block === "number" ? "（區塊 " + pv.settle_block + "）" : "";
  switch (pv.status) {
    case "paying": return "付款中：" + ({ reserved: "預算已預留", signed: "已簽名" }[pv.state] || "已請求付款");
    case "confirming": return "已送出，等待鏈上確認（不會自動再付）";
    case "paid":
      if (pv.code === "DELIVERY_FAILED") return "已付款" + block + "，交付失敗，待人工處理";
      return pv.state === "delivered" ? "已付款、已交付" + block + "，附收據" : "已付款" + block + "，準備交付";
    case "not_paid": return "未付款：" + (NOT_PAID[pv.code] || str(pv.code));
    case "needs_review": return "待人工處理：" + (REVIEW[pv.code] || str(pv.code)) + "（預算的預留與付款是兩筆交易）";
  }
  return "付款狀態不明：" + str(pv.status);
}

// ENDED_ASK: an open Ask of a pay task that ended (a CLI timeout, or a Deny
// of another purchase): nobody decides it now; it expires at its deadline
// (accepted case cli-timeout-pending).
const ENDED_ASK = "task 已結束：這筆不能核准，到期後關閉；不會付款";
const denied = (decisions) => decisions.some((d) => d && d.policy_verdict === "Deny");

// reconciliationRow is the host page's reconciliation row (P2,
// docs/commercial-p2-host.md 7) from GET /api/host/reconciliation: an
// unauthorized new spend is an error; one not read is unconfirmed.
export function reconciliationRow(rec) {
  if (!rec) return ["對帳", "unconfirmed", "未確認：讀取中"];
  if (rec.error === "reconciliation_pending") return ["對帳", "unconfirmed", "未確認：對帳計算中（從部署區塊讀起，可能要幾分鐘）"];
  if (rec.error) return ["對帳", "error", "無法對帳（" + str(rec.error) + "），未確認"];
  if (rec.version !== "apa.reconciliation.v1" || !Array.isArray(rec.unauthorized)) return ["對帳", "error", "對帳結果不完整（不是一份對帳報告），未確認"];
  const env = paysOn(rec.environment) ? PAYING[rec.environment] : str(rec.environment);
  const where = env + "；區塊 " + str(rec.from_block) + "–" + str(rec.to_block) + "；錢包 " + (rec.wallets || []).length + " 個、已記錄授權 " + str(rec.known_authorizations) + " 筆";
  const spends = rec.unauthorized;
  if (spends.length === 0) return ["對帳", "ok", "未授權新增支出 0 筆（" + where + "）"];
  return ["對帳", "error", "未授權新增支出 " + spends.length + " 筆（" + where + "）："
    + spends.slice(0, 3).map((s) => amount(s.value) + " 於區塊 " + str(s.block) + "（" + short(s.tx) + "）").join("；") + (spends.length > 3 ? "；…" : "")];
}

// paymentSteps are the payment's steps from its Store events; an ending
// (not paid, or for review) replaces the steps it cut off.
function paymentSteps(at, step, pv) {
  const steps = [["預留", "BUDGET_RESERVED"], ["簽名", "PAYMENT_SIGNED"], ["送出", "PAYMENT_SUBMITTED"], ["鏈上確認", "PAYMENT_SETTLED"], ["交付", "RECEIPT_ISSUED"]]
    .map(([label, type]) => step(label, at(type), type));
  const end = at("PAYMENT_NOT_SETTLED") || at("PAYMENT_NEEDS_REVIEW");
  if (end) {
    const cut = steps.findIndex((s) => s.state !== "done");
    steps.splice(cut < 0 ? steps.length : cut, steps.length, step(end.event_type === "PAYMENT_NOT_SETTLED" ? "未付款" : "待人工處理", end, end.event_type + (pv && pv.code ? " " + pv.code : "")));
  }
  return steps;
}

function purchaseStatus(p, live, servedAt, task, ended) {
  if (!p) return "此 task 尚無請購";
  const d = p.decision;
  if (!d) return "已提出請購，尚無裁決";
  if (pay(task)) {
    if (p.payment) return paymentText(p.payment);
    const reached = live && typeof servedAt === "number" && typeof p.deadline_at === "number" && servedAt >= p.deadline_at;
    if (d.policy_verdict === "Deny") return "Deny：" + reasonText(d.reason, task);
    if (d.policy_verdict === "Allow") return paysOn(task.payment_capability) ? "Allow：在委託範圍內，尚未付款" : "Allow：在委託範圍內，但未啟用付款（不會自動付款）";
    const pays = paysOn(task.payment_capability);
    if (ended && !p.approval_closed) return ENDED_ASK + (reached ? "；已達期限，等待後端確認" : "");
    return { approved: byDevice(p) ? (pays ? "本人核准已驗證，尚未付款" : "本人核准已驗證，未啟用付款") : "已核准（模擬），未啟用付款", denied: byDevice(p) ? "已拒絕（本人，已驗證裝置）" : "已拒絕（模擬核准者）",
      superseded: "報價已改變：這次核准作廢，需要重新請購", expired: "已過期，無法核准（後端已記錄）", withdrawn: withdrawnText(p) }[p.approval_closed]
      || (simulated() ? "等你核准（模擬核准；不會付款）" : "等你核准（本人以已登記的裝置；" + (pays ? "核准後由 Host 付款）" : "不會付款）")) + (reached ? "；已達期限，等待後端確認" : "");
  }
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
  const closing = p && (at("APPROVAL_EXPIRED") || at("APPROVAL_WITHDRAWN") || at("APPROVED") || at("APPROVAL_DENIED") || at("APPROVAL_SUPERSEDED"));
  const waiting = p && p.decision && p.decision.policy_verdict === "Ask" && !p.approval_closed;
  const pv = p && p.payment;
  const paying = !!(p && (pv || at("PAYMENT_REQUESTED")));
  // A pay task ended by its lifecycle or a Deny: its open Ask is decided by nobody now.
  const endedPay = pay(t) && (!!ended || denied(evs.filter((e) => e.event_type === "POLICY_DECIDED").map((e) => e.decision)));
  const steps = [
    step("進店", evs.find((e) => e.event_type === "TASK_STARTED"), "TASK_STARTED"),
    { label: "選商品", state: "none", note: hist ? "無 Store 事件；工具呼叫見查證面板（真實執行 trace）"
      : ctx.live ? "無 Store 事件；工具呼叫在 CLI 結束後見查證面板（真實 Agent）" : "無獨立事件（fake 腳本）" },
    { label: "報價", state: "none", note: (hist ? "無 Store 事件；工具呼叫見查證面板（真實執行 trace）；"
      : ctx.live ? "無 Store 事件；工具呼叫在 CLI 結束後見查證面板（真實 Agent）；" : "無獨立事件；") + "402 以 hash 綁定在請購" },
    step("請購", p && at("PURCHASE_PROPOSED"), "PURCHASE_PROPOSED"),
    step("裁決", p && at("POLICY_DECIDED"), p && p.decision ? str(p.decision.policy_verdict) : ""),
    closing || ended ? step("收尾", closing || ended, (closing || ended).event_type)
      : waiting && endedPay ? { label: "收尾", state: "none", note: ENDED_ASK }
      : waiting ? { label: "收尾", state: "wait", note: hist ? "執行結束時仍待核准（歷史快照，不是即時狀態；期限已過）" : pay(t) ? (simulated() ? "等你核准（模擬）" : "等你核准（本人裝置）") : "等待中（M1 無核准能力）" }
      : p && p.decision && p.decision.policy_verdict === "Deny" ? { label: "收尾", state: "none", note: "Deny 即結束；沒有收尾事件" }
      : p && p.decision && p.decision.policy_verdict === "Allow" ? { label: "收尾", state: "none", note: paying ? "Allow：由 Host 付款，見下列步驟"
        : paysOn(t.payment_capability) ? "尚未付款；付款開始後見下列步驟" : pay(t) ? "未啟用付款；沒有後續事件" : "M1 無付款；沒有後續事件" }
      : step("收尾", null),
    ...(paying ? paymentSteps(at, step, pv) : []),
  ];
  const cat = catalogFor(ctx.catalog, p);
  const rows = [];
  if (p) {
    rows.push(["402 綁定金額", amount(p.amount) + "（基本單位，" + str(p.asset_id) + "）", "Store"]);
    rows.push(["目錄標示", cat ? str(cat.price.asset_display) + "；" + str(cat.price.fiat_amount) + " " + str(cat.price.fiat_currency) + "（FX " + str(cat.price.fx_rate) + "，" + str(cat.price.fx_source) + "）" : "目錄未取得",
      "商店 HTTP /catalog（fixture，不綁定此請購" + (ctx.catalogAt ? "；瀏覽器讀取 " + ctx.catalogAt : "") + "）"]);
    if (p.decision) {
      rows.push(["裁決", str(p.decision.policy_verdict), live ? "Store" : "Store 事件（回放）"]);
      rows.push(["原因", reasonText(p.decision.reason, t), live ? "Store" : "Store 事件（回放）"]);
    }
    if (p.credential) rows.push(["憑證檢查", credentialText(p.credential), "Store 事件 CREDENTIAL_VERIFIED"]);
    if (typeof p.deadline_at === "number") rows.push(["期限", hhmmss(p.deadline_at), hist ? "Store 時間；當時的期限，早已過，不是倒數" : "Store 時間；裁決時決定"]);
    if (pv) {
      rows.push(["付款", paymentText(pv) + "（payment_status：" + str(pv.status) + "，state：" + str(pv.state) + "）", "Store"]);
      if (pv.reserve_tx) rows.push(["預算預留 tx#1", short(pv.reserve_tx), "Store（Z2 預留的鏈上交易）"]);
      if (pv.settle_tx) rows.push(["付款 tx#2", short(pv.settle_tx) + (typeof pv.settle_block === "number" ? "，區塊 " + pv.settle_block : ""), "Store（鏈上確認後才記下）"]);
      if (pv.receipt_id) rows.push(["收據", str(pv.receipt_id) + "；內容 sha256 " + short(pv.content_sha256), "Store（內容只交給 Host，不交給 Agent）"]);
    } else {
      rows.push(["付款", "未付款（payment_status：" + str(p.decision ? p.decision.payment_status : "unavailable") + "）", "Store"]);
    }
    if (p.approval && p.approval.grant) rows.push(["授權", grantText(p.approval.grant) + short(p.approval.grant.jti) + "（" + str(p.approval.grant.authorization_method) + "）："
      + (pv && p.approval.grant.authorization_method === "webauthn" ? "這筆付款的依據" : "不能付款"), "Store"]);
  }
  return {
    mode: live ? "live" : "replay", n: ctx.n, m: ctx.m,
    title: "客人 " + (CUSTOMER[t.consumer_id] || str(t.consumer_id)) + " · " + str(t.consumer_id) + " · " + str(t.task_id),
    po: po || null, scenePo: sc.po || null, differs: !!(po && sc.po && po !== sc.po), options: live ? ids : [],
    // C3: the host's next step about a payment that did not go through (live only: a replay or a record is not acted on).
    next: live && !hist ? paymentNext(pv) : "",
    steps, status: hist && p && p.decision && p.decision.policy_verdict === "Ask" && !p.approval_closed
      ? "Ask：執行結束時待人工核准（歷史快照；不是仍有效的核准）" : purchaseStatus(p, live, t.served_at, t, endedPay),
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
// failed: the latest read of the trace failed or timed out; reply is then
// the last one this page got, shown as such, never as the present.
function liveTools(reply, taskId, toolRows, failed) {
  const title = "工具呼叫（真實 Agent trace）";
  const note = "工具呼叫來自 CLI 結束後才寫出的白名單 trace，不是逐工具即時串流；請購與事件的對應由 Host 推導；不含模型文字。";
  const mine = !!reply && reply.task_id === taskId;
  if (failed) {
    if (!mine) return { title, note, rows: [["trace 更新", "trace 更新失敗：目前未確認", "網頁"]] };
    const last = liveTools(reply, taskId, toolRows, false).rows.map(([k, v, s]) =>
      [k, reply.state === "running" ? "上次結果：CLI 執行中（不代表目前仍在執行）" : v, s + "（上次結果）"]);
    return { title, note, rows: [["trace 更新", "trace 更新失敗：目前未確認；以下是上次取得的結果", "網頁"], ...last] };
  }
  if (!mine) return { title, note, rows: [["工具呼叫", "讀取中（未確認）", "—"]] };
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
      ...(ctx.venue ? [["情境", venue(ctx.venue).name + (venue(ctx.venue).demo ? "（示範情境・示意商品，沒有接 POS、庫存或物流）" : "") + "・交付：" + venue(ctx.venue).delivery, "Host 場景目錄"]] : []),
      ["資源", str(p.http_method) + " " + str(p.resource_url), "Store"], ["amount（基本單位）", amount(p.amount), "Store"],
      ["asset", str(p.asset_id), "Store"], ["network（chain）", str(p.chain_id), "Store"], ["payTo", str(p.pay_to), "Store"],
      ["payment_requirement_hash", str(p.payment_requirement_hash), "Store"],
      ["目錄價格", cat ? str(cat.price.asset_display) + "；" + str(cat.price.fiat_amount) + " " + str(cat.price.fiat_currency) : "目錄未取得", "商店 HTTP /catalog" + (ctx.catalogAt ? "（瀏覽器讀取 " + ctx.catalogAt + "）" : "")],
      ["FX", cat ? str(cat.price.fx_rate) + "（" + str(cat.price.fx_source) + "）" : "目錄未取得", "商店 HTTP /catalog"],
      ["完整 402 內容", "未提供（API 未開放；D4 暫緩）", "—"],
    ] : [] },
    { title: "請購", note: pay(t) ? str(t.contract_version) + "：intent_hash 一起涵蓋業務意圖與付款綁定（EIP-3009：收款人、金額、網路、資產、nonce、時間窗；agent-payment-authority/pay/" + (t.contract_version === V2 ? "v2" : "v1") + "）" + (paysOn(t.payment_capability) ? "；付款與交付見付款列。" : "；不會付款。")
      : "intent_hash 為 fixture domain（agent-payment-authority/m1-fixture/v2/verifier-not-deployed），不是真實付款綁定。", rows: p ? [
      ["purchase_order_id", str(p.purchase_order_id), "Store"], ["request_id", str(p.request_id), "Store"],
      ["intent_hash", str(p.intent_hash), "Store"], ["payment_requirement_hash", str(p.payment_requirement_hash), "Store"],
      ["PURCHASE_PROPOSED", proposed ? "#" + proposed.sequence + " · " + hhmmss(proposed.observed_at) : UNKNOWN, "Store 事件"],
    ] : [] },
    { title: "裁決", note: pay(t) ? (byDevice(p) || !(p && p.approval && p.approval.resolved_by) && !simulated()
      ? (paysOn(t.payment_capability) ? "固定原因代碼；核准由 Signer 驗證本人已登記的裝置；本人核准的 grant 由 Host 付款（" + PAYING[t.payment_capability] + "），付款與交付見付款列。"
        : "固定原因代碼；核准由 Signer 驗證本人已登記的裝置；grant 不能取得付款能力，沒有付款或交付。")
      : "固定原因代碼；核准是 fake 模擬（主持人扮演，不是本人驗證）；模擬 grant 不能取得付款能力，沒有付款或交付。")
      : "固定原因代碼；M1 沒有核准、付款或交付。", rows: p ? [
      ["verdict", str(d.policy_verdict), "Store"], ["reason", reasonText(d.reason, t), "Store"], ["approval_status", str(d.approval_status), "Store"],
      ["payment_status", str(d.payment_status), "Store"], ["decided_at", hhmmss(p.decided_at), "Store 時間"],
      ["deadline_at", typeof p.deadline_at === "number" ? hhmmss(p.deadline_at) : "—（非 Ask）", "Store 時間"],
      ["收尾", p.approval_closed ? str(p.approval_closed) + (p.closed_reason ? "（" + reasonText(p.closed_reason, t) + "）" : "") + " · " + hhmmss(p.closed_at) : hist ? "執行結束時尚未收尾（歷史快照）" : "尚未收尾", "Store"],
      ...(p.credential ? [
        ["憑證檢查", credentialText(p.credential), "Store 事件 CREDENTIAL_VERIFIED"],
        ["憑證細節", "issuer " + str(p.credential.issuer || "—") + " · 狀態索引 " + str(p.credential.status_index || "—") + " · 清單第 " + str(p.credential.status_list_version || "—")
          + " 版 · challenge " + short(p.credential.challenge_id || "—") + " · verifier " + str(p.credential.verifier), "Store 事件 CREDENTIAL_VERIFIED"],
      ] : []),
      ...(mine.filter((e) => e.purchase_order_id === po && /^VOICE_/.test(e.event_type)).map((e) => ["語音挑戰", voiceEventText(e), "Store 事件 " + e.event_type])),
      ...(p.approval ? [
        ["核准會話", str(p.approval.approval_session_id) + " · " + str(p.approval.state) + (p.approval.resolved_by ? " · " + str(p.approval.resolved_by) : ""), "Store"],
        ["grant", p.approval.grant ? str(p.approval.grant.jti) + " · " + str(p.approval.grant.authorization_method) + " · audience " + str(p.approval.grant.audience)
          + " · 到期 " + hhmmss(p.approval.grant.expires_at) + (p.approval.grant.authorization_method === "webauthn" ? (paysOn(t.payment_capability) ? "（本人核准；Host 依此付款）" : "（本人核准；付款未啟用，不能付款）") : "（模擬：Signer 樁拒絕，不能付款）") : "無", "Store"],
      ] : []),
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
    sections.push(liveTools(ctx.trace, t.task_id, toolRows, !!ctx.traceFailed));
  }
  // In step only when Unity's report names the projection sent: the same
  // generation, task, sequence and phase. Anything else is "not yet in
  // step", never a completed sync (review of 7ffc45d, R1).
  const sent = ctx.sent, rep = ctx.report;
  // Version 2: the report must be this customer's, of this slot (the
  // six-field rule, sameShown2); viewing an older task compares nothing.
  const diffs = [];
  if (sent && rep) {
    if (rep.gen !== sent.gen) diffs.push("世代不同（Unity gen " + rep.gen + "）");
    if (sent.consumer !== undefined && rep.consumer !== sent.consumer) diffs.push("客人不同（Unity " + str(rep.consumer) + "）");
    if (sent.slot !== undefined && rep.slot !== sent.slot) diffs.push("欄位世代不同（Unity " + rep.slot + "，送出 " + sent.slot + "）");
    if (rep.task !== sent.task) diffs.push("task 不同（Unity " + str(rep.task) + "）");
    if (rep.seq < sent.seq) diffs.push("序號較舊（Unity #" + rep.seq + "，送出 #" + sent.seq + "）");
    else if (rep.seq !== sent.seq) diffs.push("序號不同（Unity #" + rep.seq + "）");
    if (rep.phase !== sent.phase) diffs.push("phase 不同（Unity " + str(rep.phase) + "）");
  }
  const same = !!(sent && rep) && diffs.length === 0;
  const sync = ctx.viewOlder ? "你正在查看較早的 task；場景呈現的是 " + str(ctx.viewOlder) + "（查看與場景不同，不是同步失敗）"
    : !sent ? "尚未送出投影" : !rep ? "Unity 尚未回報：尚未一致" : same ? "Unity 已呈現 " + str(rep.task) + " #" + rep.seq + "（" + str(rep.phase) + "）"
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

// approvalTarget (P1-a, review of f95415d R1): what an approve, deny or
// evidence button of 「需要你處理」 acts on: the credentials generation, the
// simulated capability, the consumer, task, approval session and intent,
// and whether that session is still pending. The focus may come back to a
// rebuilt button only when every one of these is the same (sameTarget); a
// button that submits needs the session still pending. Anything else is
// another item, and the focus goes to the panel's heading, never to a
// button of it.
export function approvalTarget(gen, simId, item) {
  const p = (item && item.purchase) || {}, ap = p.approval || {};
  return { gen, sim: simId, consumer: (item && item.consumer_id) || "", task: (item && item.task_id) || "", session: ap.approval_session_id || "",
    intent: p.intent_hash || "", pending: ap.state === "pending" };
}
const TARGET_KEYS = ["gen", "sim", "consumer", "task", "session", "intent"];
export function sameTarget(was, now, submits) {
  return !!was && !!now && TARGET_KEYS.every((k) => String(was[k]) === String(now[k]) && now[k] !== "" && now[k] !== 0) && (!submits || now.pending);
}

// runCard (UX1, docs/commercial-ux1.md): what an Agent's card says of its
// newest run (null: none). Only a running run can be stopped; a stopping one
// still holds its place. tool_calls is the calls the host has answered,
// sent while the run runs or stops; anything else is unconfirmed or not
// given, never 0. runsError: the runs list could not be read now.
export function runCard(run, runsError) {
  const state = !run ? "idle" : run.live === "running" ? "running" : run.live === "stopping" ? "stopping" : "ended";
  const text = { idle: "未開始", running: "執行中", stopping: "停止中（仍占名額）", ended: "已結束" }[state];
  let calls = "", kind = "";
  if (state === "running" || state === "stopping") {
    if (runsError) [calls, kind] = ["已回覆工具呼叫：未確認（場次清單讀取失敗）", "unconfirmed"];
    else if (Number.isInteger(run.tool_calls) && run.tool_calls >= 0) [calls, kind] = ["已回覆工具呼叫 " + run.tool_calls + " 次", "known"];
    else [calls, kind] = ["已回覆工具呼叫：未確認", "unconfirmed"];
  } else if (state === "ended") [calls, kind] = ["工具呼叫數：場次結束後不提供", "not_provided"];
  return { state, text, calls, kind, stoppable: state === "running" };
}

// b64u and unb64u: base64url without padding, for WebAuthn's binary fields
// (A2: the Signer's options in, the browser's credential out).
export const b64u = (buf) => btoa(String.fromCharCode(...new Uint8Array(buf))).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
export const unb64u = (s) => Uint8Array.from(atob(String(s).replace(/-/g, "+").replace(/_/g, "/") + "===".slice((String(s).length + 3) % 4)), (c) => c.charCodeAt(0));

// ---- the audience window (docs/commercial-gap2.md): five steps in plain words ----

export const SHOW_STEPS = ["委託", "選購", "核准", "付款", "交付"]; // GD2
// PLAIN: a reason as the audience reads it; the codes stay on the host page.
export const PLAIN = {
  INSUFFICIENT_BUDGET: "超出預算", MERCHANT_NOT_ALLOWED: "不在允許的商店", ASSET_NOT_ALLOWED: "不是允許的幣別",
  CREDENTIAL_INVALID: "憑證檢查沒通過", CREDENTIAL_REVOKED: "憑證已撤銷", CREDENTIAL_STATUS_UNAVAILABLE: "憑證狀態無法確認",
  AUTHORIZATION_EXPIRED: "授權或報價已過期", CONFIG_MISSING: "缺少設定或憑證", TASK_NOT_ACTIVE: "任務已結束", CONTRACT_VERSION_MISMATCH: "版本不符",
  OWNERSHIP_MISMATCH: "不是這位客人的請購", PAYMENT_CAPABILITY_UNAVAILABLE: "這場展示不能付款", APPROVER_UNAVAILABLE: "沒有可核准的人",
  VOICE_CHALLENGE_FAILED: "唸的內容或聲音不符", APPROVER_DENIED: "核准者拒絕",
};
export const plainReason = (code) => PLAIN[code] || "其他原因（主持人頁有細節）";
// usdc: base units (6 decimals) as USDC, never rounded: at least two decimals, more when they matter.
export const usdc = (v) => (typeof v === "string" && /^[0-9]+$/.test(v) ? (Number(v) / 1e6).toFixed(6).replace(/(\.\d\d\d*?)0+$/, "$1") + " USDC" : "金額未提供");
const PAID = ["paying", "confirming", "paid", "delivered", "delivery_failed", "not_paid", "payment_review"];
// UNMOVED: the review codes whose result is known: no payment was made (payflow: never sent, or expired unused on chain).
// Not VERIFICATION_FAILED (review): that is the shop's word only, and a shop the Host does not run could still send it.
const UNMOVED = { RESERVED_NOT_PAID: "預算已預留，但付款沒有送出", NOT_SETTLED_EXPIRED: "付款授權已過期，鏈上沒有用過" };

// showCustomer: one customer's five steps for the audience (state: todo,
// now, done, fail, unknown, off) and the caption under the scene. Nothing
// reads as done before the Store says so; an unknown result is unknown.
// traits: the host's (authenticator: device | virtual).
export function showCustomer(consumer, snapshot, delegation, traits = {}) {
  const sc = scene(snapshot);
  const task = snapshot && snapshot.task;
  const p = task && sc.po ? (task.purchases || []).find((x) => x.purchase_order_id === sc.po) : null;
  const pays = !!task && paysOn(task.payment_capability);
  const steps = SHOW_STEPS.map((label) => ({ label, state: "todo", text: "" }));
  const set = (i, state, text) => Object.assign(steps[i], { state, text });
  const ph = sc.phase;
  const d = delegation;
  if (!task) set(0, "now", "等待開始");
  else {
    // Gap 4: who confirmed the delegation, by its DELEGATION_BOUND (the person's device, or the host for the customer).
    const bound = ((snapshot && snapshot.events) || []).find((e) => e.event_type === "DELEGATION_BOUND");
    const terms = d && d.task_budget ? "預算 " + usdc(d.task_budget) + "，超過 " + usdc(d.allow_max) + " 要問本人" : "預算與範圍";
    set(0, "done", !bound ? (d && d.task_budget ? "客人交代 Agent：" + terms : "客人已交代 Agent 預算與範圍")
      : bound.authorization_method === "webauthn" ? "客人用自己的裝置確認了委託：" + terms
      : "主持人代為確認委託（示範，不是本人）：" + terms);
    if (ph === "empty" || ph === "entered") set(1, "now", "Agent 正在挑選商品");
    else set(1, "done", "Agent 選好商品，報價 " + usdc(p && p.amount));
  }
  const g = p && p.approval && p.approval.grant;
  // approved: by the record's own method; a simulated approval is the host's, never the person's.
  const approved = () => (!byDevice(p) ? "主持人代為核准（示範，不是本人）"
    : approverText(traits.authenticator) + "核准了" + (g && g.voice === "verified" ? "，聲音也相符" : g && g.voice === "unavailable" ? "（語音無法使用，記為降級）" : ""));
  let decided = false; // the approval step let the purchase through
  if (sc.verdict === "Allow") { set(2, "done", "在委託範圍內，自動放行"); decided = true; }
  else if (sc.verdict === "Deny") set(2, "fail", plainReason(sc.reason) + "，已拒絕");
  else if (sc.verdict === "Ask") {
    if (ph === "waiting") set(2, "now", traits.authenticator ? "超過自動範圍，等本人核准"
      : isPay(task.contract_version) ? "超過自動範圍，等核准（示範：由主持人代為核准）" : "超過自動範圍；這場展示無法核准");
    else if (ph === "approved" || PAID.includes(ph)) { set(2, "done", approved()); decided = true; }
    else if (ph === "approval_denied") set(2, "fail", p && p.closed_reason === "VOICE_CHALLENGE_FAILED" ? "唸的內容或聲音不符，已拒絕" : "本人拒絕了");
    else if (ph === "expired") set(2, "fail", "核准期限已過");
    else if (ph === "superseded") set(2, "fail", "報價改變，要重新選購");
    else if (ph === "withdrawn") set(2, "fail", CREDENTIAL_CLOSED[p && p.closed_reason] || "已取消");
    else set(2, "unknown", "結果未確認");
  } else if (ph === "unknown") set(2, "unknown", "結果未確認");
  if (steps[2].state === "fail") set(3, "off", "這筆沒有付款（沒有花錢）");
  else if (decided && !pays) { set(3, "off", "這場展示不付款"); set(4, "off", "付款後才交付"); }
  else if (decided) {
    if (ph === "allowed" || ph === "approved") set(3, "now", READY);
    else if (ph === "paying") set(3, "now", "預留預算中（第 1 筆鏈上交易）");
    else if (ph === "confirming") set(3, "now", "已送出付款，等鏈上確認（第 2 筆）");
    else if (ph === "not_paid") set(3, "fail", "這筆沒有付款（沒有花錢）");
    else if (ph === "payment_review") {
      // By its code (Aaron 10/24): two are known, the money did not move; the shop's
      // refusal is said as the shop's; a chain that does not add up is for a person.
      const code = p && p.payment && p.payment.code;
      if (Object.hasOwn(UNMOVED, code)) set(3, "fail", "沒有付款（錢沒有動）：" + UNMOVED[code]);
      else if (code === "VERIFICATION_FAILED") set(3, "unknown", "商店回覆沒有接受、沒有送出（商店的說法，鏈上未確認）");
      else if (code === "RECONCILE_NEEDS_REVIEW") set(3, "unknown", "鏈上的付款結果對不起來，待人工確認");
      else set(3, "unknown", "付款結果未確認");
    }
    else set(3, "done", "已付款：預留與付款，兩筆鏈上交易");
    if (ph === "paid") set(4, "now", "付款完成，正在交付");
    else if (ph === "delivered") set(4, "done", "已交付，附收據");
    else if (ph === "delivery_failed") set(4, "fail", "已付款，交付失敗，待人工處理");
  }
  // A task's end stops what had not started; a payment already reserved goes on (payflow).
  if (sc.end) {
    const i = steps.findIndex((s) => s.state === "now");
    const going = i >= 3 && !(i === 3 && (ph === "allowed" || ph === "approved"));
    if (i >= 0 && going) steps[i].text += i === 4 ? "（任務已結束，交付照常進行）" : "（任務已結束，付款照常完成）";
    else if (i >= 0) Object.assign(steps[i], { state: "fail", text: sc.end === "cancelled" ? "任務已取消" : "任務逾時結束" });
  }
  const name = "客人 " + (CUSTOMER[consumer] || (/^consumer-[a-z0-9]+$/.test(consumer) ? consumer.slice(9).toUpperCase() : consumer));
  const last = [...steps].reverse().find((s) => s.state !== "todo" && s.state !== "off") || steps[0];
  return { consumer, name, steps, caption: name + "・" + last.label + "：" + last.text, amount: p ? usdc(p.amount) : "" };
}

const READY = "準備付款";
const QUEUED = "排在前一筆付款之後（Host 依序處理付款）";
// showQueue (batch 3, Aaron 10/24): customers shop at once, but the Host's
// payment worker is one loop (internal/payflow Run; docs/commercial-gap-review.md
// A4): it waits for each confirmation itself, moving on only once a wait gives
// up, and starts a payment at the top of a round (Before). So a payment still
// ready while another's is in progress is behind it, and the window says so
// (review 2); with one customer shopping, or no payment, nothing changes.
export function showQueue(models, cap) {
  if (models.filter((m) => m.steps[0].state === "done").length < 2 || !paysOn(cap)) return { models, note: "" };
  const ready = (m) => m.steps[3].state === "now" && m.steps[3].text === READY;
  const going = models.some((m) => (m.steps[3].state === "now" && !ready(m)) || m.steps[4].state === "now"); // delivering is the worker's too (review 3)
  return {
    note: "客人同時選購；付款由 Host 依序處理，一次處理一筆；一筆等鏈上確認太久時，會先處理下一筆。",
    models: models.map((m) => (going && ready(m) ? { ...m, steps: m.steps.map((s, i) => (i === 3 ? { ...s, text: QUEUED } : s)), caption: m.name + "・" + m.steps[3].label + "：" + QUEUED } : m)),
  };
}

// networkText: what the audience is told of the money, from the host's own capability.
export const networkText = (cap) => (cap === "base_sepolia" ? "Base Sepolia 測試網路・測試 USDC（不是真錢）"
  : cap === "local_anvil" ? "本機測試鏈・測試 USDC（不是真錢）" : "這場展示不付款");

// productName: the item a purchase names, by its resource path; the
// catalog's titles are English fixtures, so the audience's words are here.
const PRODUCTS = { "/resources/kr-market": "韓國市場簡報", "/scenes/cafe/lunch-set": "午餐套餐（12:30 取餐）", "/scenes/mart/coffee-beans": "辦公室咖啡豆 ×2" };
export function productName(url) {
  let path = "";
  try { path = new URL(url).pathname; } catch { return "商品"; }
  if (PRODUCTS[path]) return PRODUCTS[path];
  if (path.startsWith("/eval/")) return "評測資料（" + path.slice(6) + "）";
  return "商品（" + (path.split("/").pop() || "未命名") + "）";
}

// txLink: a transaction for the audience: short, with its public explorer
// page only on a public testnet; a local chain says it cannot be looked up.
export function txLink(cap, hash) {
  if (typeof hash !== "string" || !/^0x[0-9a-fA-F]{64}$/.test(hash)) return { text: "交易編號未提供", href: "" };
  const text = hash.slice(0, 10) + "…" + hash.slice(-6);
  return Object.hasOwn(EXPLORER, cap) ? { text, href: EXPLORER[cap] + hash } : { text: text + "（本機鏈，無法公開查詢）", href: "" };
}

const approvalWords = (p, traits) => {
  const g = p && p.approval && p.approval.grant;
  if (!byDevice(p)) return "主持人代為核准（示範，不是本人）";
  return approverText(traits.authenticator) + "核准" + (g && g.voice === "verified" ? "，聲音也相符" : g && g.voice === "unavailable" ? "（語音無法使用，記為降級）" : "");
};

// showEvidence (gap review B2): one customer's "why trustworthy" items, each
// only what a stored record says; nothing here says "verified" (the
// independent checker's report does, docs/commercial-e1a.md 4).
// chainLine (O2): one transaction as the Host read it from the chain.
export function chainLine(tx, head, kind) {
  if (!tx) return "";
  if (tx.status === "not_found") return "鏈上：還查不到這筆交易";
  if (tx.status === "reverted") return "鏈上：區塊 #" + tx.block + "，執行失敗";
  const at = "鏈上：區塊 #" + tx.block + (head >= tx.block ? "・" + (head - tx.block + 1) + " 個確認" : "");
  if (kind === "reserve" && tx.spent) return at + "・合約記下 " + usdc(tx.spent);
  if (kind === "settle" && tx.transfer) return at + "・" + usdc(tx.transfer.amount) + (tx.transfer.to_shop ? " 客人錢包→商店" : " 從客人錢包轉出（收款人不是這筆請購的商店）");
  return at;
}

export function showEvidence(snapshot, traits = {}, chain = null) {
  const task = snapshot && snapshot.task;
  if (!task) return [];
  const sc = scene(snapshot);
  const p = sc.po ? (task.purchases || []).find((x) => x.purchase_order_id === sc.po) : null;
  const items = [{ label: "委託", text: "主持人代客人確認（示範，不是本人）" }];
  if (!p) return items;
  if (p.credential) items.push({ label: "憑證", text: p.credential.result === "valid" ? "Agent 出示了客人的憑證，檢查通過（測試發行者）" : "憑證檢查沒通過" });
  if (sc.verdict === "Allow") items.push({ label: "核准", text: "在委託範圍內，自動放行" }); // the events' verdict, as the five steps
  else if (sc.verdict === "Deny") items.push({ label: "核准", text: plainReason(sc.reason) + "，已拒絕" });
  else if (p.approval && p.approval.state === "approved") items.push({ label: "核准", text: approvalWords(p, traits) });
  const pv = p.payment || {};
  const onChain = chain && chain.purchases && chain.purchases[p.purchase_order_id];
  if (pv.reserve_tx) items.push({ label: "預算預留", ...txLink(task.payment_capability, pv.reserve_tx), note: "第 1 筆・含 ZK 預算證明",
    chain: onChain && onChain.reserve && onChain.reserve.hash === pv.reserve_tx ? chainLine(onChain.reserve, chain.head, "reserve") : "" });
  if (pv.settle_tx) items.push({ label: "付款", ...txLink(task.payment_capability, pv.settle_tx),
    note: pv.settle_tx !== pv.reserve_tx ? "第 2 筆・和第 1 筆不是同一筆" : "第 2 筆（交易編號和第 1 筆相同，未確認）",
    chain: onChain && onChain.settle && onChain.settle.hash === pv.settle_tx ? chainLine(onChain.settle, chain.head, "settle") : "" });
  if (pv.receipt_id) items.push({ label: "收據", text: String(pv.receipt_id), note: "記錄了交付內容的 sha256" });
  return items;
}

// showReceipt (gap review B6): the receipt card once the Store says the
// purchase was delivered with a receipt; null before.
export function showReceipt(snapshot, traits = {}) {
  const task = snapshot && snapshot.task;
  const sc = scene(snapshot);
  const p = task && sc.po ? (task.purchases || []).find((x) => x.purchase_order_id === sc.po) : null;
  const pv = p && p.payment;
  if (sc.phase !== "delivered" || !pv || !pv.receipt_id) return null;
  const cap = task.payment_capability;
  return { title: "收據", rows: [
    { label: "商品", text: productName(p.resource_url) },
    { label: "金額", text: usdc(p.amount) + "（測試幣）" },
    { label: "網路", text: cap === "base_sepolia" ? "Base Sepolia 測試網路" : cap === "local_anvil" ? "本機測試鏈" : "不付款" },
    { label: "核准", text: sc.verdict === "Allow" ? "在委託範圍內，自動放行" : approvalWords(p, traits) },
    { label: "預算預留", ...txLink(cap, pv.reserve_tx) },
    { label: "付款", ...txLink(cap, pv.settle_tx) },
    { label: "兩筆交易", text: pv.reserve_tx && pv.settle_tx && pv.reserve_tx !== pv.settle_tx ? "不是同一筆，也不是一次完成" : "未確認" },
    { label: "收據編號", text: String(pv.receipt_id) },
    { label: "內容摘要", text: typeof pv.content_sha256 === "string" && pv.content_sha256 ? "sha256 " + pv.content_sha256.replace(/^sha256:/, "").slice(0, 12) + "…" : "未提供" },
  ] };
}

// ---- the checker's report on the audience window (docs/commercial-gap2.md GD4-A) ----

const VERDICTS = { PASS: "通過", FAIL: "未通過", INCONCLUSIVE: "無法判定", NOT_EVALUATED: "未評估" };
// CLAIMS: E1-E10 as an audience reads them (docs/commercial-e1a.md 4.5).
export const CLAIMS = {
  E1: "證據檔案完整、沒有被改動", E2: "每一步都對到同一筆請購", E3: "事件紀錄前後相連、順序正確", E4: "兩筆鏈上交易與 ZK 預算證明（向鏈查證）",
  E5: "結果未知時沒有重複付款", E6: "每筆紀錄如實標示是模擬或真實", E7: "兩位客人的身分與錢包互不混用", E8: "期間沒有未經授權的付款（向鏈重算）",
  E9: "證據只含允許的欄位，祕密掃描通過", E10: "商店誘導文字的測試", // E10 passes with no finding; whether the model was fooled is the report's injections
};
const OUTSIDE = [["BBS+", "憑證證明由 checker 重新驗算"], ["voice", "語音的誤判率"], ["Admin", "Admin 彙總"], ["delegation", "委託由本人確認"]];

// parseChecker: a report of `verify-demo --mode e1a`, read as data: only
// its fixed fields, every claim E1-E10 with a known verdict; else an error.
export function parseChecker(text) {
  let r;
  try { r = JSON.parse(text); } catch { return { error: "不是 JSON" }; }
  if (!r || r.mode !== "e1a") return { error: "不是 e1a 的 checker 報告" };
  if (typeof r.run_id !== "string" || !/^[A-Za-z0-9._-]{1,80}$/.test(r.run_id)) return { error: "缺少證據包編號（run_id）" };
  if (typeof r.verdict !== "string" || !Object.hasOwn(VERDICTS, r.verdict)) return { error: "總結果不明" };
  const claims = {};
  for (const id of Object.keys(CLAIMS)) {
    const v = r.claims && r.claims[id];
    if (typeof v !== "string" || !Object.hasOwn(VERDICTS, v)) return { error: id + " 的結果不明" };
    claims[id] = v;
  }
  const c = r.checker || {};
  const checker = [c.name, c.version].filter((x) => typeof x === "string" && x).join(" ").slice(0, 80) || "checker";
  const notEvaluated = (Array.isArray(r.not_evaluated) ? r.not_evaluated : []).filter((x) => typeof x === "string").slice(0, 10);
  const fooled = (Array.isArray(r.injections) ? r.injections : []).filter((x) => x && typeof x.fooled === "boolean").map((x) => x.fooled).slice(0, 10);
  // findings: the claims that did not pass, with the checker's reason for the
  // host page only (capped); the window gets the claim and whether the chain was asked.
  const findings = (Array.isArray(r.findings) ? r.findings : [])
    .filter((x) => x && typeof x.claim === "string" && Object.hasOwn(CLAIMS, x.claim) && typeof x.verdict === "string" && x.verdict !== "PASS")
    .slice(0, 20).map((x) => ({ claim: x.claim, verdict: x.verdict, reason: typeof x.reason === "string" ? x.reason.slice(0, 200) : "", noChain: typeof x.reason === "string" && x.reason.startsWith("no --rpc") }));
  return { report: { run_id: r.run_id, verdict: r.verdict, claims, checker, not_evaluated: notEvaluated, fooled, findings } };
}

// showChecker: the report as the audience reads it; the page says only what the report says.
export function showChecker(r) {
  if (!r) return null;
  const noChain = new Set((r.findings || []).filter((f) => f.noChain).map((f) => f.claim));
  const items = Object.keys(CLAIMS).map((id) => ({ id, label: CLAIMS[id], state: r.claims[id],
    text: VERDICTS[r.claims[id]] + (r.claims[id] !== "PASS" && noChain.has(id) ? "（沒有向鏈查詢）" : "") }));
  const off = items.filter((x) => x.state !== "PASS");
  // All passed only when every claim is PASS (docs/commercial-e1a.md 4.5: a required claim counts only as PASS).
  const summary = r.verdict === "PASS" && !off.length ? "逐項檢查，全部通過"
    : !off.length ? "總結果：" + VERDICTS[r.verdict] + "（主持人頁有細節）"
    : "有 " + off.length + " 項" + (off.some((x) => x.state === "FAIL") ? "未通過" : off.every((x) => x.state === "NOT_EVALUATED") ? "未評估" : "無法判定") + "（主持人頁有細節）";
  const outside = r.not_evaluated.map((s) => (OUTSIDE.find(([k]) => s.includes(k)) || [, "其他項目"])[1]);
  const n = (r.fooled || []).filter(Boolean).length;
  const injection = !(r.fooled || []).length ? "這個證據包沒有商店誘導文字的測試" : n ? "商店誘導文字的測試：模型被騙了 " + n + " 次（有沒有多付錢見 E8）"
    : "商店誘導文字的測試：模型沒有被騙（有沒有多付錢見 E8）";
  // The report is of an evidence pack, which need not be the session on screen (review S3-4).
  return { title: "獨立驗證：證據包 " + r.run_id + " 的結果（不一定是畫面上這一場）", by: "由 " + r.checker + " 在場次後產生；本頁只顯示它的報告", summary, items, injection,
    outside: outside.length ? "不在這次驗證範圍：" + outside.join("、") : "" };
}
