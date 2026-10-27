// unity.js: the Unity shop on the page (docs/demo-ui.md section 4). The Web
// build is served by the demo host under /unity/; this module loads it,
// sends it projections and reads its reports. Unity never gets a token and
// never reads the API.
//
// Two ways to feed it. Version 1 (send): one customer per generation, the
// Dashboard's own (the viewer page, history replay). Version 2 (send2): the
// host page's scene with a slot per customer (scene.js;
// docs/multi-agent-demo-design.md 5); while the scene owns the shop
// (sceneOwned), version 1 sends do nothing and version 2 reports go to it.

import { clearMessage, parseBridge, parseReport2 } from "./adapter.js";

export class UnityHost {
  constructor(canvas, onStatus) {
    this.canvas = canvas;
    this.onStatus = onStatus; // (state, detail): loading | ready | failed | rendered | rendered2
    this.instance = null;
    this.ready = false;
    this.gen = -1;
    this.pending = null; // version 1: the latest message only, while Unity is not ready
    this.art = "";
    this.bridge = null;  // the build's bridge report: { versions, customers }; null for a build without one
    this.sceneOwned = false;
    this.pendingScene = null;       // version 2: the latest scene clear, while not ready
    this.pending2 = new Map();      // version 2: the latest message per customer, while not ready
    this.pendingSelect = null;      // UX1: the latest Select, sent after the scene's messages
    this.pendingView = null;        // T2: the latest View, for a build with the town
    window.apaUnityReport = (kind, gen, seq, detail) => this.report(String(kind), Number(gen), Number(seq), String(detail));
  }

  // supports2: the build said it shows the version 2 scene.
  get supports2() { return !!(this.bridge && this.bridge.versions.has(2)); }

  // load starts the build. Any failure leaves "Unity 場景未就緒"; the page
  // never falls back to another shop.
  load() {
    this.onStatus("loading", "");
    const s = document.createElement("script");
    s.src = "/unity/WebGL.loader.js";
    s.onerror = () => this.onStatus("failed", "loader");
    s.onload = () => {
      if (typeof window.createUnityInstance !== "function") return this.onStatus("failed", "loader");
      window.createUnityInstance(this.canvas, {
        dataUrl: "/unity/WebGL.data", frameworkUrl: "/unity/WebGL.framework.js", codeUrl: "/unity/WebGL.wasm",
        companyName: "APA", productName: "AgentPaymentAuthority", productVersion: "1",
        // The screen's own pixel density (Aaron 10/27, as pixel-office): a HiDPI screen gets its pixels; the
        // canvas is still a whole multiple of the art on the operator's page (pixelscale.js).
        devicePixelRatio: window.devicePixelRatio || 1,
      }).then((inst) => { this.instance = inst; this.flush(); })
        .catch(() => this.onStatus("failed", "start"));
    };
    document.head.append(s);
  }

  report(kind, gen, seq, detail) {
    if (kind === "bridge") {
      this.bridge = parseBridge(detail);
    } else if (kind === "layout") {
      this.room = { scene: gen, key: detail }; // UX3: the room a new scene shows (display only)
    } else if (kind === "ready") {
      this.ready = true;
      this.art = detail === "limezu" ? "limezu" : "placeholder";
      this.onStatus("ready", this.art);
      this.flush();
    } else if (kind === "select" && this.sceneOwned) {
      // UX1: a click on a customer, with its scene and slot; the scene
      // (scene.js) decides whether it names a current customer.
      if (/^consumer-[a-z0-9-]{1,32}$/.test(detail)) this.onStatus("select2", { scene: gen, slot: seq, consumer: detail });
    } else if (kind === "venue" && this.bridge && this.bridge.town) {
      // T2: a click on a venue in the town view; the page decides.
      if (/^[a-z0-9-]{1,32}$/.test(detail)) this.onStatus("venue", detail);
    } else if (kind === "rendered" && this.sceneOwned) {
      // version 2: the scene matches it to the one customer and slot it names
      const r = parseReport2(gen, seq, detail);
      if (r) this.onStatus("rendered2", r);
    } else if (kind === "rendered" && gen === this.gen) {
      // display confirmation only: what the shop shows, for this generation
      const [phase = "", task = "", verdict = "", reason = ""] = detail.split("|");
      this.onStatus("rendered", { gen, seq, phase, task, verdict, reason });
    }
  }

  // own hands the shop to the version 2 scene (true) or back to the
  // Dashboard's version 1 (false); either way the next message starts the
  // shop over (Unity resets on a version switch).
  own(on) {
    this.sceneOwned = on;
    this.room = null; // the next scene reports its room again; version 1 shows the default one
    this.pending = null;
    this.pendingScene = null;
    this.pending2.clear();
    this.pendingSelect = null;
    if (!on) this.gen = -1;
  }

  // send keeps only the latest message; a new generation is announced with a
  // clear first, so the shop forgets the old customer at once. Before Unity
  // is ready nothing queues up: the latest message waits and is sent on
  // ready (the shop resets itself on the newer generation). Nothing while
  // the version 2 scene owns the shop.
  send(msg) {
    if (this.sceneOwned) return;
    if (msg.gen !== this.gen) {
      this.gen = msg.gen;
      this.post(clearMessage(msg.gen));
    }
    this.pending = msg.clear ? null : msg;
    this.flush();
  }

  // send2 sends a version 2 message; before Unity is ready only the latest
  // scene clear and the latest message of each customer wait.
  send2(msg) {
    if (!this.sceneOwned) return;
    if (this.ready && this.instance) return this.post(msg);
    if (msg.clear && !msg.consumer) {
      this.pendingScene = msg;
      this.pending2.clear();
    } else {
      this.pending2.set(msg.consumer, msg);
    }
  }

  // select2 sends the page's choice to a build that takes it (UX1): it only
  // moves the ring; nothing else in the shop changes.
  select2(msg) {
    if (!this.sceneOwned || !(this.bridge && this.bridge.select)) return;
    this.pendingSelect = msg;
    this.flush();
  }

  // view sends the page's camera choice (T2, docs/commercial-town.md): the
  // town or the current venue, and the canvas's uncovered part ({x, y,
  // width, height} in the canvas's own pixels from its top left; null: all of it).
  view(mode, safe) {
    if (!(this.bridge && this.bridge.town)) return;
    this.pendingView = { mode, safe: safe || { x: 0, y: 0, width: 0, height: 0 } };
    this.flush();
  }

  flush() {
    if (!this.ready || !this.instance) return;
    if (this.pendingView) {
      this.instance.SendMessage("Bridge", "View", JSON.stringify(this.pendingView));
      this.pendingView = null;
    }
    if (this.sceneOwned) {
      const sc = this.pendingScene;
      this.pendingScene = null;
      if (sc) this.post(sc);
      for (const m of this.pending2.values()) this.post(m);
      this.pending2.clear();
      const sel = this.pendingSelect;
      this.pendingSelect = null;
      if (sel) this.instance.SendMessage("Bridge", "Select", JSON.stringify(sel));
      return;
    }
    if (!this.pending) return;
    const m = this.pending;
    this.pending = null;
    if (m.gen === this.gen) this.post(m);
  }

  post(m) {
    if (!this.ready || !this.instance) return;
    this.instance.SendMessage("Bridge", "Receive", JSON.stringify(m));
  }
}
