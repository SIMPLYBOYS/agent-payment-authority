#!/usr/bin/env python3
"""Z2 end-to-end on a local chain (docs/commercial-z2.md): start anvil,
deploy the verifier and the budget contract, register a task, prove a
spend with zkbudget, sign it with the policy test key, send it from a
relayer, and check the state; then the refusals on chain (a resend, a
tampered amount, another key's signature), each leaving the state as it
was, and a second spend from the new state.

Synthetic data and anvil's well-known test accounts only; nothing leaves
127.0.0.1. No secret is printed (the test keys are anvil's public ones).

  python3 -I scripts/verify/z2-anvil.py OUT_DIR
"""
import hashlib, json, os, secrets, shutil, socket, subprocess, sys, time

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
FOUNDRY = os.path.join(ROOT, "var/tools/foundry-v1.8.3/bin")
FORGE, ANVIL, CAST = (os.path.join(FOUNDRY, b) for b in ("forge", "anvil", "cast"))
# anvil's default accounts (its documented test mnemonic): 0 deployer, 1 issuer, 2 policy signer, 3 relayer.
KEYS = ["0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80",
        "0x59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d",
        "0x5de4111afa1a4b94908f83103eb1f1706367c2e68ca870fc3fb9f804cdab365a",
        "0x7c852118294e51e653712a81e05800f419141751be58f605c371e15141b007a6"]

lines, failures = [], 0


def log(s):
    lines.append(s)
    print(s, flush=True)


def check(name, ok, detail=""):
    global failures
    log(("PASS " if ok else "FAIL ") + name + (f"  [{detail}]" if detail else ""))
    failures += 0 if ok else 1


def run(cmd, cwd=ROOT, ok=True, env=None):
    p = subprocess.run(cmd, cwd=cwd, capture_output=True, text=True, timeout=600, env=env)
    if ok and p.returncode != 0:
        raise RuntimeError(f"{os.path.basename(cmd[0])} {cmd[1] if len(cmd) > 1 else ''}: {p.stderr.strip()[:400]}")
    return p


