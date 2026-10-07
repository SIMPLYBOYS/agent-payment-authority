// render.js: the Dashboard's DOM output (Inspector, Timeline, status). Every
// value is set with textContent: nothing from the API is parsed as HTML,
// used as a URL or loaded as an image. The shop is Unity's (unity.js).

import { UNKNOWN } from "./adapter.js";

function el(tag, cls, text) {
  const n = document.createElement(tag);
  if (cls) n.className = cls;
  if (text !== undefined) n.textContent = text;
  return n;
}

export function renderInspector(root, rows) {
  root.replaceChildren();
  if (rows.length === 0) {
    root.append(el("p", "empty", "尚無資料"));
    return;
  }
  const dl = el("dl", "inspector");
  for (const [k, v] of rows) dl.append(el("dt", null, k), el("dd", v.startsWith(UNKNOWN) ? "unknown" : null, v));
  root.append(dl);
}

// renderTimeline lists the events; each opens to its whitelisted JSON, as
// text. expanded holds the event_ids the viewer opened: a redraw (a poll, a
// new event, a failed read) keeps them open and keeps closed ones closed;
// the caller clears the set when the viewer or task changes.
// In a replay, at is the replay position: the Timeline stays the full
// record, says so, and marks the events the replay has not reached.
export function renderTimeline(root, rows, expanded = new Set(), at = null) {
  root.replaceChildren();
  if (rows.length === 0) {
    root.append(el("p", "empty", "尚無事件"));
    return;
  }
  if (at) root.append(el("p", "full-note", "完整紀錄，非回放當步（回放位置 #" + at.n + "／共 " + at.m + "）"));
  const ol = el("ol", "timeline");
  for (const r of rows) {
    const li = el("li");
    li.dataset.seq = String(r.seq);
    if (at && r.seq > at.n) { li.dataset.ahead = "1"; li.title = "回放尚未到此"; }
    const head = el("button", "event-head");
    head.type = "button";
    head.append(el("span", "seq", "#" + r.seq), el("span", "type", r.type), el("span", "detail", r.detail), el("span", "time", r.time), el("span", "source", r.source));
    const body = el("div", "event-json");
    body.hidden = !expanded.has(r.id);
    body.append(el("p", "json-note", "API 白名單 JSON（不是 CLI 原始輸出，也不是完整未遮罩 raw log）"), el("pre", null, r.json));
    head.setAttribute("aria-expanded", String(!body.hidden));
    head.addEventListener("click", () => {
      body.hidden = !body.hidden;
      if (body.hidden) expanded.delete(r.id); else expanded.add(r.id);
      head.setAttribute("aria-expanded", String(!body.hidden));
    });
    li.append(head, body);
    ol.append(li);
  }
  root.append(ol);
}

// renderScene writes the shop status line under the Unity canvas: the
// committed phase label and its notes, never a claim of payment.
export function renderSceneText(root, sc, motion = "") {
  root.replaceChildren(el("p", "scene-label", sc.label));
  root.dataset.phase = sc.phase;
  root.dataset.motion = motion; // first | moving | shown | waiting | "" (D2, for the browser test)
  for (const n of sc.notes) root.append(el("p", "scene-note", n));
}

// renderCard is the purchase card (docs/demo-presentation-design.md 4.4,
// 4.5). on.pick(po) and on.back() are the hand pick of a purchase (live).
export function renderCard(root, c, on) {
  root.replaceChildren();
  delete root.dataset.mode;
  delete root.dataset.po;
  if (!c) {
    root.append(el("p", "empty", "尚未選擇 task"));
    return;
  }
  root.dataset.mode = c.mode;
  root.dataset.po = c.po || "";
  if (c.mode === "replay") root.append(el("p", "card-replay", "回放當步 #" + c.n + "／共 " + c.m + "（唯讀）"));
  root.append(el("p", "card-title", c.title), el("p", "card-po", c.po ? "請購 " + c.po : "尚無請購"));
  if (c.differs) {
    const d = el("p", "card-differs", "你正在查看 " + c.po + "；商店場景呈現的是 " + c.scenePo + " ");
    const back = el("button", "card-back", "回到場景這筆");
    back.type = "button";
    back.addEventListener("click", () => on.back());
    d.append(back);
    root.append(d);
  }
  const ol = el("ol", "steps");
  c.steps.forEach((st, i) => {
    const li = el("li", null, "①②③④⑤⑥"[i] + st.label);
    li.dataset.state = st.state;
    li.append(el("span", "step-note", st.note));
    ol.append(li);
  });
  root.append(ol, el("p", "card-status", c.status), el("p", "card-task", c.task));
  const dl = el("dl", "card-rows");
  for (const [k, v, src] of c.rows) {
    const dd = el("dd", null, v);
    dd.append(el("span", "src", src));
    dl.append(el("dt", null, k), dd);
  }
  root.append(dl);
  if (c.options.length > 1) {
    const sel = el("select", "po-pick");
    sel.setAttribute("aria-label", "其他請購");
    for (const id of c.options) {
      const o = el("option", null, id === c.scenePo ? id + "（場景這筆）" : id);
      o.value = id;
      o.selected = id === c.po;
      sel.append(o);
    }
    sel.addEventListener("change", () => on.pick(sel.value));
    const label = el("label", "po-pick-label", "其他請購 ▾ ");
    label.append(sel);
    root.append(label);
  }
}

// renderEvidence is the evidence panel (section 7): a banner, then one
// block per page, each value with its source.
export function renderEvidence(root, e) {
  root.replaceChildren();
  if (!e) {
    root.append(el("p", "empty", "尚未選擇 task"));
    return;
  }
  root.append(el("p", "evidence-banner", e.banner));
  for (const sec of e.sections) {
    const box = el("section", "evidence-page");
    box.dataset.page = sec.title;
    box.append(el("h3", null, sec.title), el("p", "evidence-note", sec.note));
    const dl = el("dl", "card-rows");
    if (sec.rows.length === 0) dl.append(el("dt", null, "—"), el("dd", "unknown", "此 task 尚無請購"));
    for (const [k, v, src] of sec.rows) {
      const dd = el("dd", null, v);
      dd.append(el("span", "src", src));
      dl.append(el("dt", null, k), dd);
    }
    box.append(dl);
    root.append(box);
  }
}
