#!/usr/bin/env python3
"""Z2 contract reverse checks: remove one check of src/Budget.sol in a
disposable copy of contracts/ and require the named forge test to fail
(a compile error or any other failure does not count).

  python3 -I contracts/reverse.py OUT_DIR [name regex]
"""
import os, re, shutil, subprocess, sys, tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
FORGE = os.path.join(os.path.dirname(HERE), "var/tools/foundry-v1.8.3/bin/forge")
SRC = "src/Budget.sol"
ITEMS = [  # (name, forge test, old, new)
    ("a zero amount is refused", "test_ZeroAmount", "        if (amount == 0) revert ZeroAmount();\n", ""),
    ("C_old is the current commitment", "test_SecondSpendOfAnOldState",
     "if (current == 0 || current != cOld) revert StaleCommitment();", "if (current == 0 || false && current != cOld) revert StaleCommitment();"),
    ("a nullifier is used once", "test_UsedNullifierWithTheCommitmentReinstated",
     "        if (nullifierUsed[nullifier]) revert NullifierUsed();\n", ""),
    ("the policy signature is checked", "test_PolicySignatureByAnotherKey",
     "!= policySigner) {\n            revert PolicySignatureInvalid();", "!= policySigner && false) {\n            revert PolicySignatureInvalid();"),
    ("a malleated signature is refused", "test_MalleatedPolicySignature",
     "if (uint256(s) > HALF_N || (v != 27 && v != 28)) return address(0);", "if (false && uint256(s) > HALF_N || (v != 27 && v != 28)) return address(0);"),
    ("the new commitment is stored", "test_RegisterAndSpendTwice", "        commitment[task] = cNew;\n", ""),
    ("only the issuer registers", "test_RegisterOnlyIssuer", "        if (msg.sender != issuer) revert NotIssuer();\n", ""),
    ("a task registers once", "test_RegisterOnce", "        if (commitment[task] != 0) revert AlreadyRegistered();\n", ""),
    ("the proof is verified", "test_TamperedAmount", "        verifier.verifyProof(\n", "        if (false) verifier.verifyProof(\n"),
]


def main():
    out = sys.argv[1]
    os.makedirs(out, exist_ok=True)
    only = re.compile(sys.argv[2]) if len(sys.argv) > 2 else None
    items = [(n, it) for n, it in enumerate(ITEMS, 1) if not only or only.search(it[0])]
    caught = 0
    for n, (name, test, old, new) in items:
        tmp = tempfile.mkdtemp(prefix="z2crev-")
        copy = os.path.join(tmp, "contracts")
        shutil.copytree(HERE, copy, ignore=shutil.ignore_patterns("reverse.py", "__pycache__", ".claude"))
        cfg = os.path.join(copy, "foundry.toml")
        conf = open(cfg).read()  # read before opening for writing (which truncates)
        open(cfg, "w").write(conf.replace('"../var/contracts/out"', '"out"').replace('"../var/contracts/cache"', '"cache"'))
        path = os.path.join(copy, SRC)
        text = open(path).read()
        if text.count(old) != 1:
            result, why, log = "not-caught", f"anchor found {text.count(old)} times", ""
        else:
            open(path, "w").write(text.replace(old, new))
            p = subprocess.run([FORGE, "test", "--root", copy, "--match-test", f"^{test}\\(", "--offline"], cwd=copy, capture_output=True, text=True, timeout=600)
            log = p.stdout + p.stderr
            if "Compiler run failed" in log or "Error (" in log:
                result, why = "not-caught", "compile failed"
            elif p.returncode == 0 or not re.search(rf"\[FAIL[^\]]*\] {re.escape(test)}\(", log):
                result, why = "not-caught", f"exit {p.returncode}; {test} did not fail"
            else:
                result, why = "caught", re.search(rf"\[FAIL[^\]]*\] {re.escape(test)}\([^\n]*", log).group(0)[:160]
                caught += 1
        open(os.path.join(out, f"{n:02d}-{name.replace(' ', '-')}.log"), "w").write(f"item: {name}\ntest: {test}\nmutation: {old!r} -> {new!r}\n\n{log}")
        print(f"RESULT {result} | {name} | {why}", flush=True)
        shutil.rmtree(tmp, ignore_errors=True)
    print(f"{caught}/{len(items)} caught by the named test failing")
    return 0 if caught == len(items) else 1


if __name__ == "__main__":
    sys.exit(main())
