"""Before a Base Sepolia show (docs/commercial-gap-review.md C4; 10/24 a show
ran into the relayer's daily cap unseen): the relayer's transactions today
against its cap, and the balances the show spends from, read-only; and
(10/30: every Allow refused DELEGATION_EXPIRED after its reservation) how long
the Signer's delegations for the shop still run, from its signer.json.

    python3 -I scripts/demo/testnet-preflight.py RELAYER_SOCK HOST_DIR [--signer SIGNER.JSON] [town]

town: the town on the real Agent (docs/commercial-live-town.md), three venues of two customers
(docs/commercial-venues.md, Aaron 10/30): the relayer 11 (6 registrations, 5 reservations: B's
beer is denied before any), three times the gas keys' transactions, and a wallet pays up to 0.67
(A: the brief 0.50, the lunch 0.08, the beer 0.09; B: 0.06 and 0.42). The shop settles 5; the
town's host has a daily cap of 6 (e1a-host --shop-cap; its count is in the Host's readiness table).

Prints one line per check. Exit 0: enough for one show of two customers
(the relayer 4: two registrations, two reservations; each wallet one 0.50
brief; each gas key two transactions at today's gas price); 1: not enough,
with why; 2: something could not be checked (review: never a clean pass
then). The shop's own count is in the Host's readiness table (its database
is the Host's).
"""
import http.client
import json
import socket
import sys
import time
import urllib.request
from datetime import datetime, timedelta, timezone

NEED_RELAYER = 4           # two registrations, two reservations
NEED_USDC = 500000         # one 0.50 brief per customer (6 decimals)
TX_GAS = 500000            # one transaction's gas, above a reservation's (~420k); the L1 data fee on top is small on Base Sepolia
TXS = {"relayer-gas": 2, "issuer": 2, "shop": 2}  # a show of two customers: reservations, registrations, settlements
SHOW_SECONDS = 3 * 3600    # a delegation must outlast the show: its device confirmation pays for 2 hours
TAIPEI = timezone(timedelta(hours=8))


class UnixHTTP(http.client.HTTPConnection):
    def __init__(self, path):
        super().__init__("relayer", timeout=5)
        self.path = path

    def connect(self):
        self.sock = socket.socket(socket.AF_UNIX)
        self.sock.settimeout(5)
        self.sock.connect(self.path)


def health(path):
    c = UnixHTTP(path)
    c.request("GET", "/v1/health")
    r = c.getresponse()
    if r.status != 200:
        raise RuntimeError("relayer health %d" % r.status)
    return json.load(r)


def rpc(url, method, params):
    req = urllib.request.Request(url, data=json.dumps({"jsonrpc": "2.0", "id": 1, "method": method, "params": params}).encode(),
                                 headers={"content-type": "application/json", "user-agent": "apa-preflight"})
    with urllib.request.urlopen(req, timeout=15) as r:
        out = json.load(r)
    if "result" not in out:
        raise RuntimeError(method + ": no result")
    return int(out["result"], 16)


def main():
    sock, host, rest, signer = sys.argv[1], sys.argv[2], sys.argv[3:], None
    if "--signer" in rest:
        i = rest.index("--signer")
        signer = rest[i + 1]
        del rest[i:i + 2]
    town = rest == ["town"]
    try:
        return check(sock, host, town, signer)
    except Exception as e:  # noqa: BLE001 - one line says what could not be checked; a crash is never "short"
        print("preflight: 無法檢查（%s）" % e)  # the relayer's socket or the host files: no node URL in it
        return 2


