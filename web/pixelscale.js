// pixelscale.js: the Unity scene at whole multiples of its art pixels (Aaron
// 10/23: a basic scale by default, zoom on demand, never blurred). The camera
// shows 480 x 320 art pixels (ShopBuilder: orthographic size 5, 32 px a
// unit); the canvas is N times that in CSS pixels, drawn 1:1 (unity.js:
// devicePixelRatio 1) and scaled by the browser with image-rendering: pixelated.

export const NATIVE_W = 480, NATIVE_H = 320;
export const MAX_ZOOM = 6;

// fitScale: the largest whole multiple that fits; on a screen too small
// for its own pixels (a phone), the fraction that fits: it is never cut off.
export function fitScale(w, h) {
  const k = Math.min(w / NATIVE_W, h / NATIVE_H);
  return k >= 1 ? Math.min(MAX_ZOOM, Math.floor(k)) : Math.max(0.25, Math.floor(k * 100) / 100);
}

// attachZoom: sizes canvas inside frame and wires the controls (a box for
// the −, label and ＋ buttons and 符合). avail() is the space the page gives
// the scene; zooming past it scrolls the frame. The choice is remembered per
// page in this browser only.
export function attachZoom({ canvas, frame, controls, avail, key }) {
  let zoom = 0; // 0: fit
  try { zoom = Number(localStorage.getItem(key)) || 0; } catch { /* no storage: fit */ }
  const btn = (text, label, on) => {
    const b = document.createElement("button");
    b.type = "button";
    b.textContent = text;
    b.setAttribute("aria-label", label);
    b.addEventListener("click", on);
    return b;
  };
  const shown = document.createElement("span");
  shown.className = "zoom-level";
  const set = (z) => {
    zoom = z;
    try { localStorage.setItem(key, String(z)); } catch { /* not kept */ }
    apply();
  };
  const current = () => (zoom > 0 ? zoom : fit());
  const fit = () => { const a = avail(); return fitScale(a.w, a.h); };
  if (controls) controls.replaceChildren(btn("－", "縮小場景", () => set(Math.max(1, Math.ceil(current()) - 1))), shown,
    btn("＋", "放大場景", () => set(Math.min(MAX_ZOOM, Math.floor(current()) + 1))), btn("符合", "場景符合畫面", () => set(0)));
  function apply() {
    const n = current();
    canvas.style.width = Math.round(NATIVE_W * n) + "px";
    canvas.style.height = Math.round(NATIVE_H * n) + "px";
    shown.textContent = n + "×" + (zoom > 0 ? "" : "（符合）");
    const a = avail();
    frame.dataset.zoomed = n > fitScale(a.w, a.h) ? "true" : "false"; // larger than the space: the frame scrolls
    frame.style.maxWidth = frame.dataset.zoomed === "true" ? Math.floor(a.w) + "px" : "";
    frame.style.maxHeight = frame.dataset.zoomed === "true" ? Math.floor(a.h) + "px" : "";
    canvas.dataset.scale = String(n);
  }
  addEventListener("resize", apply);
  // A size change without a resize event (a panel opening, an emulated screen) refits too; apply is idempotent.
  if (typeof ResizeObserver === "function") new ResizeObserver(() => apply()).observe(document.documentElement);
  apply();
  return { apply, get scale() { return current(); } };
}
