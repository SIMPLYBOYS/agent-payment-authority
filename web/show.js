// show.js: the audience window (docs/commercial-gap2.md). Its Unity shows
// the scene the host page sends; beside it each customer's five steps, in
// plain words (adapter.js showCustomer). No token, no API call: everything
// comes from the host page over showlink.js.

import { UnityHost } from "./unity.js";
import * as A from "./adapter.js";
import { listen } from "./showlink.js";
import { attachZoom, roomPixels } from "./pixelscale.js";

const $ = (id) => document.getElementById(id);
const el = (tag, cls, text) => {
  const x = document.createElement(tag);
  if (cls) x.className = cls;
  if (text !== undefined) x.textContent = text;
  return x;
};

let unity = null;
let lastView = null;
let owned = false;
let shownKey = "";
// T4 (Aaron 10/30: 依階段切換): the town fills the window with one line; in a venue, the scene, the steps and the evidence.
let phase = "venue", walking = "";
const link = listen({
  own: (on) => {
    if (link.otherWindow) return; // the host page follows another window: this one shows nothing of it
    // A host page that loaded again counts its scenes from 0: a version 1
    // clear first makes Unity start the next version 2 scene afresh (review 1).
    if (on) unity.post(A.clearMessage(0));
    unity.own(on);
    owned = on;
    render();
  },
  send2: (msg) => { if (!link.otherWindow) unity.send2(msg); },
  select2: (msg) => { if (!link.otherWindow) unity.select2(msg); },
  view: (v) => { lastView = v; render(); },
  cam: (mode, safe) => {
    if (link.otherWindow) return;
    phase = mode;
    document.body.dataset.phase = mode;
    unity.view(mode, safe);
    zoom.apply();
    render();
  },
  play: (c) => { if (!link.otherWindow) { walking = c; unity.play(c); render(); } },
  steer: (x, y) => { if (!link.otherWindow) unity.steer(x, y); },
  tour: () => { if (!link.otherWindow) unity.tour(); },
  gates: (on) => { if (!link.otherWindow) unity.gates(on); },
  openGate: (msg) => { if (!link.otherWindow) unity.openGate(msg); },
});
const tell = () => link.status({ ready: unity.ready, supports2: unity.supports2, select: !!(unity.bridge && unity.bridge.select) });
unity = new UnityHost($("unity-canvas"), (s, d) => {
  if (s === "rendered2" || s === "select2" || s === "venue" || s === "gate") return link.report(s, d); // venue: a door reached (T4); gate: a shut gate (transit)
  const box = $("unity-status");
  box.dataset.state = s;
  box.textContent = s === "ready" ? "" : s === "loading" ? "場景載入中…" : "場景未就緒";
  tell();
});

// anchor: a public explorer page, opened apart from this window.
function anchor(text, href) {
  const a = el("a", "", text);
  a.href = href;
  a.target = "_blank";
  a.rel = "noopener noreferrer";
  return a;
}
const wrap = (...x) => { const dd = el("dd", ""); dd.append(...x); return dd; };
// parts: a row of named addresses, each its explorer page where it has one.
const parts = (r) => r.parts.flatMap((x, i) => [...(i ? [r.sep] : []), x.name + " ", x.href ? anchor(x.text, x.href) : x.text]);