def main():
    out = os.path.abspath(sys.argv[1])  # go build runs in zk/: a relative path would land there
    os.makedirs(out, exist_ok=True)
    go_env = dict(os.environ, GOTOOLCHAIN="go1.26.8", GOFLAGS="-mod=readonly", GOPROXY="off")
    bin_dir = os.path.join(out, "bin")
    os.makedirs(bin_dir, exist_ok=True)
    zkb = os.path.join(bin_dir, "zkbudget")
    run(["go", "build", "-o", zkb, "./cmd/zkbudget"], cwd=os.path.join(ROOT, "zk"), env=go_env)
    setup = os.path.join(ROOT, "zk/setup")
    commit = run(["git", "rev-parse", "HEAD"]).stdout.strip()
    dirty = run(["git", "status", "--porcelain", "--untracked-files=no"]).stdout.strip() != ""
    log(f"commit {commit}" + (" (tracked changes present)" if dirty else ""))
    log(run([ANVIL, "--version"]).stdout.strip().splitlines()[0])

    anvil = None
    try:
        s = socket.socket()  # inside try: a refused bind (a sandbox) still writes the log
        s.bind(("127.0.0.1", 0))
        port = s.getsockname()[1]
        s.close()
        rpc = f"http://127.0.0.1:{port}"
        anvil = subprocess.Popen([ANVIL, "--host", "127.0.0.1", "--port", str(port), "--chain-id", "31337", "--silent"],
                                 stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        for _ in range(100):
            if run([CAST, "chain-id", "--rpc-url", rpc], ok=False).returncode == 0:
                break
            time.sleep(0.1)
        cast = lambda *a, ok=True: run([CAST, *a, "--rpc-url", rpc], ok=ok)
        addr = [run([CAST, "wallet", "address", "--private-key", k]).stdout.strip() for k in KEYS]

        def create(contract, *args):
            p = run([FORGE, "create", "--rpc-url", rpc, "--private-key", KEYS[0], "--broadcast", contract,
                     *(["--constructor-args", *args] if args else [])], cwd=os.path.join(ROOT, "contracts"))
            return next(l.split()[-1] for l in p.stdout.splitlines() if l.startswith("Deployed to:"))

        verifier = create("src/Verifier.sol:Verifier")
        budget = create("src/Budget.sol:Budget", verifier, addr[1], addr[2])
        log(f"deployed verifier {verifier}, budget {budget} on chain 31337 (anvil)")

        def zk(*a):
            return json.loads(run([zkb, *a]).stdout)

        r0, r1, r2 = (zk("random")["r"] for _ in range(3))
        task, balance = "show-a-01", 5_000_000
        c0 = zk("commit", "-contract", budget, "-task", task, "-balance", str(balance), "-r", r0)
        field = cast("call", budget, "taskField(string)(uint256)", task).stdout.split()[0]
        check("the contract's task field is the prover's (chain, contract and task)", field == c0["task_field"], field)
        rc = json.loads(cast("send", "--private-key", KEYS[1], "--json", budget, "registerTask(string,uint256)", task, c0["commitment"]).stdout)
        check("registerTask by the issuer", rc["status"] == "0x1", f"gas {int(rc['gasUsed'], 16)}")
        reg_gas = int(rc["gasUsed"], 16)
        not_issuer = cast("send", "--private-key", KEYS[3], budget, "registerTask(string,uint256)", "show-x-01", c0["commitment"], ok=False)
        check("registerTask by anyone else reverts", not_issuer.returncode != 0)

        def state():
            c = cast("call", budget, "commitment(uint256)(uint256)", field).stdout.split()[0]
            return c

        def used(n):
            return cast("call", budget, "nullifierUsed(uint256)(bool)", n).stdout.strip() == "true"

        def prove(bal, r, amount, rnew, label):
            intent = "0x" + hashlib.sha256(("synthetic intent " + label).encode()).hexdigest()
            p = zk("prove", "-dir", setup, "-contract", budget, "-task", task, "-balance", str(bal), "-r", r,
                   "-amount", str(amount), "-rnew", rnew, "-intent", intent)
            p["intent"] = intent
            return p

        def sign(p, key=KEYS[2], amount=None):
            i = p["inputs"]
            d = cast("call", budget, "policyDigest(uint256,bytes32,uint256,uint256,uint256)(bytes32)", i[0], p["intent"],
                     str(amount if amount is not None else i[2]), i[4], i[3]).stdout.strip()
            return run([CAST, "wallet", "sign", "--private-key", key, d]).stdout.strip()

        def spend(p, sig, amount=None, key=KEYS[3], mined=False):
            i = p["inputs"]
            force = ["--gas-limit", "2000000"] if mined else []  # skip estimation: the failing transaction is mined
            return cast("send", "--private-key", key, "--json", *force, budget,
                        "spend(string,uint256,uint256,uint256,uint256,bytes32,bytes,bytes)", task, i[1],
                        str(amount if amount is not None else i[2]), i[3], i[4], p["intent"], p["proof"], sig, ok=False)

        p1 = prove(balance, r0, 500_000, r1, "e2e-1")
        # A tampered amount, signed by the policy layer too: only the proof can refuse it.
        bad = spend(p1, sign(p1, amount=500_001), amount=500_001, mined=True)
        rcpt = json.loads(bad.stdout) if bad.stdout.strip().startswith("{") else {}
        status, used_gas = rcpt.get("status"), int(rcpt.get("gasUsed", "0x0"), 16)
        # Well below the 2M limit: a revert, not out-of-gas (review note).
        check("a tampered amount is mined and fails (status 0, not out of gas); the state is as it was",
              status == "0x0" and 0 < used_gas < 1_500_000 and state() == c0["commitment"] and not used(p1["inputs"][4]),
              f"status {status}, gas {used_gas}")
        other = spend(p1, sign(p1, key=KEYS[0]))
        check("another key's policy signature reverts; the state is as it was", other.returncode != 0 and state() == c0["commitment"])
        good = spend(p1, sign(p1))
        rc = json.loads(good.stdout) if good.returncode == 0 else {}
        spend_gas = int(rc.get("gasUsed", "0x0"), 16)
        topic = "0x" + run([CAST, "keccak", "Spent(uint256,bytes32,uint256,uint256,uint256)"]).stdout.strip()[2:]
        events = [l for l in rc.get("logs", []) if l["topics"][0] == topic]
        check("the spend from a relayer: new commitment, nullifier used, one Spent event with the intent",
              rc.get("status") == "0x1" and state() == p1["inputs"][3] and used(p1["inputs"][4]) and len(events) == 1
              and events[0]["data"][2:66] == p1["intent"][2:], f"gas {spend_gas}")
        again = spend(p1, sign(p1))
        check("the same spend sent again reverts; the state is as it was", again.returncode != 0 and state() == p1["inputs"][3])
        p2 = prove(balance - 500_000, r1, 400_000, r2, "e2e-2")
        second = spend(p2, sign(p2))
        check("the next spend from the new state", second.returncode == 0 and state() == p2["inputs"][3], f"gas {int(json.loads(second.stdout)['gasUsed'], 16) if second.returncode == 0 else 0}")
        json.dump({"register_gas": reg_gas, "spend_gas": spend_gas, "budget": budget, "verifier": verifier},
                  open(os.path.join(out, "gas.json"), "w"), indent=2)
    except Exception as e:
        check("the flow ran to its end", False, str(e)[:400])
    else:
        check("the flow ran to its end", True)
    finally:
        if anvil:
            anvil.terminate()
            anvil.wait()
    log("result: " + (f"FAIL ({failures})" if failures else "PASS"))
    open(os.path.join(out, "z2-anvil.txt"), "w").write("\n".join(lines) + "\n")
    shutil.rmtree(bin_dir, ignore_errors=True)
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
