"""P2-a development Signer configuration (scripts/demo/p2-dev.sh;
docs/commercial-p2-host.md 12.8), from the environment: A and B (the
wallets' addresses), SSOCK (the socket), PAY (p2-local's payments.json),
OUT (signer.json, written 0600) and UID_ (the caller). Reads no key."""
import json, os, time
pay = json.load(open(os.environ["PAY"]))
now = int(time.time())
asset, merchant = pay["token"], pay["pay_to"]
def wallet(x, addr): return {"wallet_id": "wallet-" + x, "consumer_id": "consumer-" + x, "chain_id": "84532", "address": addr}
def delegation(x): return {"delegation_id": "dlg-" + x, "version": 1, "consumer_id": "consumer-" + x, "wallet_id": "wallet-" + x, "chain_id": "84532",
                           "asset_contract": asset, "merchants": [merchant], "per_tx_max": "100000", "total_cap": "1000000",
                           "valid_from": now - 60, "valid_until": now + 7 * 86400}
def approver(x): return {"approver_id": "approver-" + x, "consumer_id": "consumer-" + x, "wallet_ids": ["wallet-" + x], "signer_max": "1000000",
                         "signer_total": "5000000", "signer_window_seconds": 86400, "version": 1}
cfg = {"version": 1, "socket": os.environ["SSOCK"], "callers": [int(os.environ["UID_"])], "max_window_seconds": 600,
       "wallets": [wallet("a", os.environ["A"]), wallet("b", os.environ["B"])],
       "assets": [{"chain_id": "84532", "asset_contract": asset, "eip712_name": pay["token_name"], "eip712_version": pay["token_version"]}],
       "delegations": [delegation("a"), delegation("b")],
       "approvers": [approver("a"), approver("b")],
       "webauthn": {"rp_id": "localhost", "origins": ["http://localhost:8402"], "challenge_ttl_seconds": 120, "approval_ttl_seconds": 300},
       "reservation": {"rpc": pay["rpc"], "chain_id": str(pay["chain_id"]), "budget_contract": pay["budget_contract"], "confirmations": pay["confirmations"]}}
fd = os.open(os.environ["OUT"], os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
os.write(fd, json.dumps(cfg, indent=1).encode())
os.close(fd)
