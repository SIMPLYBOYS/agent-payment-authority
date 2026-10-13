// virtual.mjs: the A1 page end to end in headless Chrome with a CDP virtual
// authenticator, so the page is known to work before the real-device run.
// Every result line it produces is labelled "virtual": it is never a
// real-device result. Usage: node spikes/webauthn/virtual.mjs BINARY OUTDIR
// (OUTDIR is made private here; the spike makes its own run directory in it).
import { spawn } from "node:child_process";
import { mkdirSync, readFileSync, statSync } from "node:fs";
import { dirname } from "node:path";
import { join } from "node:path";
import { startChrome, sleep } from "../../scripts/verify/harness.mjs";

const [bin, outDir] = process.argv.slice(2);
mkdirSync(outDir, { recursive: true, mode: 0o700 });
const srv = spawn(bin, ["--addr", "127.0.0.1:0", "--authenticator", "virtual", "--out-dir", outDir], { stdio: ["ignore", "ignore", "pipe"] });
let log = "";
srv.stderr.on("data", (b) => { log += b; });
let port;
for (let i = 0; i < 100 && !port; i++) { await sleep(100); port = log.match(/listening on http:\/\/localhost:(\d+)/)?.[1]; }
if (!port) throw new Error("spike did not start: " + log);
const results = log.match(/results (\S+)\n/)[1];
console.log("results " + results);

const lines = () => { try { return readFileSync(results, "utf8").trim().split("\n").filter(Boolean).map((l) => JSON.parse(l)); } catch { return []; } };
const b = await startChrome(join(outDir, "chrome-profile"));
let failed = 0;
const check = (ok, what) => { console.log((ok ? "PASS " : "FAIL ") + what); if (!ok) failed++; };
try {
  await b.send("WebAuthn.enable");
  await b.send("WebAuthn.addVirtualAuthenticator", { options: { protocol: "ctap2", transport: "internal", hasResidentKey: true,
    hasUserVerification: true, isUserVerified: true, automaticPresenceSimulation: true } });

  // click runs one step and waits for its result line.
  const click = async (button, step) => {
    const n = lines().length;
    await b.evaluate(`document.getElementById(${JSON.stringify(button)}).click()`);
    for (let i = 0; i < 100; i++) { const l = lines(); if (l.length > n) return l[l.length - 1]; await sleep(100); }
    throw new Error(step + ": no result line");
  };
  await b.send("Page.navigate", { url: `http://localhost:${port}/` });
  check(await b.waitFor(`document.querySelectorAll("#display dt").length === 7`), "the page shows the synthetic purchase and intent_hash");

  let r = await click("b-register", "register");
  check(r.step === "register" && r.ok && r.fmt === "none" && /UV=1/.test(r.flags), "register: " + JSON.stringify([r.ok, r.code, r.fmt, r.flags]));
  r = await click("b-approve", "approve");
  check(r.step === "approve" && r.ok && /UP=1 UV=1/.test(r.flags), "approve: " + JSON.stringify([r.ok, r.code, r.flags]));
  r = await click("b-replay", "replay");
  check(r.step === "replay" && r.code === "CHALLENGE_USED", "replay: " + r.code);
  r = await click("b-wrong", "wrong-challenge");
  check(r.step === "wrong-challenge" && r.code === "ASSERTION_INVALID" && r.detail === "challenge_mismatch", "wrong challenge: " + r.code + " " + r.detail);
  r = await click("b-probe", "uv-discouraged");
  check(r.step === "uv-discouraged", "UV discouraged, recorded: " + JSON.stringify([r.ok, r.code, r.detail, r.flags]));

  // Wrong origin: the same page from 127.0.0.1 cannot use rp_id=localhost.
  await b.send("Page.navigate", { url: `http://127.0.0.1:${port}/` });
  check(await b.waitFor(`!document.getElementById("origin-note").hidden`), "the 127.0.0.1 page says it is the wrong-origin test");
  r = await click("b-register", "browser:register");
  check(r.step === "browser:register" && !r.ok && r.detail === "SecurityError", "wrong origin, browser refuses: " + r.detail);

  check(lines().every((l) => l.authenticator === "virtual"), "every result line is labelled virtual");
  // R2 of 20677b0: fixed words only, in a new private run directory.
  check(lines().every((l) => /^[A-Za-z_:]*$/.test(l.detail || "")), "every detail is a fixed word: " + lines().map((l) => l.detail || "-").join(","));
  check((statSync(results).mode & 0o777) === 0o600 && (statSync(dirname(results)).mode & 0o777) === 0o700 && dirname(dirname(results)) === outDir,
    "results.jsonl 0600 in a new 0700 run directory under OUTDIR");
  // The refusals above are 4xx answers by design (and there is no favicon);
  // only exceptions and console errors count as page errors.
  const errors = b.consoleText.filter((t) => !t.startsWith("LOG Failed to load resource: the server responded with a status of 4"));
  check(errors.length === 0, "no page errors: " + errors.join(" | "));
} finally {
  b.close();
  srv.kill("SIGINT");
}
console.log(failed ? `${failed} FAILED` : "ALL PASS (virtual authenticator; not a real-device result)");
process.exit(failed ? 1 : 0);
