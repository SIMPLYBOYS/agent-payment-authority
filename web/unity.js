// unity.js: the Unity shop on the page (docs/demo-ui.md section 4). The Web
// build is served by the demo host under /unity/; this module loads it,
// sends it projections and reads its reports. Unity never gets a token and
// never reads the API.

import { clearMessage } from "./adapter.js";

export class UnityHost {
  constructor(canvas, onStatus) {
    this.canvas = canvas;
    this.onStatus = onStatus; // (state, detail): loading | ready | failed | rendered
    this.instance = null;
    this.ready = false;
    this.gen = -1;
    this.pending = null; // the latest message only, while Unity is not ready
    this.art = "";
    window.apaUnityReport = (kind, gen, seq, detail) => this.report(String(kind), Number(gen), Number(seq), String(detail));
  }

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
      }).then((inst) => { this.instance = inst; this.flush(); })
        .catch(() => this.onStatus("failed", "start"));
    };
    document.head.append(s);
  }

  report(kind, gen, seq, detail) {
    if (kind === "ready") {
      this.ready = true;
      this.art = detail === "limezu" ? "limezu" : "placeholder";
      this.onStatus("ready", this.art);
      this.flush();
    } else if (kind === "rendered" && gen === this.gen) {
      // display confirmation only: what the shop shows, for this generation
      const [phase = "", task = "", verdict = "", reason = ""] = detail.split("|");
      this.onStatus("rendered", { gen, seq, phase, task, verdict, reason });
    }
  }

  // send keeps only the latest message; a new generation is announced with a
  // clear first, so the shop forgets the old customer at once. Before Unity
  // is ready nothing queues up: the latest message waits and is sent on
  // ready (the shop resets itself on the newer generation).
  send(msg) {
    if (msg.gen !== this.gen) {
      this.gen = msg.gen;
      this.post(clearMessage(msg.gen));
    }
    this.pending = msg.clear ? null : msg;
    this.flush();
  }

  flush() {
    if (!this.pending || !this.ready || !this.instance) return;
    const m = this.pending;
    this.pending = null;
    if (m.gen === this.gen) this.post(m);
  }

  post(m) {
    if (!this.ready || !this.instance) return;
    this.instance.SendMessage("Bridge", "Receive", JSON.stringify(m));
  }
}
