// pixelscale.js: the Unity scene at whole multiples of its art pixels (Aaron
// 10/23: a basic scale by default, zoom on demand, never blurred). The camera
// shows 480 x 320 art pixels (ShopBuilder: orthographic size 5, 32 px a
// unit); the canvas is N times that in CSS pixels, rendered at the screen's
// density (unity.js; a whole multiple again on a whole-number density) and
// scaled by the browser with image-rendering: pixelated.

export const NATIVE_W = 480, NATIVE_H = 320;

// roomPixels: the art pixels the venue view of the room the scene shows has (the bridge's rooms, 32 px a
// tile; interiors v2: a room may be larger); 480 x 320 when the build or the room says nothing.
export function roomPixels(unity) {
  const r = unity && unity.room && unity.bridge && unity.bridge.rooms && unity.bridge.rooms[unity.room.key];
  return r ? { w: Math.round(r.w * 32), h: Math.round(r.h * 32) } : { w: NATIVE_W, h: NATIVE_H };
}
export const MAX_ZOOM = 6;

// fitScale: the largest whole multiple that fits, at least its own pixels;
// only a screen narrower than them (a phone) gets the fraction of its
// width, so the scene is never cut off at the side.
export function fitScale(w, h, nw = NATIVE_W, nh = NATIVE_H) {
  if (w < nw) return Math.max(0.25, Math.floor((w / nw) * 100) / 100);
  return Math.max(1, Math.min(MAX_ZOOM, Math.floor(Math.min(w / nw, h / nh))));
}

// attachZoom: sizes canvas inside frame and wires the controls (a box for
// the −, label and ＋ buttons and 符合). avail() is the space the page gives
// the scene; zooming past it scrolls the frame. The choice is remembered per
// page in this browser only. fill() true (簡報模式, T2): the canvas is the
// whole of avail() and Unity picks its own whole multiples (TownView).
export function attachZoom({ canvas, frame, controls, avail, key, fill = () => false, native = () => ({ w: NATIVE_W, h: NATIVE_H }) }) {
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
  const fit = () => { const a = avail(), n = native(); return fitScale(a.w, a.h, n.w, n.h); };
  if (controls) controls.replaceChildren(btn("－", "縮小場景", () => set(Math.max(1, Math.ceil(current()) - 1))), shown,
    btn("＋", "放大場景", () => set(Math.min(MAX_ZOOM, Math.floor(current()) + 1))), btn("符合", "場景符合畫面", () => set(0)));
  function apply() {
    if (fill()) {
      const a = avail();
      canvas.style.width = Math.floor(a.w) + "px";
      canvas.style.height = Math.floor(a.h) + "px";
      shown.textContent = "符合畫面";
      frame.dataset.zoomed = "false";
      frame.style.maxWidth = frame.style.maxHeight = "";
      canvas.dataset.scale = "fill";
      return;
    }
    const n = current(), px = native();
    canvas.style.width = Math.round(px.w * n) + "px";
    canvas.style.height = Math.round(px.h * n) + "px";
    shown.textContent = n + "×" + (zoom > 0 ? "" : "（符合）");
    const a = avail();
    frame.dataset.zoomed = n > fitScale(a.w, a.h, px.w, px.h) ? "true" : "false"; // larger than the space: the frame scrolls
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
