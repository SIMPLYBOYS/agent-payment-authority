// The talk's words are drawn in Unity with Resources/OfficeFont (Aaron 11/15: speech as pixel-office draws it), a
// CJK subset: a character it lacks shows blank in the WebGL build. Every line, answer and key hint the page can say
// must be in it, and every catalog's purpose (the clerk's answer names it).
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
import * as A from "../adapter.js";

// cmap: the code points an OpenType font maps (cmap subtables of format 4 and 12).
function cmap(buf) {
  const u16 = (o) => buf.readUInt16BE(o), u32 = (o) => buf.readUInt32BE(o);
  let off = -1;
  for (let i = 0; i < u16(4); i++) if (buf.toString("latin1", 12 + 16 * i, 16 + 16 * i) === "cmap") off = u32(20 + 16 * i);
  const have = new Set();
  for (let i = 0; i < u16(off + 2); i++) {
    const s = off + u32(off + 8 + 8 * i);
    if (u16(s) === 12) {
      for (let g = 0; g < u32(s + 12); g++) for (let c = u32(s + 16 + 12 * g); c <= u32(s + 20 + 12 * g); c++) have.add(c);
    } else if (u16(s) === 4) {
      const n = u16(s + 6) / 2;
      for (let k = 0; k < n; k++) {
        const end = u16(s + 14 + 2 * k), start = u16(s + 16 + 2 * n + 2 * k);
        if (start !== 0xffff) for (let c = start; c <= end; c++) have.add(c);
      }
    }
  }
  return have;
}

test("OfficeFont has every character the talk can say", () => {
  const have = cmap(readFileSync(new URL("../../unity/AgentPaymentAuthority/Assets/APA/Resources/OfficeFont.otf", import.meta.url)));
  assert.ok(have.size > 5000, "a CJK subset");
  const words = [];
  for (const p of Object.values(A.PEOPLE)) {
    words.push(p.hello, ...p.lines);
    p.lines.forEach((_, n) => words.push(A.talkReply(A.personTalk(Object.keys(A.PEOPLE).find((k) => A.PEOPLE[k] === p), n).choices)));
  }
  const dir = new URL("../../testdata/demo/", import.meta.url);
  for (const f of readdirSync(dir).filter((x) => /^scenarios.*\.json$/.test(x))) {
    for (const sc of JSON.parse(readFileSync(new URL(f, dir))).scenarios || []) {
      if (!sc.delegation || sc.transit) continue;
      for (const venue of ["複合式書店", "街角烘焙咖啡", "便利商店", "郵局", "旅館"]) {
        const t = A.clerkTalk(venue, A.venueStart([sc], [], true), true);
        words.push(t.line, A.talkReply(t.choices));
      }
    }
  }
  const busy = A.clerkTalk("x", A.venueStart([{ id: "b", consumer_id: "consumer-b" }], [{ consumer_id: "consumer-b", live: "running" }], true), true);
  words.push(busy.line, A.talkReply(busy.choices), A.clerkTalk("x", A.venueStart([], [], true), true).line);
  const missing = [...new Set(words.join("").replace(/\s/g, ""))].filter((c) => !have.has(c.codePointAt(0)));
  assert.deepEqual(missing, [], "drawn blank in Unity: " + missing.join(""));
});
