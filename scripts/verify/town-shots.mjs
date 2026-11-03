// Town v2 screenshots (docs/town-v2.md V6): the audience window at 1920x1080 in the town view: the shops at
// rest, the tour along the street (the video's prologue), and customer A walked east past the crossroads to
// the gas station, waiting there while the road's cars call. The demo host with the fake CLI (nothing pays),
// the Unity Web build, headless Chrome. Pictures only: it checks no more than that each view came up.
//
//   node scripts/verify/town-shots.mjs [--unity-build DIR] [--out DIR]

import { execFileSync } from "node:child_process";
import { mkdtempSync, mkdirSync, readFileSync, writeFileSync, chmodSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { startDemo, startChrome, sleep, SUBJECTS } from "./harness.mjs";

const repo = resolve(new URL("../..", import.meta.url).pathname);
const arg = (k, d) => { const i = process.argv.indexOf(k); return i > 0 ? process.argv[i + 1] : d; };
const unityBuild = resolve(arg("--unity-build", join(repo, "unity/AgentPaymentAuthority/Build/WebGL/Build")));
const commit = execFileSync("git", ["-C", repo, "rev-parse", "HEAD"]).toString().trim();
const out = resolve(arg("--out", join(repo, "artifacts/town-v2", "shots-" + commit.slice(0, 12))));
mkdirSync(out, { recursive: true });

const tmp = mkdtempSync(join(tmpdir(), "apa-town-shots-"));
chmodSync(tmp, 0o700);
const home = join(tmp, "home");
mkdirSync(home);
const bin = join(tmp, "bin");
mkdirSync(bin);
for (const c of ["demo", "fake-claude", "apa-issuer"]) execFileSync("go", ["build", "-o", join(bin, c), "./cmd/" + c], { cwd: repo, stdio: "inherit" });
const cred = join(tmp, "cred");
execFileSync(join(bin, "apa-issuer"), ["init", "--dir", cred, "--issuer", "issuer-demo", "--consumers", "consumer-a,consumer-b"]);
execFileSync(join(bin, "apa-issuer"), ["subjects", "--dir", cred, "--subjects", SUBJECTS]);

let demo, b, s, failures = 0;
const step = (name, ok) => { console.log((ok ? "ok   " : "FAIL ") + name); if (!ok) failures++; return ok; };
try {
  const state = join(tmp, "state");
  demo = startDemo({ repo, bin, catalog: join(repo, "testdata/demo/scenarios-p2.json"), unityBuild, state, home, extra: ["--credential-dir", cred] });
  const addr = await demo.ready();
  const tokenFile = join(tmp, "tokens.json");
  writeFileSync(tokenFile, readFileSync(join(state, "tokens.json")));
  b = await startChrome(join(tmp, "chrome-profile"));
  await b.send("Emulation.setFocusEmulationEnabled", { enabled: true });
  await b.send("Emulation.setDeviceMetricsOverride", { width: 1440, height: 900, deviceScaleFactor: 1, mobile: false });
  const click = (sel) => b.evaluate(`(() => { const e = document.querySelector(${JSON.stringify(sel)}); if (!e || e.disabled || e.hidden) return false; e.click(); return true; })()`);
  let n = 0;
  // shot: headless Chrome draws (and Unity runs a frame) only when asked for a picture.
  async function shot(name) {
    const png = await s.send("Page.captureScreenshot", { format: "png" });
    if (name) { writeFileSync(join(out, String(++n).padStart(2, "0") + "-" + name + ".png"), Buffer.from(png.data, "base64")); console.log("  " + name); }
  }
  async function run(ms, every = 250) { for (const end = Date.now() + ms; Date.now() < end; await sleep(every)) await shot(); }

  await b.send("Page.navigate", { url: "http://" + addr + "/host" });
  await b.waitFor(`document.readyState === "complete"`);
  const doc = await b.send("DOM.getDocument", {});
  const q = await b.send("DOM.querySelector", { nodeId: doc.root.nodeId, selector: "#token-file" });
  await b.send("DOM.setFileInputFiles", { nodeId: q.nodeId, files: [tokenFile] });
  step("the host page with the token file", await b.waitFor(`document.getElementById("creds").dataset.mode === "file"`, 20000));
  s = await b.openPage("http://" + addr + "/show");
  await s.send("Emulation.setDeviceMetricsOverride", { width: 1920, height: 1080, deviceScaleFactor: 1, mobile: false });
  step("the window's Unity is ready", await s.waitFor(`document.getElementById("unity-status").dataset.state === "ready"`, 90000));
  step("presenting: the window shows the town", await click("#present-toggle") && await b.waitFor(`!document.getElementById("present-tour").hidden`, 20000)
    && await s.waitFor(`document.body.dataset.phase === "town"`, 20000));
  await run(4000);
  await shot("shops-at-rest");

  // The tour along the street. Headless Chrome runs Unity a frame only per picture, so the pan's pace here is not
  // the real one (it ran several times faster): these are pictures along the way, not of given points.
  step("the tour starts", await click("#present-tour"));
  const t0 = Date.now();
  for (const [t, name] of [[600, "tour-a"], [2500, "tour-b"], [4500, "tour-c"], [7000, "tour-d"], [24000, "tour-back-at-the-shops"]]) {
    await run(Math.max(0, t0 + t - Date.now()));
    await shot(name);
  }

  // Customer A walked east along the north pavement, over the crossroads, to the gas station.
  step("walk customer A", await click('#present-play button[data-play="consumer-a"]') && await b.waitFor(`document.getElementById("present-play-hint").hidden === false`, 10000));
  await run(1500);
  await b.send("Input.dispatchKeyEvent", { type: "keyDown", code: "ArrowRight", key: "ArrowRight", windowsVirtualKeyCode: 39 });
  const w0 = Date.now();
  for (const [t, name] of [[4000, "walk-past-the-hotel"], [9000, "walk-at-the-crossroads"], [19000, "walk-at-the-gas-station"]]) {
    await run(Math.max(0, w0 + t - Date.now()));
    await shot(name);
  }
  await b.send("Input.dispatchKeyEvent", { type: "keyUp", code: "ArrowRight", key: "ArrowRight", windowsVirtualKeyCode: 39 });
  // A minute at the gas station: a picture every 5 s, the cars that call among them.
  for (let i = 1; i <= 12; i++) { await run(5000); await shot("gas-station-" + String(i * 5).padStart(2, "0") + "s"); }
  const errs = b.consoleText.filter((x) => !/favicon/.test(x));
  step("no page errors", errs.length === 0) || console.log(errs.join("\n"));
} finally {
  if (b) b.close();
  if (demo) await demo.stop();
}
console.log("shots: " + out);
process.exit(failures ? 1 : 0);
