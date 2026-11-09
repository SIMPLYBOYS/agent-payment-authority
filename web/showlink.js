// showlink.js: the host page and the audience window (/show) on one
// computer (docs/commercial-gap2.md, GD1-A). The host page keeps every token
// and every read; it sends the audience window the scene's Unity messages
// and what each customer's slot has read (task and events, never a token).
// The audience window shows them on its own Unity and sends back what Unity
// reports. A BroadcastChannel reaches this origin's pages only.
//
// ponytail: one channel for the machine; a second host page would talk to
// the same window. One host page per demo, as it is used.

export const CHANNEL = "apa-show-v1";
const BEAT_MS = 2000; // the audience window says it is there this often
const GONE_MS = 6000; // and is gone after this long without a word

const newId = () => Math.random().toString(36).slice(2, 10);

// createRelay is the host page's side: a stand-in for UnityHost
// (ready, supports2, own, send2, select2) that forwards to the audience
// window. onStatus(kind, detail): "rendered2" and "select2" as UnityHost
// reports them, "show" when the window came, went or loaded again.
export function createRelay(onStatus, { channel = new BroadcastChannel(CHANNEL), now = () => Date.now(), setTimer = setInterval } = {}) {
  let show = null; // { id, ready, supports2, select, seen }
  let owned = false;
  let lastView = "";
  const post = (kind, body = {}) => channel.postMessage({ from: "host", kind, ...body });
  channel.onmessage = (e) => {
    const m = e.data;
    if (!m || m.from !== "show" || typeof m.id !== "string") return;
    if (m.kind === "bye") {
      if (show && show.id === m.id) { show = null; onStatus("show", null); }
      return;
    }
    if (m.kind === "status") {
      // One window at a time: another one waits while the followed one is alive (review 2).
      if (show && show.id !== m.id && now() - show.seen <= GONE_MS) { post("busy", { to: m.id }); return; }
      const was = show;
      show = { id: m.id, ready: !!m.ready, supports2: !!m.supports2, select: !!m.select, seen: now() };
      post("ack", { to: m.id }); // the window knows a host page is there
      // A new window, or one whose Unity just became ready, gets the scene again.
      if (!was || was.id !== m.id || was.ready !== show.ready || was.supports2 !== show.supports2) { lastView = ""; onStatus("show", show); }
      return;
    }
    if (!show || show.id !== m.id) return; // reports of a window this page does not follow
    if (m.kind === "rendered2" && owned) onStatus("rendered2", m.r);
    else if (m.kind === "select2" && owned) onStatus("select2", m.r);
    else if (m.kind === "venue" && owned && typeof m.r === "string") onStatus("venue", m.r); // T4: a door reached on the window's Unity
    else if (m.kind === "gate" && owned && typeof m.r === "string") onStatus("gate", m.r); // transit: a shut gate reached there
  };
  setTimer(() => {
    if (show && now() - show.seen > GONE_MS) { show = null; onStatus("show", null); }
  }, BEAT_MS);
  post("hello"); // a window already open says who it is
  return {
    get connected() { return !!show; },
    get ready() { return !!show && show.ready; },
    get supports2() { return !!show && show.supports2; },
    get sceneOwned() { return owned; },
    own(on) { owned = on; post("own", { on }); },
    send2(msg) { if (owned) post("send2", { msg }); },
    select2(msg) { if (owned && show && show.select) post("select2", { msg }); },
    // T4 (docs/commercial-town.md): the town's camera and the walked customer, on the window's Unity as on this page's.
    cam(mode, safe) { if (owned) post("cam", { mode, safe: safe || null }); },
    play(consumer) { if (owned) post("play", { consumer: consumer || "" }); },
    steer(x, y) { if (owned) post("steer", { x, y }); },
    tour() { if (owned) post("tour"); },
    // transit (docs/commercial-transit.md): the paid gates on or off, and the answer to a gate reached.
    gates(on) { if (owned) post("gates", { on: !!on }); },
    openGate(msg) { if (owned) post("opengate", { msg }); },
    // view: what the audience window shows beside the scene; sent when it changed.
    view(v) {
      const key = JSON.stringify(v);
      if (key === lastView || !show) return;
      lastView = key;
      post("view", { view: v });
    },
  };
}

// listen is the audience window's side. handlers: own(on), send2(msg),
// select2(msg), view(v), and (T4) cam(mode, safe), play(consumer), steer(x, y), (town v2 V4) tour(). It returns report(kind, r) for Unity's reports and
// status(s) for this window's Unity state ({ ready, supports2, select }).
export function listen(handlers, { channel = new BroadcastChannel(CHANNEL), setTimer = setInterval, onLeave = (f) => addEventListener("pagehide", f), now = () => Date.now() } = {}) {
  const id = newId();
  let state = { ready: false, supports2: false, select: false };
  let hostSeen = 0;
  let busy = 0; // when the host page last said it follows another window
  const post = (kind, body = {}) => channel.postMessage({ from: "show", id, kind, ...body });
  channel.onmessage = (e) => {
    const m = e.data;
    if (!m || m.from !== "host") return;
    if (m.to !== undefined && m.to !== id) return; // addressed to another window
    hostSeen = now();
    if (m.kind === "busy") { busy = now(); return; }
    if (m.kind === "ack") busy = 0;
    if (m.kind === "hello") post("status", state);
    else if (m.kind === "own") handlers.own(!!m.on);
    else if (m.kind === "send2" && m.msg) handlers.send2(m.msg);
    else if (m.kind === "select2" && m.msg) handlers.select2(m.msg);
    else if (m.kind === "view" && m.view) handlers.view(m.view);
    else if (m.kind === "cam" && (m.mode === "town" || m.mode === "venue") && handlers.cam) handlers.cam(m.mode, m.safe || null);
    else if (m.kind === "play" && typeof m.consumer === "string" && handlers.play) handlers.play(m.consumer);
    else if (m.kind === "tour" && handlers.tour) handlers.tour();
    else if (m.kind === "gates" && handlers.gates) handlers.gates(!!m.on);
    else if (m.kind === "opengate" && m.msg && handlers.openGate) handlers.openGate(m.msg);
    else if (m.kind === "steer" && handlers.steer) handlers.steer(Math.sign(Number(m.x) || 0), Math.sign(Number(m.y) || 0));
  };
  setTimer(() => post("status", state), BEAT_MS);
  onLeave(() => post("bye"));
  post("status", state);
  return {
    status(s) { state = { ready: !!s.ready, supports2: !!s.supports2, select: !!s.select }; post("status", state); },
    report(kind, r) { post(kind, { r }); },
    // hostThere: a host page answered within GONE_MS.
    get hostThere() { return hostSeen > 0 && now() - hostSeen <= GONE_MS; },
    // otherWindow: the host page follows another audience window.
    get otherWindow() { return busy > 0 && now() - busy <= GONE_MS; },
  };
}
