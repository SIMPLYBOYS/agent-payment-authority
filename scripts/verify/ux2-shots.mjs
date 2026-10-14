// UX2 concept screenshots (docs/commercial-scenes.md): the clickable
// prototype docs/mockups/scenes/index.html in headless Chrome, desktop
// 1440x900 and phone 390x844, written next to it. Placeholder art only.
//
//   node scripts/verify/ux2-shots.mjs
import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { startChrome, sleep } from "./harness.mjs";

const repo = resolve(new URL("../..", import.meta.url).pathname);
const dir = join(repo, "docs/mockups/scenes");
const page = "file://" + join(dir, "index.html");
const shots = [
  ["1-picker", "view=picker", 1440, 900],
  ["2-task-cafe", "view=task", 1440, 900],
  ["3-ask-mart", "view=ask&scene=mart", 1440, 900],
  ["3b-failed-evidence-mart", "view=ask&scene=mart&failed=1&evidence=1", 1440, 900],
  ["4-ask-data-main", "view=ask&scene=data", 1440, 900],
  ["m1-picker", "view=picker", 390, 844],
  ["m3-ask-mart", "view=ask&scene=mart", 390, 844],
];
const b = await startChrome(mkdtempSync(join(tmpdir(), "ux2-chrome-")));
let failures = 0;
try {
  for (const [name, query, w, h] of shots) {
    await b.send("Emulation.setDeviceMetricsOverride", { width: w, height: h, deviceScaleFactor: 1, mobile: w < 500 });
    await b.send("Page.navigate", { url: page + "?" + query });
    await b.waitFor(`document.readyState === "complete" && !!document.getElementById("app").firstElementChild`);
    await sleep(300);
    const overflow = await b.evaluate(`document.documentElement.scrollWidth > window.innerWidth`);
    if (overflow) { failures++; console.log("FAIL " + name + ": horizontal overflow"); }
    const png = await b.send("Page.captureScreenshot", { format: "png", captureBeyondViewport: false });
    writeFileSync(join(dir, name + ".png"), Buffer.from(png.data, "base64"));
    console.log("shot " + name + ".png" + (overflow ? " (overflow)" : ""));
  }
  const errs = b.consoleText.filter((x) => !/favicon/.test(x));
  if (errs.length) { failures++; console.log("page errors: " + errs.join(" | ")); }
} finally {
  b.close();
}
process.exit(failures ? 1 : 0);
