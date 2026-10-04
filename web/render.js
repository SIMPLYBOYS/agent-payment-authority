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

// renderTimeline lists the events; each opens to its whitelisted JSON, as text.
export function renderTimeline(root, rows) {
  root.replaceChildren();
  if (rows.length === 0) {
    root.append(el("p", "empty", "尚無事件"));
    return;
  }
  const ol = el("ol", "timeline");
  for (const r of rows) {
    const li = el("li");
    li.dataset.seq = String(r.seq);
    const head = el("button", "event-head");
    head.type = "button";
    head.append(el("span", "seq", "#" + r.seq), el("span", "type", r.type), el("span", "detail", r.detail), el("span", "time", r.time), el("span", "source", r.source));
    const body = el("div", "event-json");
    body.hidden = true;
    body.append(el("p", "json-note", "API 白名單 JSON（不是 CLI 原始輸出，也不是完整未遮罩 raw log）"), el("pre", null, r.json));
    head.addEventListener("click", () => { body.hidden = !body.hidden; });
    li.append(head, body);
    ol.append(li);
  }
  root.append(ol);
}

// renderScene writes the shop status line under the Unity canvas: the
// committed phase label and its notes, never a claim of payment.
export function renderSceneText(root, sc) {
  root.replaceChildren(el("p", "scene-label", sc.label));
  root.dataset.phase = sc.phase;
  for (const n of sc.notes) root.append(el("p", "scene-note", n));
}
