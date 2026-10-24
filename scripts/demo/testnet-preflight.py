"""Before a Base Sepolia show (docs/commercial-gap-review.md C4; 10/24 a show
ran into the relayer's daily cap unseen): the relayer's transactions today
against its cap, and the balances the show spends from, read-only.

    python3 -I scripts/demo/testnet-preflight.py RELAYER_SOCK HOST_DIR

Prints one line per check. Exit 0: enough for one show of two customers
(the relayer 4: two registrations, two reservations; each wallet one 0.50
brief); 1: not enough, with why; 2: could not check. The shop's own count
is in the Host's readiness table (its database is the Host's).
"""
import http.client
import json
import socket
import sys
import urllib.request

NEED_RELAYER = 4           # two registrations, two reservations
NEED_USDC = 500000         # one 0.50 brief per customer (6 decimals)
NEED_GAS = 10**13          # 0.00001 ETH: about ten transactions at Base Sepolia's fees


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
    sock, host = sys.argv[1], sys.argv[2]
    try:
        h = health(sock)
        pay = json.load(open(host + "/payments.json"))
        reg = json.load(open(host + "/registry.json"))
    except Exception as e:  # noqa: BLE001 - one line says what could not be read
        print("preflight: 無法檢查（%s）" % e)
        return 2
    short = []
    if "sent_today" not in h:
        print("preflight: relayer 沒有回報今天的筆數（舊版？），未檢查")
    else:
        left = max(h["daily_tx_cap"] - h["sent_today"], 0)
        print("preflight: relayer 今天已送 %d／%d 筆（UTC 日，台北 08:00 歸零），剩 %d" % (h["sent_today"], h["daily_tx_cap"], left))
        if left < NEED_RELAYER:
            short.append("relayer 今天只剩 %d 筆，一場兩位客人要 %d 筆（登記 2、預留 2）" % (left, NEED_RELAYER))
    url, token = pay["rpc"], pay["token"]
    try:
        for w in reg["wallets"]:
            n = rpc(url, "eth_call", [{"to": token, "data": "0x70a08231" + w["from"][2:].lower().rjust(64, "0")}, "latest"])
            print("preflight: %s %s：%.2f test USDC" % (w["wallet_id"], w["from"], n / 1e6))
            if n < NEED_USDC:
                short.append("%s 只有 %.2f test USDC，一筆簡報要 0.50" % (w["wallet_id"], n / 1e6))
        for name, a in (("relayer-gas", h.get("gas")), ("issuer", h.get("issuer")), ("shop", pay.get("pay_to"))):
            if not a:
                print("preflight: %s 的位址未知，未檢查 gas" % name)
                continue
            n = rpc(url, "eth_getBalance", [a, "latest"])
            print("preflight: %s %s：%.7f ETH" % (name, a, n / 1e18))
            if n < NEED_GAS:
                short.append("%s 的 gas 只有 %.7f ETH" % (name, n / 1e18))
    except Exception as e:  # noqa: BLE001
        print("preflight: 讀鏈失敗（%s），餘額未檢查" % e)
        return 2
    for s in short:
        print("preflight: 不夠：" + s)
    return 1 if short else 0


if __name__ == "__main__":
    sys.exit(main())
