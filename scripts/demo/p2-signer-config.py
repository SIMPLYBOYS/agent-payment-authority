"""P2-a development Signer configuration (scripts/demo/p2-dev.sh;
docs/commercial-p2-host.md 12.8), from the environment: A and B (the
wallets' addresses), SSOCK (the socket), PAY (p2-local's payments.json),
OUT (signer.json, written 0600) and UID_ (the caller). Reads no key.

The chain, the token, the merchant and each consumer's delegation ID and
version come from payments.json: the Host starts payments under exactly
those delegations. The caps last the life of signer.db, which is kept
until the demo ends (the delegation's total counts every payment ever
signed under it; the approver's total, 24 hours): both are the minted 20.00
test USDC, so rehearsals do not run them out."""
import json, os, time

pay = json.load(open(os.environ["PAY"]))
now = int(time.time())
chain, asset, merchant = str(pay["chain_id"]), pay["token"], pay["pay_to"]
dlg = {c: ref.split("@") for c, ref in pay["delegations"].items()}  # consumer -> [id, version]
CAP = "20000000"


def wallet(x, addr):
    return {"wallet_id": "wallet-" + x, "consumer_id": "consumer-" + x, "chain_id": chain, "address": addr}


def delegation(x):
    did, ver = dlg["consumer-" + x]
    return {"delegation_id": did, "version": int(ver), "consumer_id": "consumer-" + x, "wallet_id": "wallet-" + x, "chain_id": chain,
            "asset_contract": asset, "merchants": [merchant], "per_tx_max": "100000", "total_cap": CAP,
            "valid_from": now - 60, "valid_until": now + 7 * 86400}


def approver(x):
    return {"approver_id": "approver-" + x, "consumer_id": "consumer-" + x, "wallet_ids": ["wallet-" + x], "signer_max": "1000000",
            "signer_total": CAP, "signer_window_seconds": 86400, "version": 1}


cfg = {"version": 1, "socket": os.environ["SSOCK"], "callers": [int(os.environ["UID_"])], "max_window_seconds": 600,
       "wallets": [wallet("a", os.environ["A"]), wallet("b", os.environ["B"])],
       "assets": [{"chain_id": chain, "asset_contract": asset, "eip712_name": pay["token_name"], "eip712_version": pay["token_version"]}],
       "delegations": [delegation("a"), delegation("b")],
       "approvers": [approver("a"), approver("b")],
       "webauthn": {"rp_id": "localhost", "origins": ["http://localhost:8402"], "challenge_ttl_seconds": 120, "approval_ttl_seconds": 300},
       "reservation": {"rpc": pay["rpc"], "chain_id": chain, "budget_contract": pay["budget_contract"], "confirmations": pay["confirmations"]}}
fd = os.open(os.environ["OUT"], os.O_WRONLY | os.O_CREAT | os.O_TRUNC | os.O_NOFOLLOW, 0o600)
os.fchmod(fd, 0o600)  # an existing file keeps its mode otherwise
os.write(fd, json.dumps(cfg, indent=1).encode())
os.close(fd)