function render() {
  const v = lastView;
  const there = link.hostThere;
  document.body.dataset.host = there ? "on" : "off";
  if (link.otherWindow) { $("show-network").textContent = "另一個展示視窗正在使用；請關閉這一個"; return; }
  if (!v) return;
  $("show-title").textContent = phase === "town" ? "城鎮" : v.venue ? A.venue(v.venue).name : "示範商店";
  $("show-network").textContent = A.networkText(v.payments) + (there ? "" : "・主持人頁未連線（停在最後狀態）");
  const queue = A.showQueue((v.customers || []).map((c) => ({ ...A.showCustomer(c.consumer, c.snapshot, c.delegation, v),
    evidence: A.showEvidence(c.snapshot, v, c.chain), receipt: A.showReceipt(c.snapshot, v, c.chain) })), v.payments);
  const shown = queue.models;
  // Redrawn only when something changed: the captions are a live region.
  const chk = A.showChecker(v.checker);
  const key = JSON.stringify([shown, queue.note, owned, chk, phase, walking]);
  if (key === shownKey) return;
  shownKey = key;
  if (!owned) $("show-captions").replaceChildren(el("p", "", "場景暫停：主持人頁正在回放或切換"));
  else if (phase === "town") $("show-captions").replaceChildren(el("p", "show-town-caption", A.showTownCaption(walking)));
  const box = $("show-customers");
  if (!shown.length) box.replaceChildren(el("p", "show-wait", "還沒有客人進店。"));
  else box.replaceChildren(...shown.map((m) => {
    const card = el("section", "show-card");
    card.dataset.consumer = m.consumer;
    card.append(el("h2", "", m.name + " 的 Agent" + (m.amount ? "・" + m.amount : "")));
    const ol = el("ol", "show-steps");
    m.steps.forEach((s, i) => {
      const li = el("li", "show-step");
      li.dataset.state = s.state;
      li.append(el("b", "", (i + 1) + " " + s.label), el("span", "", s.text));
      ol.append(li);
    });
    card.append(ol);
    return card;
  }), ...(queue.note ? [el("p", "show-queue", queue.note)] : []));
  if (owned && phase !== "town") $("show-captions").replaceChildren(...shown.map((m) => el("p", "", m.caption)));
  queueMicrotask(() => { zoom.apply(); place(); }); // the evidence below may have changed the room the scene has
  $("show-trust").replaceChildren(...shown.filter((m) => m.evidence.length).map((m) => {
    const row = el("div", "show-trust-row");
    row.dataset.consumer = m.consumer;
    row.append(el("h3", "", m.name + "・為什麼可信"));
    const ul = el("ul", "");
    for (const it of m.evidence) {
      const li = el("li", "");
      li.append(el("b", "", it.label), it.href ? anchor(it.text, it.href) : el("span", "", it.text));
      if (it.note) li.append(el("small", "", it.note));
      if (it.chain) li.append(el("small", "show-chain", it.chain));
      ul.append(li);
    }
    row.append(ul);
    return row;
  }));
  const cb = $("show-checker");
  cb.hidden = !chk;
  if (chk) {
    const ol = el("ol", "");
    for (const it of chk.items) {
      const li = el("li", "");
      li.dataset.state = it.state;
      li.append(el("b", "", it.id + " " + it.text), el("span", "", it.label));
      ol.append(li);
    }
    cb.replaceChildren(el("h3", "", chk.title), el("p", "show-checker-sum", chk.summary), ol, ...(chk.injection ? [el("p", "show-checker-sum", chk.injection)] : []), el("p", "show-checker-by", chk.by + (chk.outside ? "。" + chk.outside : "")));
  }
  $("show-receipts").replaceChildren(...shown.filter((m) => m.receipt).map((m) => {
    const card = el("section", "show-receipt");
    card.dataset.consumer = m.consumer;
    card.append(el("h3", "", m.name + "・" + m.receipt.title));
    const dl = el("dl", "");
    for (const r of m.receipt.rows) dl.append(el("dt", "", r.label), r.parts ? wrap(...parts(r)) : r.href ? wrap(anchor(r.text, r.href)) : el("dd", "", r.text));
    card.append(dl);
    return card;
  }));
}
// The scene at whole multiples of its pixels (pixelscale.js): what fits above the caption and the evidence.
const frame = document.querySelector(".show-frame");
const zoom = attachZoom({ canvas: $("unity-canvas"), frame, controls: $("scene-zoom"), key: "apa-zoom-show", fill: () => phase === "town", native: () => roomPixels(unity),
  avail: () => ({ w: frame.parentElement.clientWidth, h: innerHeight - frame.getBoundingClientRect().top - $("show-captions").offsetHeight - $("show-trust").offsetHeight - 12 }) });
unity.onRoom = () => { zoom.apply(); place(); }; // interiors v2: another room may be another size
// place: the receipt over the scene when the scene is big enough (2× and up),
// else in the side column; the side column never makes the row taller than the scene.
function place() {
  const big = Number($("unity-canvas").dataset.scale) >= 2;
  const r = $("show-receipts"), side = document.querySelector(".show-side");
  const want = big ? frame : side;
  if (r.parentElement !== want) want.append(r);
  r.classList.toggle("overlay", big);
  side.style.maxHeight = document.querySelector(".show-stage").offsetHeight + "px";
}
addEventListener("resize", () => queueMicrotask(place));
if (typeof ResizeObserver === "function") new ResizeObserver(() => queueMicrotask(place)).observe(document.documentElement);
place();
setInterval(render, 2000); // the host page's presence, even when nothing changes
unity.load();
