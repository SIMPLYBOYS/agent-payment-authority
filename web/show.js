// show.js: the audience window (docs/commercial-gap2.md). Its Unity shows
// the scene the host page sends; beside it each customer's five steps, in
// plain words (adapter.js showCustomer). No token, no API call: everything
// comes from the host page over showlink.js.

import { UnityHost } from "./unity.js";
import * as A from "./adapter.js";
import { listen } from "./showlink.js";

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
});
const tell = () => link.status({ ready: unity.ready, supports2: unity.supports2, select: !!(unity.bridge && unity.bridge.select) });
unity = new UnityHost($("unity-canvas"), (s, d) => {
  if (s === "rendered2" || s === "select2") return link.report(s, d);
  const box = $("unity-status");
  box.dataset.state = s;
  box.textContent = s === "ready" ? "" : s === "loading" ? "場景載入中…" : "場景未就緒";
  tell();
});

function render() {
  const v = lastView;
  const there = link.hostThere;
  document.body.dataset.host = there ? "on" : "off";
  if (link.otherWindow) { $("show-network").textContent = "另一個展示視窗正在使用；請關閉這一個"; return; }
  if (!v) return;
  $("show-title").textContent = v.venue ? A.venue(v.venue).name : "示範商店";
  $("show-network").textContent = A.networkText(v.payments) + (there ? "" : "・主持人頁未連線（停在最後狀態）");
  const shown = (v.customers || []).map((c) => A.showCustomer(c.consumer, c.snapshot, c.delegation, v));
  // Redrawn only when something changed: the captions are a live region.
  const key = JSON.stringify([shown, owned]);
  if (key === shownKey) return;
  shownKey = key;
  if (!owned) $("show-captions").replaceChildren(el("p", "", "場景暫停：主持人頁正在回放或切換"));
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
  }));
  if (owned) $("show-captions").replaceChildren(...shown.map((m) => el("p", "", m.caption)));
}
setInterval(render, 2000); // the host page's presence, even when nothing changes
unity.load();
