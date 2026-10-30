"""testnet-preflight.py against a fake relayer socket and a fake node.

    python3 -I -m unittest discover -s scripts/demo -p 'test_testnet_preflight.py'

Needs local sockets (outside a sandbox that forbids them).
"""
import json
import os
import shutil
import socketserver
import subprocess
import sys
import tempfile
import threading
import time
import unittest
from http.server import BaseHTTPRequestHandler, HTTPServer

HERE = os.path.dirname(os.path.abspath(__file__))
GWEI = 10**9


class UnixServer(socketserver.ThreadingUnixStreamServer):
    def get_request(self):
        r, _ = super().get_request()
        return r, ("local", 0)


DAY = 86400


def signer_json(left=7 * DAY):
    now = int(time.time())
    return {"delegations": [{"delegation_id": i, "version": 1, "valid_from": now - DAY, "valid_until": now + left} for i in ("dlg-ta", "dlg-tb")]
            + [{"delegation_id": "dlg-t402", "version": 1, "valid_from": now - 9 * DAY, "valid_until": now - DAY}]}  # test402's, expired: not a show's


def run(health, usdc, gas, price=GWEI // 100, town=False, signer=None):
    """The script's exit code and output with these answers."""
    class H(BaseHTTPRequestHandler):
        def log_message(self, *a):
            pass

        def reply(self, obj):
            b = json.dumps(obj).encode()
            self.send_response(200)
            self.end_headers()
            self.wfile.write(b)

        def do_GET(self):
            self.reply(health)

        def do_POST(self):
            q = json.loads(self.rfile.read(int(self.headers["content-length"])))
            v = {"eth_call": usdc, "eth_getBalance": gas, "eth_gasPrice": price}[q["method"]]
            self.reply({"jsonrpc": "2.0", "id": 1, "result": hex(v)})

    d = tempfile.mkdtemp(dir="/tmp" if len(tempfile.gettempdir()) > 60 else None)  # a unix socket's path is short
    os.makedirs(d + "/host")
    node = HTTPServer(("127.0.0.1", 0), H)
    with open(d + "/host/payments.json", "w") as f:
        json.dump({"rpc": "http://127.0.0.1:%d" % node.server_address[1], "token": "0x" + "4" * 40, "pay_to": "0x" + "3" * 40,
                   "delegations": {"consumer-a/0x" + "3" * 40: "dlg-ta@1", "consumer-b/0x" + "3" * 40: "dlg-tb@1", "consumer-b/0x" + "0" * 37 + "402": "dlg-t402@1"}}, f)
    with open(d + "/signer.json", "w") as f:
        json.dump(signer or signer_json(), f)
    with open(d + "/host/registry.json", "w") as f:
        json.dump({"wallets": [{"wallet_id": "wallet-a", "from": "0x" + "a" * 40}, {"wallet_id": "wallet-b", "from": "0x" + "b" * 40}]}, f)
    relayer = UnixServer(d + "/r.sock", H)
    for s in (node, relayer):
        threading.Thread(target=s.serve_forever, daemon=True).start()
    try:
        r = subprocess.run([sys.executable, "-I", os.path.join(HERE, "testnet-preflight.py"), d + "/r.sock", d + "/host", "--signer", d + "/signer.json"] + (["town"] if town else []), capture_output=True, text=True, timeout=30)
        return r.returncode, r.stdout
    finally:
        node.shutdown()
        relayer.shutdown()
        relayer.server_close()
        shutil.rmtree(d, ignore_errors=True)


READY = {"state": "ready", "daily_tx_cap": 12, "sent_today": 8, "gas": "0x" + "1" * 40, "issuer": "0x" + "2" * 40}


class Preflight(unittest.TestCase):
    def test_enough(self):
        code, out = run(READY, 19920000, 9 * 10**13)
        self.assertEqual(code, 0, out)
        self.assertIn("relayer 今天已送 8／12 筆", out)

    def test_town_needs_two_scenes(self):
        # The town on the real Agent (docs/commercial-live-town.md): 8 relayer transactions, 0.58 a wallet.
        code, out = run({**READY, "sent_today": 5}, 550000, 9 * 10**13, town=True)
        self.assertEqual(code, 1, out)
        self.assertIn("relayer 今天只剩 7 筆，兩個場域、每場兩位客人要 8 筆", out)
        self.assertIn("只有 0.55 test USDC，兩個場域、每場兩位客人要 0.58", out)
        code, out = run({**READY, "sent_today": 4}, 580000, 9 * 10**13, town=True)
        self.assertEqual(code, 0, out)

    def test_short_says_each(self):
        code, out = run({**READY, "sent_today": 12}, 300000, 1000)
        self.assertEqual(code, 1, out)
        self.assertIn("不夠：relayer 今天只剩 0 筆", out)
        self.assertIn("不夠：wallet-b 只有 0.30 test USDC", out)
        self.assertIn("不夠：shop 的 gas", out)

    def test_gas_need_follows_the_price(self):
        # 2 transactions x 500k gas x 2 x price: 2e15 wei at 1 gwei, so 9e13 is short then.
        self.assertEqual(run(READY, 19920000, 9 * 10**13, price=GWEI)[0], 1)

    def test_an_old_relayer_is_never_a_clean_pass(self):
        old = {k: v for k, v in READY.items() if k != "sent_today"}
        code, out = run(old, 19920000, 9 * 10**13)
        self.assertEqual(code, 2, out)
        self.assertIn("未檢查", out)

    def test_delegations_outlast_the_show(self):
        # 10/30: dlg-ta and dlg-tb had expired on 10/28; every Allow was refused after its reservation.
        code, out = run(READY, 19920000, 9 * 10**13, signer=signer_json(left=-3600))
        self.assertEqual(code, 1, out)
        self.assertIn("不夠：委派 dlg-ta@1 到台北", out)
        self.assertIn("不夠：委派 dlg-tb@1 到台北", out)
        self.assertNotIn("dlg-t402", out)
        code, out = run(READY, 19920000, 9 * 10**13, signer=signer_json(left=2 * 3600))  # less than a show
        self.assertEqual(code, 1, out)
        code, out = run(READY, 19920000, 9 * 10**13, signer={"delegations": signer_json()["delegations"][:1]})
        self.assertIn("不夠：signer.json 沒有委派 dlg-tb@1", out)
        code, out = run(READY, 19920000, 9 * 10**13)
        self.assertEqual(code, 0, out)
        self.assertIn("委派 dlg-ta@1 到台北", out)
        self.assertRegex(out, "還有 (7 天 0|6 天 23) 小時")  # the same second, or the next

    def test_an_unreadable_signer_json_is_unchecked(self):
        code, out = run(READY, 19920000, 9 * 10**13, signer="not json")
        self.assertEqual(code, 2, out)
        self.assertIn("讀不到 signer.json", out)

    def test_nothing_there(self):
        r = subprocess.run([sys.executable, "-I", os.path.join(HERE, "testnet-preflight.py"), "/nonexistent.sock", "/nonexistent"], capture_output=True, text=True)
        self.assertEqual(r.returncode, 2, r.stdout)


if __name__ == "__main__":
    unittest.main()