def delegations(pay, signer, now):
    """The shop's delegations in signer.json: (lines, short)."""
    have = {d["delegation_id"]: d for d in json.load(open(signer)).get("delegations", [])}
    lines, short = [], []
    for k, ref in sorted(pay.get("delegations", {}).items()):
        if k.partition("/")[2].lower() != pay["pay_to"].lower():
            continue  # test402's: not paid in a show
        dlg, _, ver = ref.partition("@")
        d = have.get(dlg)
        if d is None or str(d.get("version")) != ver:
            short.append("signer.json 沒有委派 %s" % ref)
            continue
        until = datetime.fromtimestamp(d["valid_until"], TAIPEI).strftime("%m/%d %H:%M")
        left = d["valid_until"] - now
        lines.append("preflight: 委派 %s 到台北 %s，還有 %d 天 %d 小時" % (ref, until, max(left, 0) // 86400, max(left, 0) % 86400 // 3600))
        if d["valid_from"] > now or left < SHOW_SECONDS:
            short.append("委派 %s 到台北 %s 為止，一場要還有 %d 小時（重產 /opt 的 signer.json：p2-testnet opt-signer）" % (ref, until, SHOW_SECONDS // 3600))
    return lines, short


def check(sock, host, town=False, signer=None):
    scenes = 3 if town else 1
    need_relayer, need_usdc = (11, 670000) if town else (NEED_RELAYER, NEED_USDC)
    show = "三個場域、每場兩位客人" if town else "一場兩位客人"
    h = health(sock)
    pay = json.load(open(host + "/payments.json"))
    reg = json.load(open(host + "/registry.json"))
    url, token = pay["rpc"], pay["token"]
    short, unchecked = [], False
    try:
        lines, s = delegations(pay, signer, int(time.time())) if signer else (["preflight: 沒有 signer.json，委派的期限未檢查"], [])
        unchecked = unchecked or not signer
        for line in lines:
            print(line)
        short += s
    except Exception as e:  # noqa: BLE001 - an unreadable signer.json (sudo expired): unchecked, the rest still checked
        print("preflight: 讀不到 signer.json（%s），委派的期限未檢查" % e.__class__.__name__)
        unchecked = True
    if "sent_today" not in h:
        unchecked = True
        print("preflight: relayer 沒有回報今天的筆數（舊版，或讀不到），未檢查")
    else:
        left = max(h["daily_tx_cap"] - h["sent_today"], 0)
        print("preflight: relayer 今天已送 %d／%d 筆（UTC 日，台北 08:00 歸零），剩 %d" % (h["sent_today"], h["daily_tx_cap"], left))
        if left < need_relayer:
            short.append("relayer 今天只剩 %d 筆，%s要 %d 筆（每位客人登記 1、預留 1）" % (left, show, need_relayer))
    try:
        price = rpc(url, "eth_gasPrice", [])
        print("preflight: gas price %.4f gwei" % (price / 1e9))
        for w in reg["wallets"]:
            n = rpc(url, "eth_call", [{"to": token, "data": "0x70a08231" + w["from"][2:].lower().rjust(64, "0")}, "latest"])
            print("preflight: %s %s：%.2f test USDC" % (w["wallet_id"], w["from"], n / 1e6))
            if n < need_usdc:
                short.append("%s 只有 %.2f test USDC，%s要 %.2f" % (w["wallet_id"], n / 1e6, show, need_usdc / 1e6))
        for name, a in (("relayer-gas", h.get("gas")), ("issuer", h.get("issuer")), ("shop", pay.get("pay_to"))):
            if not a:
                unchecked = True
                print("preflight: %s 的位址未知，未檢查 gas" % name)
                continue
            n = rpc(url, "eth_getBalance", [a, "latest"])
            need = TXS[name] * scenes * TX_GAS * 2 * price  # the node admits a transaction at about twice the base fee
            print("preflight: %s %s：%.7f ETH（一場約要 %.7f）" % (name, a, n / 1e18, need / 1e18))
            if n < need:
                short.append("%s 的 gas 只有 %.7f ETH，一場約要 %.7f" % (name, n / 1e18, need / 1e18))
    except Exception as e:  # noqa: BLE001
        print("preflight: 讀鏈失敗（%s），其餘未檢查" % str(e).replace(url, "<rpc>"))  # a node's URL may carry an API key
        unchecked = True
    for s in short:
        print("preflight: 不夠：" + s)
    return 1 if short else 2 if unchecked else 0


if __name__ == "__main__":
    sys.exit(main())
